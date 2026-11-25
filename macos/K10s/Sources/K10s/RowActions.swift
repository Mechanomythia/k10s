import AppKit
import K10sCore
import SwiftUI

enum RowAction { case delete, scale, restart }

/// A write waiting for its confirm sheet; the window presents it.
struct PendingAction: Identifiable {
    let id = UUID()
    let action: RowAction
    let row: Row
    let kind: KindEntry
    /// The row's cells by upper-cased column name (READY, DESIRED, …).
    let cells: [String: String]
}

extension KindEntry {
    var isApps: Bool { group == "apps" }
    var scalable: Bool { isApps && ["deployments", "statefulsets", "replicasets"].contains(plural) }
    var restartable: Bool { isApps && ["deployments", "statefulsets", "daemonsets"].contains(plural) }
    var isPod: Bool { group.isEmpty && kind == "Pod" }
    /// Lowercase singular for copy: "pod", "deployment".
    var noun: String { (kind.isEmpty ? plural : kind).lowercased() }
}

extension TabModel {
    /// Queues the confirm sheet for the selected row; false when the action does not apply.
    @discardableResult
    func request(_ action: RowAction) -> Bool {
        guard let row = selection, let kind else { return false }
        if action == .scale, !kind.scalable { return false }
        if action == .restart, !kind.restartable { return false }
        let cols = table.columns.map { $0.uppercased() }
        let cells = Dictionary(zip(cols, table.cells(of: row)), uniquingKeysWith: { a, _ in a })
        pendingAction = PendingAction(action: action, row: row, kind: kind, cells: cells)
        return true
    }

    func copyName() {
        guard let row = selection else { return }
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(row.name, forType: .string)
    }

    /// Opens the inspector in YAML edit mode (the inspector builds the editor).
    func edit() {
        guard selection != nil else { return }
        inspectorTab = .yaml
        inspectorOpen = true
        editing = true
    }
}

/// The items of the row menu and the Actions menu; inapplicable ones are hidden.
/// With a `row` (context menu) each item selects it first.
struct RowActionItems: View {
    let tab: TabModel
    var row: Row?

    private func act(_ f: @escaping () -> Void) -> () -> Void {
        { if let row { tab.selection = row }; f() }
    }

    var body: some View {
        if row != nil || tab.selection != nil {
            Button("Describe", action: act { _ = tab.openInspector(.describe) })
            Button("YAML", action: act { _ = tab.openInspector(.yaml) })
            Button("Events", action: act { _ = tab.openInspector(.events) })
            Button("Edit…", action: act { tab.edit() })
            if tab.kind?.scalable == true { Button("Scale…", action: act { _ = tab.request(.scale) }) }
            if tab.kind?.restartable == true { Button("Restart", action: act { _ = tab.request(.restart) }) }
            Button("Delete…", action: act { _ = tab.request(.delete) })
            Divider()
            Button("Copy name", action: act { tab.copyName() })
        }
    }
}

/// Presents the sheet for a pending action.
struct PendingActionSheet: View {
    let pending: PendingAction
    @Environment(EngineClient.self) private var engine
    @Environment(Protection.self) private var protection

    private var row: Row { pending.row }
    private var kind: KindEntry { pending.kind }
    private var isProtected: Bool { protection.isProtected(row.context) }

    private var objectParams: ObjectParams {
        ObjectParams(context: row.context, resource: kind.ref, namespace: row.namespace, name: row.name)
    }

    private func name(_ rest: String) -> Text {
        Text(row.name).font(Hy.mono(13.5)).foregroundColor(Hy.starlight) + Text(" " + rest)
    }

    var body: some View {
        switch pending.action {
        case .delete:
            let rest: String = {
                if kind.isPod { return "is deleted. If a controller owns it, it creates a new pod to replace it." }
                if kind.restartable || kind.scalable { return "and its pods are deleted. Nothing recreates them." }
                return "is deleted. Nothing recreates it."
            }()
            ConfirmSheet(title: "Delete \(kind.noun)?", context: row.context, namespace: row.namespace,
                         consequence: name(rest), protected: isProtected, name: row.name,
                         noun: kind.noun, verb: "delete", actionLabel: "Delete \(kind.noun)", danger: true) {
                let _: Empty = try await engine.request(Method.objectDelete, objectParams)
            }
        case .restart:
            ConfirmSheet(title: "Restart \(kind.noun)?", context: row.context, namespace: row.namespace,
                         consequence: name("is restarted. Pods are replaced one by one."),
                         protected: isProtected, name: row.name, noun: kind.noun, verb: "restart",
                         actionLabel: "Restart \(kind.noun)") {
                let _: Empty = try await engine.request(Method.workloadRestart, objectParams)
            }
        case .scale:
            ScaleSheet(row: row, kind: kind, protected: isProtected, cells: pending.cells)
        }
    }
}

private struct ScaleSheet: View {
    let row: Row
    let kind: KindEntry
    let protected: Bool
    let cells: [String: String]
    @Environment(EngineClient.self) private var engine
    @State private var text = ""

    /// (ready, desired) from a READY column "M/N", or READY and DESIRED columns.
    private var counts: (ready: Int, desired: Int)? {
        if let r = cells["READY"] {
            let p = r.split(separator: "/")
            if p.count == 2, let m = Int(p[0]), let n = Int(p[1]) { return (m, n) }
            if let m = Int(r), let n = cells["DESIRED"].flatMap({ Int($0) }) { return (m, n) }
        }
        return nil
    }

    private var value: Int? {
        Int(text.trimmingCharacters(in: .whitespaces)).flatMap { $0 >= 0 ? $0 : nil }
    }

    var body: some View {
        let current = counts?.desired
        ConfirmSheet(title: "Scale \(kind.noun)?", context: row.context, namespace: row.namespace,
                     consequence: Text(row.name).font(Hy.mono(13.5)).foregroundColor(Hy.starlight)
                        + Text(" gets the replica count you set."),
                     protected: protected, name: row.name, noun: kind.noun, verb: "scale",
                     actionLabel: "Scale to \(value.map(String.init) ?? "…")",
                     canRun: value != nil && value != current, content: {
            VStack(alignment: .leading, spacing: 6) {
                HyLabel("Replicas")
                TextField("", text: $text)
                    .textFieldStyle(.plain)
                    .font(Hy.mono(13)).foregroundStyle(Hy.starlight)
                    .padding(.horizontal, Hy.s3).frame(width: 120, height: 34)
                    .background(Hy.sky)
                    .overlay(RoundedRectangle(cornerRadius: Hy.r1).strokeBorder(Hy.neatlineStrong, lineWidth: 1))
                if let c = counts {
                    Text("now \(c.desired) · \(c.ready) ready").font(Hy.mono(12)).foregroundStyle(Hy.faint)
                }
            }
        }, run: {
            guard let n = value else { return }
            let p = ScaleParams(context: row.context, resource: kind.ref, namespace: row.namespace,
                                name: row.name, replicas: n)
            let _: Empty = try await engine.request(Method.workloadScale, p)
        })
        .onAppear { if text.isEmpty, let c = current { text = String(c) } }
    }
}
