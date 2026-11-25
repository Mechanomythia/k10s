import K10sCore
import SwiftUI

/// Overlay slot (T6), shown (top-aligned) when tab.commandBarOpen. Sets tab.commandBarOpen = false to close.
struct CommandBar: View {
    var tab: TabModel
    @Environment(EngineClient.self) private var engine
    @Environment(\.openWindow) private var openWindow
    @Environment(\.replaceScope) private var replaceScope
    @State private var text = ""
    @State private var highlight = 0
    @State private var failed = false
    @State private var allContexts: [String] = []
    @FocusState private var focused: Bool

    var body: some View {
        let kindRows = kindRows()
        let alsoRows = alsoRows()
        let all = kindRows + alsoRows
        return ZStack(alignment: .top) {
            Color(hex: 0x05080c, alpha: 0.72)
                .contentShape(Rectangle())
                .onTapGesture { close() }
            VStack(spacing: 0) {
                HStack(spacing: Hy.s2) {
                    Text(":").font(Hy.mono(16)).foregroundStyle(Hy.dust)
                    TextField("", text: $text)
                        .textFieldStyle(.plain)
                        .font(Hy.mono(16))
                        .foregroundStyle(Hy.starlight)
                        .focused($focused)
                        .onKeyPress(.upArrow) { highlight = max(0, highlight - 1); return .handled }
                        .onKeyPress(.downArrow) { highlight = min(max(all.count - 1, 0), highlight + 1); return .handled }
                        .onKeyPress(.tab) {
                            if all.indices.contains(highlight) { text = all[highlight].complete }
                            return .handled
                        }
                        .onKeyPress(.escape) { close(); return .handled }
                        .onKeyPress(keys: [.return]) { press in
                            run(all, newTab: press.modifiers.contains(.command))
                            return .handled
                        }
                }
                .padding(.horizontal, Hy.s4)
                .frame(height: 48)
                Neatline()
                if failed {
                    Text("No kind or command named \u{201C}\(text.trimmingCharacters(in: .whitespaces))\u{201D}.")
                        .font(Hy.ui(12.5)).foregroundStyle(Hy.faint)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .padding(.horizontal, Hy.s4).padding(.vertical, Hy.s3)
                }
                if !kindRows.isEmpty { group("Kinds", kindRows, offset: 0) }
                if !alsoRows.isEmpty { group("Also try", alsoRows, offset: kindRows.count) }
                Neatline()
                HStack(spacing: Hy.s4) {
                    hint("\u{21B5}", "open"); hint("\u{21E5}", "complete")
                    hint("\u{2318}\u{21B5}", "open in new tab"); hint("esc", "close")
                    Spacer()
                }
                .padding(.horizontal, Hy.s4)
                .frame(height: 32)
            }
            .frame(width: 640)
            .background(Hy.skyHigh)
            .overlay(RoundedRectangle(cornerRadius: Hy.r2).strokeBorder(Hy.neatlineStrong, lineWidth: 1))
            .clipShape(RoundedRectangle(cornerRadius: Hy.r2))
            .shadow(color: .black.opacity(0.5), radius: 16, y: 12)
            .padding(.top, 120)
        }
        .onAppear { focused = true }
        .onChange(of: text) { _, _ in highlight = 0; failed = false }
        .task {
            if let r: ContextsListResult = try? await engine.request(Method.contextsList, Empty()) {
                allContexts = r.contexts.map(\.name)
            }
        }
    }

    // MARK: rows

    private struct Item {
        enum Action { case kind(KindEntry), scope(TabScope), ns(String?) }
        let title: String
        let sub: String
        let right: String
        let complete: String
        let action: Action
        let isKind: Bool
    }

    private var word: String { text.trimmingCharacters(in: .whitespaces).split(separator: " ", maxSplits: 1).first.map { $0.lowercased() } ?? "" }
    private var rest: String {
        let parts = text.trimmingCharacters(in: .whitespaces).split(separator: " ", maxSplits: 1)
        return parts.count > 1 ? parts[1].trimmingCharacters(in: .whitespaces) : ""
    }
    private var hasArg: Bool { text.contains(" ") }

    private func kindRows() -> [Item] {
        guard !hasArg else { return [] }
        return tab.catalog.complete(word).prefix(6).map { k in
            Item(title: k.plural, sub: k.shortNames.joined(separator: " "),
                 right: k.group.isEmpty ? k.version : "\(k.group)/\(k.version)",
                 complete: k.plural, action: .kind(k), isKind: true)
        }
    }

