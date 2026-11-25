import K10sCore
import SwiftUI

/// Overview of a multi-context tab: one row per cluster (ADR 0007, frame 14).
struct OverviewFleet: View {
    var tab: TabModel
    @Environment(EngineClient.self) private var engine
    @State private var summaries: [String: Summary] = [:]
    @State private var updated: Date?
    @State private var selected: String?
    @FocusState private var focused: Bool

    private struct SubKey: Hashable {
        var contexts: [String]
        var restarts: Int
    }

    private static let gutter: CGFloat = 28
    private static let cols: [(String, CGFloat)] = [
        ("Context", 220), ("Version", 110), ("Nodes", 80), ("CPU", 80), ("Memory", 80), ("Pods", 80),
        ("Failing", 80), ("Degraded", 90), ("Workloads not ready", 190), ("Warnings 1 h", 120),
    ]

    private var contexts: [String] {
        let f = tab.filter.trimmingCharacters(in: .whitespaces).lowercased()
        return tab.scope.contexts.sorted().filter { f.isEmpty || $0.lowercased().contains(f) }
    }

    private func isFailed(_ s: Summary) -> Bool { s.state == .unreachable || s.state == .unauthorized }

    var body: some View {
        let rows = contexts
        VStack(alignment: .leading, spacing: 0) {
            header
            ScrollView(.horizontal) {
                VStack(alignment: .leading, spacing: 0) {
                    HStack(spacing: 0) {
                        Color.clear.frame(width: Self.gutter)
                        ForEach(Self.cols, id: \.0) { c in
                            HyLabel(c.0).lineLimit(1).frame(width: c.1, alignment: .leading)
                        }
                    }
                    .frame(height: 28)
                    Neatline()
                    ScrollView {
                        VStack(alignment: .leading, spacing: 0) {
                            ForEach(rows, id: \.self) { row($0) }
                        }
                    }
                }
                .padding(.horizontal, Hy.s5)
            }
            Text("CPU and memory: used / allocatable, from metrics-server. Orange from 80 %, red from 95 %. Click a number to open that list in a new tab.")
                .font(Hy.mono(12)).foregroundStyle(Hy.faint)
                .padding(.horizontal, Hy.s5).padding(.vertical, Hy.s3)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        .focusable()
        .focused($focused)
        .focusEffectDisabled()
        .onKeyPress(.downArrow) { move(1, rows) }
        .onKeyPress(.upArrow) { move(-1, rows) }
        .onKeyPress(.return) {
            guard let c = selected else { return .ignored }
            open(c)
            return .handled
        }
        .task(id: SubKey(contexts: tab.scope.contexts, restarts: engine.restarts)) { await run() }
    }

    // MARK: data

    @MainActor private func run() async {
        summaries = [:]
        updated = nil
        do {
            let params = OverviewSubscribeParams(contexts: tab.scope.contexts)
            for try await event in try await engine.subscribe(Method.overviewSubscribe, params: params) {
                if event.name == EventName.summary, let s = try? event.decode(Summary.self) {
                    summaries[s.context] = s
                    updated = Date()
                }
            }
        } catch {}
    }

    private func open(_ c: String) {
        tab.openTab(context: c, overview: true, kindPlural: nil, health: nil)
    }

    private func move(_ step: Int, _ rows: [String]) -> KeyPress.Result {
        guard !rows.isEmpty else { return .ignored }
        let cur = selected.flatMap { rows.firstIndex(of: $0) }
        let next = cur.map { min(rows.count - 1, max(0, $0 + step)) } ?? (step > 0 ? 0 : rows.count - 1)
        selected = rows[next]
        return .handled
    }

    // MARK: header

    private var header: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text("Overview").font(Hy.display(24)).foregroundStyle(Hy.starlight)
            TimelineView(.periodic(from: .now, by: 1)) { ctx in
                Text(meta(ctx.date)).font(Hy.mono(12)).foregroundStyle(Hy.faint)
            }
        }
        .padding(.horizontal, Hy.s5).padding(.top, Hy.s4).padding(.bottom, Hy.s3)
    }

    private func meta(_ now: Date) -> String {
        let ok = summaries.values.filter { $0.state == .live }.count
        var line = "\(ok) of \(tab.scope.contexts.count) clusters answer"
        if let u = updated { line += " · updated \(max(0, Int(now.timeIntervalSince(u)))) s ago" }
        return line
    }

    // MARK: rows

    private func row(_ c: String) -> some View {
        let s = summaries[c]
        let isSel = selected == c
        return VStack(spacing: 0) {
            HStack(spacing: 0) {
                ZStack {
                    if let s, let g = GlyphState(s.state).star { StarGlyph(state: g, size: 12) }
                }
                .frame(width: Self.gutter)
                text(c, Hy.starlight, 0)
                if let s, !isFailed(s), s.state == .live {
                    text(s.version ?? "—", Hy.dust, 1)
                    nodes(s)
                    percent(s.cpuUsedMilli.map(Double.init), s.cpuAllocatableMilli.map(Double.init), s, 3)
                    percent(s.memUsedBytes.map(Double.init), s.memAllocatableBytes.map(Double.init), s, 4)
                    text(s.podsTotal.map(String.init) ?? "—", Hy.starlight, 5)
                    count(s.podsFailing, Hy.classM, 6, c, "pods", [.failed])
                    count(s.podsDegraded, Hy.classK, 7, c, "pods", [.degraded])
                    count(s.workloadsNotReady, Hy.classK, 8, c, "deployments", [.pending, .degraded])
                    count(s.warningsLastHour, Hy.dust, 9, c, nil, nil)
                } else {
                    ForEach(1..<Self.cols.count, id: \.self) { text("—", Hy.faint, $0) }
                }
            }
            .frame(height: 31)
            Neatline()
        }
        .background(isSel ? Hy.skyHigh : Color.clear)
        .overlay(alignment: .leading) {
            if isSel { Rectangle().fill(Hy.starlight).frame(width: 2) }
        }
        .contentShape(Rectangle())
        .onTapGesture(count: 2) { selected = c; open(c) }
        .onTapGesture { selected = c; focused = true }
    }

    private func text(_ t: String, _ color: Color, _ col: Int) -> some View {
        Text(t).font(Hy.mono(12.5)).foregroundStyle(color)
            .lineLimit(1).truncationMode(.tail)
            .frame(width: Self.cols[col].1, alignment: .leading)
    }

    private func nodes(_ s: Summary) -> some View {
        guard let r = s.nodesReady, let t = s.nodesTotal else { return text("—", Hy.faint, 2) }
        return text("\(r)/\(t)", r < t ? Hy.classK : Hy.starlight, 2)
    }

    @ViewBuilder
    private func percent(_ used: Double?, _ alloc: Double?, _ s: Summary, _ col: Int) -> some View {
        if !s.metricsAvailable {
            text("n/a", Hy.faint, col).help("metrics-server not found")
        } else if let used, let alloc, alloc > 0 {
            let p = used / alloc * 100
            text("\(Int(p.rounded())) %", p >= 95 ? Hy.classM : p >= 80 ? Hy.classK : Hy.starlight, col)
        } else {
            text("—", Hy.faint, col)
        }
    }

    @ViewBuilder
    private func count(_ n: Int?, _ color: Color, _ col: Int, _ c: String,
                       _ kind: String?, _ health: Set<Health>?) -> some View {
        if let n, n > 0 {
            Button {
                selected = c
                tab.openTab(context: c, overview: kind == nil, kindPlural: kind, health: health)
            } label: {
                text(String(n), color, col)
            }
            .buttonStyle(.plain)
        } else {
            text(n.map(String.init) ?? "—", Hy.faint, col)
        }
    }
}
