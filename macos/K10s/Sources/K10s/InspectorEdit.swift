import K10sCore
import SwiftUI

/// YAML edit mode of the inspector (ADR 0004, decision 6): fetch, edit, replace, conflict handling.
struct YamlEditor: View {
    var tab: TabModel
    let row: Row
    @Environment(EngineClient.self) private var engine
    @Environment(Protection.self) private var protection

    @State private var original = ""
    @State private var edited = ""
    @State private var conflict = false
    @State private var error: String?
    @State private var confirming = false
    @State private var anyway = false
    @State private var busy = false

    private var changed: Int {
        let a = original.components(separatedBy: "\n"), b = edited.components(separatedBy: "\n")
        var n = abs(a.count - b.count)
        for i in 0..<min(a.count, b.count) where a[i] != b[i] { n += 1 }
        return n
    }

    private var params: ObjectParams {
        ObjectParams(context: row.context, resource: tab.activeKind?.ref ?? ResourceRef(version: "v1", plural: "pods"),
                     namespace: row.namespace, name: row.name)
    }

    var body: some View {
        VStack(alignment: .leading, spacing: Hy.s3) {
            if conflict { notice }
            TextEditor(text: $edited)
                .font(Hy.mono(12))
                .foregroundStyle(Hy.starlight)
                .scrollContentBackground(.hidden)
                .padding(Hy.s2)
                .background(Hy.sky)
                .overlay(RoundedRectangle(cornerRadius: Hy.r1).strokeBorder(Hy.neatlineStrong, lineWidth: 1))
                .accessibilityLabel("YAML")
            if let error {
                Text(error).font(Hy.mono(12)).foregroundStyle(Hy.classM).textSelection(.enabled)
            }
            HStack(spacing: Hy.s2) {
                Text("\(changed) lines changed · status and managedFields are not editable")
                    .font(Hy.mono(11.5)).foregroundStyle(Hy.faint).lineLimit(1).truncationMode(.tail)
                Spacer(minLength: Hy.s2)
                Button("Discard") { tab.editing = false }.buttonStyle(HyButton(kind: .quiet))
                if conflict {
                    Button("Save anyway") { attempt(anyway: true) }
                        .buttonStyle(HyButton(kind: .secondary)).disabled(busy)
                }
                Button("Save") { attempt(anyway: false) }
                    .buttonStyle(HyButton(kind: .primary)).disabled(changed == 0 || busy)
            }
        }
        .padding(Hy.s4)
        .task(id: row.uid) { await reload() }
        .sheet(isPresented: $confirming) {
            ConfirmSheet(title: "Save changes?", context: row.context, namespace: row.namespace,
                         consequence: Text("Your edits replace ")
                            + Text(row.name).font(Hy.mono(13.5)).foregroundColor(Hy.starlight)
                            + Text(" in the cluster."),
                         protected: true, name: row.name, noun: tab.activeKind?.noun ?? "object",
                         verb: "save", actionLabel: "Save") {
                await perform(anyway: anyway)
            }
        }
    }

    private var notice: some View {
        HStack(alignment: .top, spacing: Hy.s3) {
            StarGlyph(state: .degraded, size: 16)
            VStack(alignment: .leading, spacing: 2) {
                Text("\(row.name) changed since you opened it.").font(Hy.ui(13, 600)).foregroundStyle(Hy.starlight)
                Text("Your edits are kept. Reload to start from the new version, or save over it anyway.")
                    .font(Hy.ui(13)).foregroundStyle(Hy.dust)
            }
            Spacer(minLength: Hy.s2)
            Button("Reload") { Task { await reload() } }.buttonStyle(HyButton(kind: .secondary, small: true))
        }
        .padding(Hy.s3)
        .background(Hy.skyRaised)
        .overlay(RoundedRectangle(cornerRadius: Hy.r1).strokeBorder(Hy.neatlineStrong, lineWidth: 1))
        .accessibilityElement(children: .combine)
    }

    // MARK: actions

    private func attempt(anyway: Bool) {
        self.anyway = anyway
        if protection.isProtected(row.context) { confirming = true } else { Task { await perform(anyway: anyway) } }
    }

    private func fetch() async throws -> String {
        let r: TextResult = try await engine.request(Method.objectYAML, params)
        return r.text
    }

    private func reload() async {
        do {
            let t = Self.stripStatus(try await fetch())
            original = t; edited = t; conflict = false; error = nil
        } catch { self.error = error.localizedDescription }
    }

    private func perform(anyway: Bool) async {
        busy = true
        defer { busy = false }
        error = nil
        do {
            var yaml = edited
            if anyway {
                let current = try await fetch()
                if let rv = Self.line(current, "  resourceVersion: ") {
                    yaml = yaml.components(separatedBy: "\n")
                        .map { $0.hasPrefix("  resourceVersion: ") ? rv : $0 }.joined(separator: "\n")
                }
            }
            let p = ReplaceParams(context: row.context, resource: params.resource, namespace: row.namespace,
                                  name: row.name, yaml: yaml)
            let _: ReplaceResult = try await engine.request(Method.objectReplace, p)
            tab.editing = false
        } catch let e as ProtocolError where e.code == ErrorCode.conflict {
            conflict = true
        } catch { self.error = error.localizedDescription }
    }

    // MARK: text helpers

    private static func line(_ s: String, _ prefix: String) -> String? {
        s.components(separatedBy: "\n").first { $0.hasPrefix(prefix) }
    }

    /// Drops the top-level `status:` block: from the line "status:" to the next top-level key.
    static func stripStatus(_ s: String) -> String {
        var out: [String] = [], skipping = false
        for l in s.components(separatedBy: "\n") {
            if l == "status:" { skipping = true; continue }
            if skipping {
                if let c = l.first, c != " ", c != "-" { skipping = false } else { continue }
            }
            out.append(l)
        }
        return out.joined(separator: "\n")
    }
}