    private func alsoRows() -> [Item] {
        var out: [Item] = []
        let sets = SavedSets().list()
        let ctxs = allContexts.isEmpty ? tab.scope.contexts : allContexts
        func ctx(_ names: [String], _ sub: String, name: String? = nil) -> Item {
            let s = names.joined(separator: ",")
            return Item(title: "ctx \(s)", sub: sub, right: "", complete: "ctx \(s)",
                        action: .scope(TabScope(contexts: names, name: name)), isKind: false)
        }
        func nsItem(_ n: String?) -> Item {
            Item(title: "ns \(n ?? "all")", sub: n == nil ? "all namespaces" : "switch namespace", right: "",
                 complete: "ns \(n ?? "all")", action: .ns(n), isKind: false)
        }
        func setItem(_ s: TabScope) -> Item {
            Item(title: "set \(s.name ?? "")", sub: "open a saved context set", right: "",
                 complete: "set \(s.name ?? "")", action: .scope(s), isKind: false)
        }
        switch word {
        case "ctx":
            let names = rest.split(separator: ",", omittingEmptySubsequences: false).map { $0.trimmingCharacters(in: .whitespaces) }
            if names.count > 1, !rest.isEmpty, names.allSatisfy({ ctxs.contains($0) }) {
                out.append(ctx(names, "read both in this tab"))
            }
            let head = names.dropLast().filter { !$0.isEmpty }
            let last = (names.last ?? "").lowercased()
            for c in ctxs where !head.contains(c) && c.lowercased().contains(last) {
                let all = head + [c]
                out.append(ctx(all, all.count > 1 ? "read both in this tab" : "open \(c) in this tab"))
            }
        case "ns":
            let q = rest.lowercased()
            if "all".hasPrefix(q) { out.append(nsItem(nil)) }
            for n in tab.namespaces where n.lowercased().contains(q) { out.append(nsItem(n)) }
        case "set":
            let q = rest.lowercased()
            for s in sets where (s.name ?? "").lowercased().contains(q) { out.append(setItem(s)) }
        default:
            guard !hasArg else { break }
            if "ctx".hasPrefix(word), let c = ctxs.first { out.append(ctx([c], "open \(c) in this tab")) }
            if "ctx".hasPrefix(word), ctxs.count > 1 { out.append(ctx(Array(ctxs.prefix(2)), "read both in this tab")) }
            if "ns".hasPrefix(word), let n = tab.namespaces.first { out.append(nsItem(n)) }
            if "set".hasPrefix(word), let s = sets.first { out.append(setItem(s)) }
        }
        return Array(out.prefix(8))
    }

    // MARK: actions

    private func close() { tab.commandBarOpen = false }

    private func run(_ rows: [Item], newTab: Bool) {
        // A literal kind (e.g. "dp") wins over fuzzy ordering.
        if !hasArg, let k = tab.catalog.resolve(word), !newTab {
            tab.select(kind: k); close(); return
        }
        guard rows.indices.contains(highlight) else { failed = true; return }
        switch rows[highlight].action {
        case .kind(let k):
            tab.select(kind: k)
        case .scope(let s):
            if newTab { openWindow(value: s) } else { replaceScope(s) }
        case .ns(let n):
            tab.namespace = n
        }
        close()
    }

    // MARK: views

    private func group(_ title: String, _ rows: [Item], offset: Int) -> some View {
        VStack(alignment: .leading, spacing: 0) {
            HyLabel(title).padding(.horizontal, Hy.s4).padding(.top, Hy.s3).padding(.bottom, Hy.s1)
            ForEach(Array(rows.enumerated()), id: \.offset) { i, row in
                let on = offset + i == highlight
                HStack(spacing: Hy.s3) {
                    Text(row.title).font(Hy.mono(13)).foregroundStyle(Hy.starlight)
                    Text(row.sub).font(row.isKind ? Hy.mono(12) : Hy.ui(12.5)).foregroundStyle(Hy.dust)
                    Spacer()
                    Text(row.right).font(Hy.mono(11.5)).foregroundStyle(Hy.faint)
                }
                .padding(.horizontal, Hy.s4)
                .frame(height: 32)
                .background(on ? Hy.skyRaised : .clear)
                .overlay(alignment: .leading) { if on { Rectangle().fill(Hy.starlight).frame(width: 2) } }
                .contentShape(Rectangle())
                .onTapGesture { highlight = offset + i; run(kindRows() + alsoRows(), newTab: false) }
            }
        }
        .padding(.bottom, Hy.s2)
    }

    private func hint(_ key: String, _ label: String) -> some View {
        HStack(spacing: Hy.s1) {
            Text(key).font(Hy.mono(11)).foregroundStyle(Hy.dust)
            Text(label).font(Hy.ui(11.5)).foregroundStyle(Hy.faint)
        }
    }
}
