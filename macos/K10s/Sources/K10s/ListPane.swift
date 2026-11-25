import K10sCore
import SwiftUI

/// Center slot (T4). Subscribes table.subscribe for tab.scope/kind/namespace, feeds tab.table,
/// forwards context.state events to tab.apply(_:), sets tab.selection / tab.inspectorOpen.
struct ListPane: View {
    var tab: TabModel
    @Environment(EngineClient.self) private var engine
    @State private var loaded = false
    @State private var showLoading = false
    @State private var retry = 0
    @FocusState private var focused: Bool

    private struct SubKey: Hashable {
        var kind: KindEntry?
        var namespace: String?
        var selector: String?
        var restarts: Int
        var retry: Int
    }

    private var key: SubKey {
        SubKey(kind: tab.kind, namespace: tab.namespace, selector: tab.table.labelSelector,
               restarts: engine.restarts, retry: retry)
    }

    private func isFailed(_ c: String) -> Bool {
        let s = tab.health.state(of: c)
        return s == .unreachable || s == .unauthorized
    }

    private func rid(_ r: Row) -> String { r.context + "\u{0}" + r.uid }

    var body: some View {
        let failing = tab.scope.contexts.filter(isFailed)
        Group {
            if tab.scope.contexts.count == 1, let c = failing.first, let st = tab.health.statuses[c] {
                ContextErrorView(context: c, status: st)
            } else if let kind = tab.kind {
                content(kind: kind, failing: failing)
            } else {
                Color.clear
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(Hy.sky)
        .task(id: key) { await run() }
        .task(id: loaded) {
            guard !loaded else { return }
            try? await Task.sleep(nanoseconds: 150_000_000)
            if !loaded { showLoading = true }
        }
    }

    // MARK: subscription

    @MainActor private func run() async {
        guard let kind = tab.kind else { return }
        var t = TableModel(contexts: tab.scope.contexts)
        t.filter = tab.table.filter
        t.wide = tab.table.wide
        t.sortColumn = tab.table.sortColumn
        t.ascending = tab.table.ascending
        tab.table = t
        loaded = false
        showLoading = false
        var params = TableSubscribeParams(contexts: tab.scope.contexts, resource: kind.ref,
                                          labelSelector: tab.table.labelSelector)
        if kind.namespaced {
            if let ns = tab.namespace { params.namespace = ns } else { params.allNamespaces = true }
        }
        do {
            for try await event in try await engine.subscribe(Method.tableSubscribe, params: params) {
                switch event.name {
                case EventName.snapshot:
                    if let s = try? event.decode(Snapshot.self) { tab.table.apply(s); loaded = true }
                case EventName.upsert:
                    if let u = try? event.decode(Upsert.self) { tab.table.apply(u) }
                case EventName.delete:
                    if let d = try? event.decode(Delete.self) { tab.table.apply(d) }
                case EventName.contextState:
                    if let c = try? event.decode(ContextStateChange.self) { tab.apply(c) }
                default:
                    break
                }
            }
        } catch {}
    }

    // MARK: content

    @ViewBuilder
    private func content(kind: KindEntry, failing: [String]) -> some View {
        let total = tab.table.totalCount
        VStack(alignment: .leading, spacing: 0) {
            header(kind: kind, total: total, failed: failing.count)
            if tab.scope.contexts.count > 1 {
                ForEach(failing, id: \.self) { notice($0) }
            }
            if !loaded {
                if showLoading {
                    Text("Loading…").font(Hy.mono(12)).foregroundStyle(Hy.faint)
                        .padding(.horizontal, Hy.s5).padding(.top, Hy.s2)
                }
                Spacer(minLength: 0)
            } else if total == 0 && tab.table.filter.isEmpty {
                EmptyKindView(kind: kind, namespace: kind.namespaced ? tab.namespace : nil)
            } else {
                grid
            }
        }
    }

    private func header(kind: KindEntry, total: Int, failed: Int) -> some View {
        let n = tab.scope.contexts.count
        let filter = tab.table.filter.trimmingCharacters(in: .whitespaces)
        var parts: [String] = []
        if !filter.isEmpty {
            parts.append("\(tab.table.filteredCount) of \(total) match “\(filter)”")
        } else {
            let ns = kind.namespaced ? " in \(tab.namespace ?? "all namespaces")" : ""
            parts.append("\(total)\(ns) · watching")
        }
        if failed > 0 && n > 1 { parts.append("\(n - failed) of \(n) clusters") }
        return VStack(alignment: .leading, spacing: 2) {
            Text(kind.kind.isEmpty ? kind.plural : kind.plural.capitalized)
                .font(Hy.display(24)).foregroundStyle(Hy.starlight)
            Text(parts.joined(separator: " · "))
                .font(Hy.mono(12)).foregroundStyle(Hy.faint)
        }
        .padding(.horizontal, Hy.s5).padding(.top, Hy.s4).padding(.bottom, Hy.s3)
    }

    private func notice(_ ctx: String) -> some View {
        let st = tab.health.statuses[ctx]
        let line = st?.state == .unauthorized
            ? "\(ctx) refused your credentials. Its rows are left out."
            : "\(ctx) is unreachable. Its rows are left out."
        return HStack(alignment: .top, spacing: Hy.s3) {
            StarGlyph(state: .failed, size: 16)
            VStack(alignment: .leading, spacing: 2) {
                Text(line).font(Hy.ui(13, 700)).foregroundStyle(Hy.starlight)
                if let e = st?.error, !e.isEmpty {
                    Text(e).font(Hy.mono(12)).foregroundStyle(Hy.faint)
                }
            }
            Spacer(minLength: Hy.s3)
            Button("Retry now") { retry += 1 }.buttonStyle(HyButton(kind: .secondary, small: true))
        }
        .padding(.vertical, Hy.s3).padding(.horizontal, Hy.s4)
        .background(Hy.skyRaised)
        .overlay(Rectangle().strokeBorder(Hy.neatline, lineWidth: 1))
        .padding(.horizontal, Hy.s5).padding(.bottom, Hy.s3)
    }

    // MARK: grid

    private static let gutter: CGFloat = 28

    private func widths(columns: [String], rows: [Row]) -> [CGFloat] {
        let sample = rows.prefix(50).map { tab.table.cells(of: $0) }
        return columns.indices.map { i in
            var longest = columns[i].count + 2
            for cells in sample where i < cells.count { longest = max(longest, cells[i].count) }
            return min(260, max(80, CGFloat(longest) * 9))
        }
    }

    private var grid: some View {
        let columns = tab.table.columns
        let rows = tab.table.rows
        var w = widths(columns: columns, rows: rows)
        let nameIdx = columns.firstIndex(of: "Name") ?? 0
        return GeometryReader { geo in
            let others = w.enumerated().filter { $0.offset != nameIdx }.reduce(0) { $0 + $1.element }
            let avail = geo.size.width - 2 * Hy.s5 - Self.gutter - others
            let _ = { if nameIdx < w.count { w[nameIdx] = max(w[nameIdx], avail) } }()
            ScrollView(.horizontal) {
                VStack(alignment: .leading, spacing: 0) {
                    headerRow(columns: columns, widths: w)
                    ScrollView {
                        LazyVStack(alignment: .leading, spacing: 0) {
                            ForEach(rows, id: \.uid) { row in
                                dataRow(row, widths: w, rows: rows)
                            }
                        }
                    }
                }
                .padding(.horizontal, Hy.s5)
                .frame(minWidth: geo.size.width, alignment: .leading)
            }
        }
        .focusable()
        .focused($focused)
        .focusEffectDisabled()
        .onKeyPress(phases: [.down, .repeat]) { press in
            handleKey(press, rows: tab.table.rows)
        }
    }

    private func headerRow(columns: [String], widths: [CGFloat]) -> some View {
        VStack(spacing: 0) {
            HStack(spacing: 0) {
                Color.clear.frame(width: Self.gutter)
                ForEach(Array(columns.enumerated()), id: \.offset) { i, name in
                    Button { sort(by: name) } label: {
                        HStack(spacing: 4) {
                            HyLabel(name)
                            if tab.table.sortColumn == name {
                                Text(tab.table.ascending ? "▲" : "▼").font(Hy.ui(9)).foregroundStyle(Hy.dust)
                            }
                            Spacer(minLength: 0)
                        }
                        .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    .frame(width: widths[i], alignment: .leading)
                }
            }
            .frame(height: 28)
            Neatline()
        }
    }

    private func sort(by name: String) {
        if tab.table.sortColumn == name {
            tab.table.ascending.toggle()
        } else {
            tab.table.sortColumn = name
            tab.table.ascending = true
        }
    }

    private func dataRow(_ row: Row, widths: [CGFloat], rows: [Row]) -> some View {
        let selected = tab.selection.map { rid($0) == rid(row) } ?? false
        let cells = tab.table.cells(of: row)
        let multi = tab.scope.contexts.count > 1
        return VStack(spacing: 0) {
            HStack(spacing: 0) {
                ZStack {
                    if let s = GlyphState(row.health).star { StarGlyph(state: s, size: 12) }
                }
                .frame(width: Self.gutter)
                ForEach(Array(cells.enumerated()), id: \.offset) { i, cell in
                    Text(cell)
                        .font(Hy.mono(12.5))
                        .foregroundStyle(multi && i == 0 ? Hy.dust : Hy.starlight)
                        .lineLimit(1).truncationMode(.tail)
                        .frame(width: i < widths.count ? widths[i] : 80, alignment: .leading)
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
        .onTapGesture(count: 2) { open(row) }
        .onTapGesture { tab.selection = row; focused = true }
    }

    private func open(_ row: Row) {
        tab.selection = row
        tab.inspectorTab = .describe
        tab.inspectorOpen = true
    }

    private func handleKey(_ press: KeyPress, rows: [Row]) -> KeyPress.Result {
        if press.key == .return {
            guard let s = tab.selection else { return .ignored }
            open(s)
            return .handled
        }
        var step = 0
        if press.key == .downArrow || press.characters == "j" { step = 1 }
        else if press.key == .upArrow || press.characters == "k" { step = -1 }
        guard step != 0, !rows.isEmpty else { return .ignored }
        let cur = tab.selection.flatMap { s in rows.firstIndex { rid($0) == rid(s) } }
        let next = cur.map { min(rows.count - 1, max(0, $0 + step)) } ?? (step > 0 ? 0 : rows.count - 1)
        tab.selection = rows[next]
        return .handled
    }
}
