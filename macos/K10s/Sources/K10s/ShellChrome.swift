import K10sCore
import SwiftUI

struct Kbd: View {
    let key: String
    init(_ key: String) { self.key = key }
    var body: some View {
        Text(key)
            .font(Hy.mono(11))
            .foregroundStyle(Hy.starlight)
            .padding(.horizontal, 5)
            .padding(.vertical, 1)
            .overlay(RoundedRectangle(cornerRadius: Hy.r1).stroke(Hy.neatlineStrong, lineWidth: 1))
    }
}

/// 52 px: contexts with stars, namespace, filter, ":" hint.
struct ToolbarView: View {
    @Bindable var tab: TabModel
    @Environment(Protection.self) private var protection
    @FocusState private var filterFocused: Bool

    private var multi: Bool { tab.scope.contexts.count > 1 }

    var body: some View {
        HStack(spacing: Hy.s4) {
            VStack(alignment: .leading, spacing: 2) {
                HyLabel(multi ? "Contexts" : "Context")
                HStack(spacing: Hy.s3) {
                    ForEach(tab.scope.contexts, id: \.self) { name in
                        HStack(spacing: 6) {
                            if let s = GlyphState(tab.health.state(of: name)).star {
                                StarGlyph(state: s, size: 12)
                            }
                            Text(name).font(Hy.mono(13)).foregroundStyle(Hy.starlight).lineLimit(1)
                            if protection.isProtected(name) { HyTag(text: "protected") }
                        }
                        .contextMenu {
                            if !protection.isProtected(name) {
                                Button("Protect") { protection.setOverride(name, .protected) }
                            }
                            if protection.isProtected(name) {
                                Button("Stop protecting") { protection.setOverride(name, .notProtected) }
                            }
                            Button("Use automatic") { protection.setOverride(name, nil) }
                        }
                    }
                    if multi {
                        Button("Edit set") { tab.newTabOpen = true }
                            .buttonStyle(HyButton(kind: .secondary, small: true))
                    }
                }
            }
            Spacer(minLength: Hy.s3)
            VStack(alignment: .leading, spacing: 2) {
                HyLabel("Namespace")
                Picker("Namespace", selection: $tab.namespace) {
                    Text("all").tag(String?.none)
                    ForEach(tab.namespaces, id: \.self) { Text($0).tag(String?.some($0)) }
                }
                .pickerStyle(.menu)
                .labelsHidden()
                .font(Hy.mono(12))
                .frame(width: 160)
            }
            TextField("/ filter by name or label", text: $tab.filter)
                .textFieldStyle(.plain)
                .font(Hy.mono(12))
                .foregroundStyle(Hy.starlight)
                .focused($filterFocused)
                .padding(.horizontal, Hy.s2)
                .frame(width: 260, height: 28)
                .background(Hy.sky)
                .overlay(RoundedRectangle(cornerRadius: Hy.r1).stroke(filterFocused ? Hy.neatlineStrong : Hy.neatline, lineWidth: 1))
            Kbd(":")
        }
        // Room for the traffic lights of the hidden title bar.
        .padding(.leading, 76)
        .padding(.trailing, Hy.s4)
        .frame(height: 52)
        .background(Hy.skyRaised)
        .onChange(of: tab.focusFilter) { _, want in
            if want {
                filterFocused = true
                tab.focusFilter = false
            }
        }
    }
}

/// 208 px kinds list; counts only for the selected kind.
struct KindsSidebar: View {
    @Bindable var tab: TabModel

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: Hy.s4) {
                overviewRow
                ForEach(tab.catalog.sections, id: \.title) { section in
                    VStack(alignment: .leading, spacing: 2) {
                        HyLabel(section.title)
                            .padding(.horizontal, Hy.s3)
                            .padding(.bottom, Hy.s1)
                        ForEach(section.kinds, id: \.self) { entry in
                            row(entry, custom: section.title == "Custom resources")
                        }
                    }
                }
            }
            .padding(.vertical, Hy.s3)
        }
        .frame(width: 208)
        .background(Hy.skyRaised)
    }

    private var overviewRow: some View {
        Button { tab.showingOverview = true } label: {
            HStack {
                Text("Overview")
                    .font(Hy.ui(13))
                    .foregroundStyle(tab.showingOverview ? Hy.starlight : Hy.dust)
                Spacer()
            }
            .padding(.horizontal, Hy.s3)
            .frame(height: 28)
            .background(tab.showingOverview ? Hy.skyHigh : Color.clear)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }

    private func row(_ entry: KindEntry, custom: Bool) -> some View {
        let selected = tab.kind == entry && !tab.showingOverview
        return Button { tab.select(kind: entry) } label: {
            HStack {
                Text(custom ? entry.plural : entry.plural.capitalized)
                    .font(Hy.ui(13))
                    .foregroundStyle(selected ? Hy.starlight : Hy.dust)
                    .lineLimit(1)
                Spacer()
                if selected {
                    Text("\(tab.table.totalCount)").font(Hy.mono(11)).foregroundStyle(Hy.faint)
                }
            }
            .padding(.horizontal, Hy.s3)
            .frame(height: 28)
            .background(selected ? Hy.skyHigh : Color.clear)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }
}

/// 28 px: key hints left, scope facts right.
struct StatusBar: View {
    var tab: TabModel
    @Environment(Protection.self) private var protection

    private let hints = [("d", "describe"), ("y", "yaml"), ("l", "logs"), ("/", "filter"), (":", "command"), ("esc", "back")]

    private var summary: String {
        let names = tab.scope.contexts
        if names.count == 1, let name = names.first {
            var parts = [name]
            if let c = tab.contexts.first(where: { $0.name == name }) { parts.append(c.cluster) }
            if let v = tab.health.statuses[name]?.version { parts.append(v) }
            if protection.isProtected(name) { parts.append("protected") }
            return parts.joined(separator: " · ")
        }
        var text = "\(tab.scope.displayName) · \(names.joined(separator: ", "))"
        if names.contains(where: protection.isProtected) { text += " · protected" }
        return text
    }

    var body: some View {
        HStack(spacing: Hy.s4) {
            ForEach(hints, id: \.0) { key, label in
                HStack(spacing: 6) {
                    Kbd(key)
                    Text(label).font(Hy.ui(12)).foregroundStyle(Hy.dust)
                }
            }
            Spacer()
            Text(summary).font(Hy.mono(11)).foregroundStyle(Hy.faint).lineLimit(1)
        }
        .padding(.horizontal, Hy.s4)
        .frame(height: 28)
        .background(Hy.skyRaised)
    }
}
