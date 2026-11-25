import K10sCore
import SwiftUI

/// Right slot (T5), shown when tab.inspectorOpen. The shell sets its width (480, 720 for logs); fill the height.
/// Reads tab.selection and tab.inspectorTab.
struct InspectorPane: View {
    var tab: TabModel
    @Environment(EngineClient.self) private var engine

    @State private var text = ""
    @State private var events: [ObjectEvent] = []
    @State private var error: String?
    @State private var loaded = false

    private var isPod: Bool { tab.activeKind?.kind == "Pod" && tab.activeKind?.group.isEmpty == true }
    private var current: InspectorTab { (tab.inspectorTab == .logs && !isPod) ? .describe : tab.inspectorTab }
    private var tabs: [InspectorTab] { isPod ? InspectorTab.allCases : [.describe, .yaml, .events] }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            if let row = tab.activeSelection {
                header(row)
                content(row)
            } else {
                Text("Select a row.").font(Hy.mono(12)).foregroundStyle(Hy.faint).padding(Hy.s4)
                Spacer(minLength: 0)
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        .background(Hy.skyRaised)
        .focusable()
        .focusEffectDisabled()
        .onKeyPress { press in
            if press.key == .escape { tab.inspectorOpen = false; return .handled }
            guard press.modifiers.isEmpty else { return .ignored }
            switch press.characters {
            case "d": tab.inspectorTab = .describe
            case "y": tab.inspectorTab = .yaml
            case "v": tab.inspectorTab = .events
            case "l": if isPod { tab.inspectorTab = .logs } else { return .ignored }
            default: return .ignored
            }
            return .handled
        }
    }

    private func header(_ row: Row) -> some View {
        VStack(alignment: .leading, spacing: 0) {
            Text(row.name).font(Hy.mono(14, 500)).foregroundStyle(Hy.starlight).lineLimit(1).truncationMode(.middle)
            Text([tab.activeKind?.kind, row.namespace, row.context].compactMap { $0 }.filter { !$0.isEmpty }.joined(separator: " · "))
                .font(Hy.mono(11.5)).foregroundStyle(Hy.faint).lineLimit(1).padding(.top, 2)
            HStack(spacing: Hy.s4) {
                ForEach(tabs, id: \.self) { t in
                    Button { tab.inspectorTab = t } label: {
                        Text(t == .yaml && tab.editing ? "YAML · editing" : t.rawValue.capitalized)
                            .font(Hy.ui(13, 600))
                            .foregroundStyle(t == current ? Hy.starlight : Hy.dust)
                            .padding(.vertical, Hy.s2)
                            .overlay(alignment: .bottom) {
                                if t == current { Rectangle().fill(Hy.starlight).frame(height: 2) }
                            }
                    }
                    .buttonStyle(.plain)
                }
                Spacer()
            }
            .padding(.top, Hy.s3)
            .overlay(alignment: .bottom) { Neatline() }
        }
        .padding(EdgeInsets(top: 16, leading: 16, bottom: 0, trailing: 16))
    }

    @ViewBuilder private func content(_ row: Row) -> some View {
        switch current {
        case .logs:
            LogsView(row: row, tab: tab)
        case .events:
            ScrollView {
                VStack(alignment: .leading, spacing: 0) {
                    if let error { errorLine(error) }
                    else if loaded && events.isEmpty { Text("No events.").font(Hy.mono(12)).foregroundStyle(Hy.faint) }
                    ForEach(Array(events.enumerated()), id: \.offset) { _, e in eventRow(e) }
                }
                .frame(maxWidth: .infinity, alignment: .leading).padding(Hy.s4)
            }
            .task(id: "\(row.uid)|events") { await loadEvents(row) }
        case .yaml where tab.editing:
            YamlEditor(tab: tab, row: row)
        case .describe, .yaml:
            let yaml = current == .yaml
            ScrollView {
                Group {
                    if let error { errorLine(error) }
                    else { Text(Self.colored(text, describe: !yaml)).font(Hy.mono(12)).lineSpacing(5).textSelection(.enabled) }
                }
                .frame(maxWidth: .infinity, alignment: .leading).padding(Hy.s4)
            }
            .task(id: "\(row.uid)|\(current.rawValue)") { await loadText(row, yaml: yaml) }
        }
    }

