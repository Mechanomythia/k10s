import K10sCore
import SwiftUI

/// The list area: the tab's own list, with a child list or the containers sliding in over it
/// for each level. Lower levels stay alive underneath, so they stay subscribed.
struct LevelsView: View {
    var tab: TabModel

    var body: some View {
        ZStack {
            ListPane(tab: tab)
            ForEach(Array(tab.levels.levels.enumerated()), id: \.offset) { i, level in
                ZStack {
                    if case .table = level.children {
                        ListPane(tab: tab, depth: i + 1, level: level)
                    } else {
                        ContainersPane(tab: tab, level: level)
                    }
                }
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .background(Hy.sky)
                .transition(.move(edge: .trailing))
            }
        }
        .overlay(alignment: .topLeading) { status }
        .clipped()
    }

    @ViewBuilder private var status: some View {
        if tab.diving {
            Text("Loading…").font(Hy.mono(12)).foregroundStyle(Hy.faint)
                .padding(.horizontal, Hy.s5).padding(.top, 76)
        } else if let error = tab.diveError {
            VStack(alignment: .leading, spacing: 2) {
                Text("k10s could not list the children.").font(Hy.ui(13, 700)).foregroundStyle(Hy.starlight)
                Text(error).font(Hy.mono(12)).foregroundStyle(Hy.faint)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(.vertical, Hy.s3).padding(.horizontal, Hy.s4)
            .background(Hy.skyRaised)
            .overlay(Rectangle().strokeBorder(Hy.neatline, lineWidth: 1))
            .padding(.horizontal, Hy.s5).padding(.top, Hy.s4)
        }
    }
}

/// The containers of a pod (ADR 0006, decision 7): the list grid with fixed columns.
/// Keys and selection live in TabModel (WindowKeys), so this only shows and clicks.
struct ContainersPane: View {
    var tab: TabModel
    let level: Level
    @Environment(EngineClient.self) private var engine
    @State private var loaded = false
    @State private var error: String?

    private static let names = ["Name", "Image", "Ready", "State", "Restarts"]
    private static let widths: [CGFloat] = [0, 320, 70, 220, 90]
    private static let gutter: CGFloat = 28

    private var parent: Row { level.parent }

    /// Changes whenever the pod's row in the list below changes.
    private var signature: String {
        guard let r = tab.table.rows.first(where: { $0.context == parent.context && $0.uid == parent.uid }) else { return "" }
        return r.cells.joined(separator: "|") + "|\(r.health)"
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            header
            if let error {
                Text(error).font(Hy.mono(12)).foregroundStyle(Hy.faint)
                    .padding(.horizontal, Hy.s5).padding(.top, Hy.s2)
                Spacer(minLength: 0)
            } else if !loaded {
                Spacer(minLength: 0)
            } else {
                grid
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        .task(id: signature) { await load() }
    }

    private var header: some View {
        let ns = parent.namespace.map { " in \($0)" } ?? ""
        let path = tab.breadcrumb(upTo: tab.levels.levels.count)
        return VStack(alignment: .leading, spacing: 2) {
            Text(tab.scope.contexts.count > 1 ? path + " · " + parent.context : path)
                .font(Hy.mono(12)).foregroundStyle(Hy.dust).lineLimit(1).truncationMode(.middle)
            Text("Containers").font(Hy.display(24)).foregroundStyle(Hy.starlight)
            Text("\(tab.containers.count)\(ns) · watching")
                .font(Hy.mono(12)).foregroundStyle(Hy.faint)
        }
        .padding(.horizontal, Hy.s5).padding(.top, Hy.s4).padding(.bottom, Hy.s3)
    }

    @MainActor private func load() async {
        let p = PodContainersParams(context: parent.context, namespace: parent.namespace ?? "", name: parent.name)
        do {
            let r: PodContainersResult = try await engine.request(Method.podContainers, p)
            if Task.isCancelled { return }
            tab.containers = r.statuses.filter { $0.isInit != true } + r.statuses.filter { $0.isInit == true }
            if !tab.containers.contains(where: { $0.name == tab.selectedContainer }) {
                tab.selectedContainer = tab.containers.first?.name
            }
            error = nil
        } catch {
            if !Task.isCancelled { self.error = error.localizedDescription }
        }
        loaded = true
    }

    private func star(_ state: String) -> StarGlyph.State {
        if state.hasPrefix("Running") { return .live }
        if state.hasPrefix("Waiting") { return .building }
        if state.hasPrefix("Terminated") { return state == "Terminated: Completed" ? .stopped : .failed }
        return .stopped
    }

    private func sized<V: View>(_ v: V, _ i: Int) -> some View {
        v.frame(minWidth: i == 0 ? 160 : Self.widths[i], maxWidth: i == 0 ? .infinity : Self.widths[i], alignment: .leading)
    }

    private var grid: some View {
        VStack(alignment: .leading, spacing: 0) {
            VStack(spacing: 0) {
                HStack(spacing: 0) {
                    Color.clear.frame(width: Self.gutter)
                    ForEach(Array(Self.names.enumerated()), id: \.offset) { i, name in
                        sized(HyLabel(name), i)
                    }
                }
                .frame(height: 28)
                Neatline()
            }
            ScrollView {
                LazyVStack(alignment: .leading, spacing: 0) {
                    ForEach(tab.containers, id: \.name) { c in containerRow(c) }
                }
            }
        }
        .padding(.horizontal, Hy.s5)
    }

    private func containerRow(_ c: ContainerStatus) -> some View {
        let selected = tab.selectedContainer == c.name
        let values = [c.image, c.ready ? "yes" : "no", c.state, String(c.restarts)]
        return VStack(spacing: 0) {
            HStack(spacing: 0) {
                StarGlyph(state: star(c.state), size: 12).frame(width: Self.gutter)
                sized(HStack(spacing: Hy.s2) {
                    Text(c.name).font(Hy.mono(12.5)).foregroundStyle(Hy.starlight).lineLimit(1).truncationMode(.tail)
                    if c.isInit == true { HyTag(text: "init") }
                }, 0)
                ForEach(Array(values.enumerated()), id: \.offset) { i, v in
                    sized(Text(v).font(Hy.mono(12.5)).foregroundStyle(Hy.starlight).lineLimit(1).truncationMode(.tail), i + 1)
                }
            }
            .frame(height: 31)
            Neatline()
        }
        .background(selected ? Hy.skyHigh : Color.clear)
        .overlay(alignment: .leading) {
            if selected { Rectangle().fill(Hy.starlight).frame(width: 2) }
        }
        .contentShape(Rectangle())
        .onTapGesture(count: 2) { tab.selectedContainer = c.name; _ = tab.openInspector(.logs) }
        .onTapGesture { tab.selectedContainer = c.name }
    }
}
