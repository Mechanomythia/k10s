import K10sCore
import SwiftUI

/// One cluster's overview: header, then four plates fed by overview.subscribe.
struct OverviewPulse: View {
    var tab: TabModel
    @Environment(EngineClient.self) private var engine
    @State private var summary: Summary?
    @State private var updated = Date()

    private struct SubKey: Hashable {
        var scope: TabScope
        var restarts: Int
    }

    private var context: String { tab.scope.contexts[0] }

    var body: some View {
        Group {
            if let s = summary, s.state == .unreachable || s.state == .unauthorized, let st = status(s) {
                ContextErrorView(context: context, status: st)
            } else {
                ScrollView { content }
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        .task(id: SubKey(scope: tab.scope, restarts: engine.restarts)) { await subscribe() }
    }

    private func subscribe() async {
        let params = OverviewSubscribeParams(contexts: [context])
        do {
            for try await event in try await engine.subscribe(Method.overviewSubscribe, params: params) {
                if event.name == EventName.summary, let s = try? event.decode(Summary.self), s.context == context {
                    summary = s
                    updated = Date()
                }
            }
        } catch {}
    }

    /// ContextStatus has no public init: round-trip the shared fields through ContextStateChange.
    private func status(_ s: Summary) -> ContextStatus? {
        guard let data = try? JSONEncoder().encode(s),
              let change = try? JSONDecoder().decode(ContextStateChange.self, from: data) else { return nil }
        var h = ContextHealth()
        h.apply(change)
        return h.statuses[s.context]
    }

    // MARK: Layout

    private var content: some View {
        VStack(alignment: .leading, spacing: Hy.s5) {
            HStack(alignment: .firstTextBaseline, spacing: Hy.s3) {
                Text("Overview").font(Hy.display(24)).foregroundStyle(Hy.starlight)
                TimelineView(.periodic(from: .now, by: 1)) { tl in
                    Text(meta(now: tl.date)).font(Hy.mono(11)).foregroundStyle(Hy.faint)
                }
            }
            if let s = summary {
                Grid(horizontalSpacing: Hy.s5, verticalSpacing: Hy.s5) {
                    GridRow {
                        capacity(s)
                        workloads(s)
                    }
                    GridRow {
                        nodes(s)
                        warnings(s)
                    }
                }
            }
        }
        .padding(Hy.s5)
        .frame(maxWidth: .infinity, alignment: .topLeading)
    }

    private func meta(now: Date) -> String {
        var parts: [String] = []
        if let v = summary?.version { parts.append(v) }
        if let n = summary?.nodesTotal { parts.append("\(n) nodes") }
        parts.append("updated \(max(0, Int(now.timeIntervalSince(updated)))) s ago")
        return parts.joined(separator: " \u{00b7} ")
    }

    // MARK: Plates

    private func capacity(_ s: Summary) -> some View {
        let metrics = s.metricsAvailable
        let cpuPct = percent(s.cpuUsedMilli, s.cpuAllocatableMilli)
        let memPct = percent(s.memUsedBytes, s.memAllocatableBytes)
        return Plate(title: "Capacity", caption: "used / allocatable") {
            VStack(alignment: .leading, spacing: Hy.s3) {
                HStack(alignment: .top, spacing: Hy.s5) {
                    figure("CPU", metrics ? cpuPct.map { "\($0) %" } ?? "n/a" : "n/a",
                           metrics ? coreText(s) : "", color: metrics ? load(cpuPct) : Hy.faint)
                    figure("Memory", metrics ? memPct.map { "\($0) %" } ?? "n/a" : "n/a",
                           metrics ? memText(s) : "", color: metrics ? load(memPct) : Hy.faint)
                    figure("Pods", count(s.podsRunning), "of \(group(s.podCapacity)) schedulable", color: Hy.starlight)
                }
                if !metrics {
                    Text("metrics-server not found").font(Hy.mono(11)).foregroundStyle(Hy.faint)
                }
            }
            .padding(Hy.s4)
            .frame(maxWidth: .infinity, alignment: .leading)
        }
    }

    private func workloads(_ s: Summary) -> some View {
        Plate(title: "Workloads", caption: "all namespaces") {
            HStack(alignment: .top, spacing: Hy.s5) {
                figure("Failing pods", count(s.podsFailing), "", color: (s.podsFailing ?? 0) > 0 ? Hy.classM : Hy.starlight,
                       open: (s.podsFailing ?? 0) > 0 ? { drill("pods", [.failed]) } : nil)
                figure("Degraded", count(s.podsDegraded), "", color: (s.podsDegraded ?? 0) > 0 ? Hy.classK : Hy.starlight,
                       open: (s.podsDegraded ?? 0) > 0 ? { drill("pods", [.degraded]) } : nil)
                figure("Not ready", count(s.workloadsNotReady), "of \(group(s.workloadsTotal)) workloads",
                       color: (s.workloadsNotReady ?? 0) > 0 ? Hy.classK : Hy.starlight,
                       open: (s.workloadsNotReady ?? 0) > 0 ? { drill("deployments", [.pending, .degraded]) } : nil)
            }
            .padding(Hy.s4)
            .frame(maxWidth: .infinity, alignment: .leading)
        }
    }

    private func nodes(_ s: Summary) -> some View {
        Plate(title: "Nodes", caption: "busiest first") {
            VStack(alignment: .leading, spacing: Hy.s2) {
                ForEach(s.topNodes ?? [], id: \.name) { n in
                    HStack(spacing: Hy.s3) {
                        Text(n.name).font(Hy.mono(12)).foregroundStyle(Hy.starlight).lineLimit(1).truncationMode(.middle)
                        Spacer(minLength: Hy.s3)
                        Text(n.cpuPercent.map { "CPU \($0) %" } ?? "CPU n/a").font(Hy.mono(12))
                            .foregroundStyle(n.cpuPercent == nil ? Hy.faint : load(n.cpuPercent))
                        Text(n.memPercent.map { "mem \($0) %" } ?? "mem n/a").font(Hy.mono(12))
                            .foregroundStyle(n.memPercent == nil ? Hy.faint : load(n.memPercent))
                        Text(n.ready ? "Ready" : "NotReady").font(Hy.mono(12))
                            .foregroundStyle(n.ready ? Hy.faint : Hy.classM)
                    }
                }
            }
            .padding(Hy.s4)
            .frame(maxWidth: .infinity, alignment: .leading)
        }
    }

    private func warnings(_ s: Summary) -> some View {
        Plate(title: "Warnings, last hour", caption: s.warningsLastHour.map(String.init)) {
            VStack(alignment: .leading, spacing: Hy.s3) {
                ForEach(Array((s.recentWarnings ?? []).enumerated()), id: \.offset) { _, w in
                    HStack(alignment: .firstTextBaseline, spacing: Hy.s3) {
                        Text(age(w.lastSeen)).font(Hy.mono(11)).foregroundStyle(Hy.faint)
                            .frame(width: 36, alignment: .leading)
                        Text(w.reason).font(Hy.mono(12)).foregroundStyle(Hy.classK)
                        Text(w.message).font(Hy.ui(12.5)).foregroundStyle(Hy.dust).lineLimit(2)
                        Spacer(minLength: 0)
                    }
                    .help(w.namespace.map { "\(w.object) \u{00b7} \($0)" } ?? w.object)
                }
            }
            .padding(Hy.s4)
            .frame(maxWidth: .infinity, alignment: .leading)
        }
    }

    // MARK: Pieces

    private func figure(_ label: String, _ value: String, _ sub: String, color: Color,
                        open: (() -> Void)? = nil) -> some View {
        VStack(alignment: .leading, spacing: Hy.s1) {
            HyLabel(label)
            Text(value).font(Hy.display(36, 300)).foregroundStyle(color)
            if !sub.isEmpty { Text(sub).font(Hy.mono(11)).foregroundStyle(Hy.faint) }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .contentShape(Rectangle())
        .onTapGesture { open?() }
    }

    private func drill(_ plural: String, _ health: Set<Health>) {
        tab.openTab(context: context, overview: false, kindPlural: plural, health: health)
    }

    // MARK: Formatting

    private func percent(_ used: Int?, _ total: Int?) -> Int? {
        guard let used, let total, total > 0 else { return nil }
        return Int((Double(used) * 100 / Double(total)).rounded())
    }

    private func load(_ p: Int?) -> Color {
        guard let p else { return Hy.starlight }
        return p >= 95 ? Hy.classM : p >= 80 ? Hy.classK : Hy.starlight
    }

    private func count(_ n: Int?) -> String { n.map(String.init) ?? "\u{2013}" }

    private func group(_ n: Int?) -> String {
        guard let n else { return "\u{2013}" }
        return n.formatted(.number.grouping(.automatic))
    }

    private func coreText(_ s: Summary) -> String {
        guard let u = s.cpuUsedMilli, let a = s.cpuAllocatableMilli else { return "" }
        return String(format: "%.1f of %g cores", Double(u) / 1000, Double(a) / 1000)
    }

    private func memText(_ s: Summary) -> String {
        guard let u = s.memUsedBytes, let a = s.memAllocatableBytes else { return "" }
        let gib = 1_073_741_824.0
        return String(format: "%.0f of %.0f GiB", Double(u) / gib, Double(a) / gib)
    }

    private func age(_ rfc3339: String) -> String {
        let f = ISO8601DateFormatter()
        f.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        guard let d = f.date(from: rfc3339) ?? ISO8601DateFormatter().date(from: rfc3339) else { return "" }
        let s = max(0, Int(Date().timeIntervalSince(d)))
        if s < 60 { return "\(s) s" }
        if s < 3600 { return "\(s / 60) m" }
        return "\(s / 3600) h"
    }
}
