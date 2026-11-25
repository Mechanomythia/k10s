import K10sCore
import SwiftUI

/// Sheet slot (T7), presented when tab.newTabOpen. Opens a scope with openWindow(value: TabScope)
/// and sets tab.newTabOpen = false. Prefilled with tab.scope (also serves "Edit set").
struct NewTabSheet: View {
    var tab: TabModel
    @Environment(EngineClient.self) private var engine
    @Environment(\.openWindow) private var openWindow
    @State private var all: [ContextInfo] = []
    @State private var picked: Set<String> = []
    @State private var filter = ""
    @State private var setName = ""
    private let sets = SavedSets()

    private var visible: [ContextInfo] {
        let q = filter.trimmingCharacters(in: .whitespaces).lowercased()
        return q.isEmpty ? all : all.filter { $0.name.lowercased().contains(q) || $0.cluster.lowercased().contains(q) }
    }

    /// Picked contexts in contexts.list order.
    private var pickedList: [String] { all.map(\.name).filter(picked.contains) }

    private var openTitle: String {
        pickedList.count == 1 ? "Open \(pickedList[0])" : "Open \(pickedList.count) contexts"
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            VStack(alignment: .leading, spacing: Hy.s2) {
                Text("Open contexts in a new tab").font(Hy.display(24)).foregroundStyle(Hy.starlight)
                Text("Pick one context to work like k9s, or several to read them side by side. Nothing connects until the tab opens.")
                    .font(Hy.ui(13)).foregroundStyle(Hy.dust)
            }
            .padding(Hy.s5)
            Neatline()
            HStack(spacing: 0) {
                setsColumn.frame(width: 240)
                Rectangle().fill(Hy.neatline).frame(width: 1)
                contextsColumn
            }
            .frame(height: 340)
            Neatline()
            HStack(alignment: .bottom, spacing: Hy.s3) {
                VStack(alignment: .leading, spacing: Hy.s1) {
                    HyLabel("Save as set (optional)")
                    TextField("", text: $setName)
                        .textFieldStyle(.plain)
                        .font(Hy.ui(13)).foregroundStyle(Hy.starlight)
                        .padding(.horizontal, Hy.s2)
                        .frame(width: 220, height: 28)
                        .overlay(Rectangle().strokeBorder(Hy.neatlineStrong, lineWidth: 1))
                }
                Spacer()
                Button("Cancel") { tab.newTabOpen = false }
                    .buttonStyle(HyButton(kind: .quiet))
                    .keyboardShortcut(.cancelAction)
                Button(openTitle) { open() }
                    .buttonStyle(HyButton(kind: .primary))
                    .disabled(picked.isEmpty)
                    .keyboardShortcut(.defaultAction)
            }
            .padding(Hy.s4)
        }
        .frame(width: 880)
        .background(Hy.skyRaised)
        .task {
            picked = Set(tab.scope.contexts)
            let r: ContextsListResult? = try? await engine.request(Method.contextsList, Empty())
            all = r?.contexts ?? []
        }
    }

    private var setsColumn: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: Hy.s1) {
                HyLabel("Saved sets").padding(.bottom, Hy.s1)
                ForEach(sets.list(), id: \.self) { scopeRow($0, title: $0.name ?? "") }
                HyLabel("Recent").padding(.top, Hy.s4).padding(.bottom, Hy.s1)
                ForEach(sets.recent, id: \.self) { scopeRow($0, title: $0.displayName) }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(Hy.s3)
        }
    }

    private func scopeRow(_ scope: TabScope, title: String) -> some View {
        Button {
            picked = Set(scope.contexts)
            setName = scope.name ?? ""
        } label: {
            VStack(alignment: .leading, spacing: 2) {
                Text(title).font(Hy.ui(13)).foregroundStyle(Hy.starlight)
                Text(scope.contexts.joined(separator: ", "))
                    .font(Hy.mono(11)).foregroundStyle(Hy.faint).lineLimit(2)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(Hy.s2)
            .background(Set(scope.contexts) == picked ? Hy.skyHigh : Color.clear)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }

    private var contextsColumn: some View {
        VStack(spacing: 0) {
            HStack {
                HyLabel("Contexts")
                Spacer()
                TextField("filter", text: $filter)
                    .textFieldStyle(.plain)
                    .font(Hy.ui(13)).foregroundStyle(Hy.starlight)
                    .padding(.horizontal, Hy.s2)
                    .frame(width: 200, height: 28)
                    .overlay(Rectangle().strokeBorder(Hy.neatlineStrong, lineWidth: 1))
            }
            .padding(Hy.s3)
            Neatline()
            ScrollView {
                LazyVStack(spacing: 0) {
                    ForEach(visible, id: \.name) { c in
                        row(c)
                        Neatline()
                    }
                }
            }
        }
    }

    private func note(_ c: ContextInfo) -> String {
        if tab.scope.contexts.contains(c.name) { return "in this tab" }
        switch tab.health.statuses[c.name]?.state {
        case .unreachable: return "unreachable"
        case .unauthorized: return "unauthorized"
        default: return "not connected"
        }
    }

    private func row(_ c: ContextInfo) -> some View {
        let binding = Binding<Bool>(
            get: { picked.contains(c.name) },
            set: { on in if on { picked.insert(c.name) } else { picked.remove(c.name) } }
        )
        let star = tab.health.statuses[c.name].flatMap { GlyphState($0.state).star }
        return HStack(spacing: Hy.s3) {
            Toggle("", isOn: binding).toggleStyle(.checkbox).labelsHidden()
            Group {
                if let star { StarGlyph(state: star, size: 12) } else { Color.clear }
            }
            .frame(width: 12, height: 12)
            Text(c.name).font(Hy.mono(13)).foregroundStyle(Hy.starlight).lineLimit(1)
            Text(c.cluster).font(Hy.mono(12)).foregroundStyle(Hy.faint).lineLimit(1).truncationMode(.middle)
            Spacer(minLength: Hy.s2)
            Text(note(c)).font(Hy.ui(12)).foregroundStyle(Hy.dust)
        }
        .padding(.horizontal, Hy.s3)
        .frame(height: 36)
        .contentShape(Rectangle())
        .onTapGesture { binding.wrappedValue.toggle() }
    }

    private func open() {
        let contexts = pickedList
        guard !contexts.isEmpty else { return }
        let name = setName.trimmingCharacters(in: .whitespaces)
        if !name.isEmpty { sets.save(name: name, contexts: contexts) }
        sets.noteRecent(TabScope(contexts: contexts, name: name.isEmpty ? nil : name))
        openWindow(value: TabScope(contexts: contexts, name: name.isEmpty ? nil : name))
        tab.newTabOpen = false
    }
}