    private func errorLine(_ s: String) -> some View {
        Text(s).font(Hy.mono(12)).foregroundStyle(Hy.faint)
    }

    private func eventRow(_ e: ObjectEvent) -> some View {
        VStack(alignment: .leading, spacing: 2) {
            HStack(spacing: Hy.s3) {
                Text(e.type).foregroundStyle(e.type == "Warning" ? Hy.classK : Hy.dust)
                Text(e.reason).foregroundStyle(Hy.starlight)
                Spacer()
                Text("×\(e.count)").foregroundStyle(Hy.faint)
                Text(Self.relative(e.lastSeen)).foregroundStyle(Hy.faint)
            }
            Text(e.message).foregroundStyle(Hy.dust).fixedSize(horizontal: false, vertical: true)
        }
        .font(Hy.mono(12))
        .padding(.vertical, Hy.s2)
        .frame(maxWidth: .infinity, alignment: .leading)
        .overlay(alignment: .bottom) { Neatline() }
    }

    // MARK: loading

    private func params(_ row: Row) -> InspectorObjectParams {
        InspectorObjectParams(context: row.context, resource: tab.activeKind?.ref ?? ResourceRef(version: "v1", plural: "pods"),
                              namespace: row.namespace, name: row.name)
    }

    private func loadText(_ row: Row, yaml: Bool) async {
        text = ""; error = nil
        do {
            let r: TextResult = try await engine.request(yaml ? Method.objectYAML : Method.objectDescribe, params(row))
            if !Task.isCancelled { text = r.text }
        } catch { if !Task.isCancelled { self.error = error.localizedDescription } }
    }

    private func loadEvents(_ row: Row) async {
        events = []; error = nil; loaded = false
        do {
            let r: ObjectEventsResult = try await engine.request(
                Method.objectEvents, InspectorEventsParams(context: row.context, namespace: row.namespace, uid: row.uid))
            if !Task.isCancelled { events = r.events; loaded = true }
        } catch { if !Task.isCancelled { self.error = error.localizedDescription } }
    }

    // MARK: formatting

    /// Text before the first ':' on each line in dust, the rest starlight.
    static func colored(_ s: String, describe: Bool) -> AttributedString {
        var out = AttributedString()
        let lines = s.split(separator: "\n", omittingEmptySubsequences: false)
        for (i, line) in lines.enumerated() {
            var head = AttributedString(), tail = AttributedString()
            if let c = line.firstIndex(of: ":") {
                head = AttributedString(String(line[...c]))
                tail = AttributedString(String(line[line.index(after: c)...]))
            } else {
                tail = AttributedString(String(line))
            }
            head.foregroundColor = Hy.dust
            tail.foregroundColor = Hy.starlight
            out += head; out += tail
            if i < lines.count - 1 { out += AttributedString("\n") }
        }
        return out
    }

    static func relative(_ iso: String) -> String {
        let f = ISO8601DateFormatter()
        var d = f.date(from: iso)
        if d == nil { f.formatOptions = [.withInternetDateTime, .withFractionalSeconds]; d = f.date(from: iso) }
        guard let d else { return iso }
        let s = max(0, Int(Date().timeIntervalSince(d)))
        if s < 60 { return "\(s)s ago" }
        if s < 3600 { return "\(s / 60)m ago" }
        if s < 86400 { return "\(s / 3600)h ago" }
        return "\(s / 86400)d ago"
    }
}

/// Request bodies built here because K10sCore's ObjectParams/ObjectEventsParams have no public initialisers.
struct InspectorObjectParams: Encodable {
    let context: String
    let resource: ResourceRef
    var namespace: String?
    let name: String
}

struct InspectorEventsParams: Encodable {
    let context: String
    var namespace: String?
    let uid: String
}

struct InspectorLogsParams: Encodable {
    let context: String
    let namespace: String
    let pod: String
    var container: String?
    var allContainers: Bool?
    var previous: Bool?
    let tailLines: Int
    let follow: Bool
    var timestamps: Bool?
}
