import K10sCore
import Observation
import SwiftUI

enum InspectorTab: String, CaseIterable {
    case describe, yaml, events, logs
}

extension GlyphState {
    /// The Theme glyph for this state; nil for `noGlyph` (show nothing).
    var star: StarGlyph.State? {
        switch self {
        case .live: return .live
        case .building: return .building
        case .degraded: return .degraded
        case .failed: return .failed
        case .stopped: return .stopped
        case .noGlyph: return nil
        }
    }
}

/// State of one window/tab. The shell owns it; the slot views read and write it.
@MainActor @Observable
final class TabModel {
    let scope: TabScope
    /// Contexts of the scope, in scope order, from contexts.list.
    var contexts: [ContextInfo] = []
    var catalog = KindCatalog(perContext: [])
    /// Selected kind; defaults to pods once the catalog is loaded. Use `select(kind:)` from the sidebar.
    var kind: KindEntry?
    /// nil = all namespaces.
    var namespace: String?
    /// Namespace names of the first context, sorted.
    var namespaces: [String] = []
    /// The list owns subscribing and feeding this (apply snapshot/upsert/delete); the shell reads totalCount and edits filter.
    var table: TableModel
    var selection: Row?
    var inspectorTab: InspectorTab = .describe
    var inspectorOpen = false
    /// Inspector edit mode (built by the inspector); set by Edit…/e.
    var editing = false
    /// A write waiting for its confirm sheet.
    var pendingAction: PendingAction?
    var health = ContextHealth()
    var sidebarVisible = true
    /// Key handling hooks: whoever handles ":" / "/" / ⌘T sets these.
    var commandBarOpen = false
    var newTabOpen = false
    /// Set to true to focus the filter field; the toolbar resets it.
    var focusFilter = false

    /// Dive levels over the tab's own list (ADR 0006); `table` is always the visible list's table.
    var levels = LevelStack()
    /// Tables of the table levels under the top one, root first.
    var belowTables: [TableModel] = []
    /// Containers level: the pod's containers as shown, and the selected one's name.
    var containers: [ContainerStatus] = []
    var selectedContainer: String?
    /// Container the logs view picks when it opens; it clears this.
    var presetContainer: String?
    /// An object.children request is in flight, or failed with this message.
    var diving = false
    var diveError: String?

    @ObservationIgnored var engine: EngineClient?
    @ObservationIgnored private var namespaceInitialized = false
    @ObservationIgnored private var diveID = 0

    init(scope: TabScope) {
        self.scope = scope
        self.table = TableModel(contexts: scope.contexts)
    }

    var filter: String {
        get { table.filter }
        set { table.filter = newValue }
    }

    func select(kind: KindEntry) {
        clearStack()
        self.kind = kind
        selection = nil
    }

    // MARK: levels

    var inContainers: Bool {
        if case .containers? = levels.top?.children { return true }
        return false
    }

    var activeKind: KindEntry? {
        switch levels.top?.children {
        case .table(let q)?: return q.kind
        case .containers?: return levels.top?.parentKind
        case nil: return kind
        }
    }

    var activeContexts: [String] { levels.top.map { [$0.parent.context] } ?? scope.contexts }

    var activeNamespace: String? {
        switch levels.top?.children {
        case .table(let q)?: return q.namespace
        case .containers?: return levels.top?.parent.namespace
        case nil: return namespace
        }
    }

    /// The row the object keys act on: the parent pod on the containers level.
    var activeSelection: Row? { inContainers ? levels.top?.parent : selection }

    /// "deployments / web / pods / web-7d9f" for the first `count` levels.
    func breadcrumb(upTo count: Int) -> String {
        levels.levels.prefix(count).map { "\($0.parentKind.plural) / \($0.parent.name)" }.joined(separator: " / ")
    }

    /// The table of the list at `depth` (0 = the tab's own list).
    func tableAt(_ depth: Int) -> TableModel { depth < belowTables.count ? belowTables[depth] : table }

    /// Edits the table at `depth`; a list that has been popped (depth too big) is ignored.
    func update(depth: Int, _ f: (inout TableModel) -> Void) {
        if depth < belowTables.count { f(&belowTables[depth]) } else if depth == belowTables.count { f(&table) }
    }

    /// Return / double-click on the selected row: dive when its kind has children. False otherwise.
    @discardableResult
    func diveIn() -> Bool {
        guard !inContainers, !diving, let row = selection, let kind = activeKind, let child = kind.childKind else { return false }
        let covered = CoveredList(filter: table.filter, sortColumn: table.sortColumn, ascending: table.ascending,
                                  wide: table.wide, selection: row)
        inspectorOpen = false
        editing = false
        diveError = nil
        switch child {
        case .containers:
            containers = []
            selectedContainer = nil
            push(Level(parent: row, parentKind: kind, children: .containers, covered: covered))
        case .table:
            guard let engine else { return false }
            diving = true
            diveID += 1
            let id = diveID
            Task { [self] in
                do {
                    let p = ObjectParams(context: row.context, resource: kind.ref, namespace: row.namespace, name: row.name)
                    let r: ChildrenResult = try await engine.request(Method.objectChildren, p)
                    guard id == diveID else { return }
                    diving = false
                    guard let childKind = catalog.kinds.first(where: {
                        $0.group == (r.resource.group ?? "") && $0.plural == r.resource.plural
                    }) else {
                        diveError = "This cluster does not list \(r.resource.plural)."
                        return
                    }
                    let level = Level(parent: row, parentKind: kind,
                                      children: .table(ChildQuery(kind: childKind, result: r)), covered: covered)
                    belowTables.append(table)
                    table = TableModel(contexts: [row.context])
                    selection = nil
                    push(level)
                } catch {
                    guard id == diveID else { return }
                    diving = false
                    diveError = error.localizedDescription
                }
            }
        }
        return true
    }

    private func push(_ level: Level) {
        withAnimation(.easeOut(duration: 0.2)) { levels.push(level) }
    }

    /// Esc: back one level, restoring the list it covered.
    func pop() {
        guard let level = levels.top else { return }
        inspectorOpen = false
        editing = false
        selectedContainer = nil
        containers = []
        if case .table = level.children, let t = belowTables.popLast() { table = t }
        selection = level.covered.selection
        withAnimation(.easeOut(duration: 0.2)) { _ = levels.pop() }
    }

    /// Back to the tab's own list (kind or namespace changed).
    func clearStack() {
        diveID += 1
        diving = false
        diveError = nil
        guard !levels.isEmpty else { return }
        inspectorOpen = false
        editing = false
        selectedContainer = nil
        containers = []
        if let root = belowTables.first { table = root }
        belowTables = []
        levels.clear()
    }

    /// Feed context.state events (from any subscription of this tab) here.
    func apply(_ change: ContextStateChange) {
        health.apply(change)
        if let v = change.version { health.setVersion(v, for: change.context) }
    }

    /// Loads contexts.list info and the kind catalog.
    func load(_ engine: EngineClient) async {
        if let r: ContextsListResult = try? await engine.request(Method.contextsList, Empty()) {
            contexts = scope.contexts.compactMap { n in r.contexts.first { $0.name == n } }
            if !namespaceInitialized {
                namespaceInitialized = true
                let ns = contexts.first?.namespace ?? ""
                namespace = ns.isEmpty ? "default" : ns
            }
        }
        var lists: [[ResourceKind]] = []
        for c in scope.contexts {
            if let r: ResourcesListResult = try? await engine.request(Method.resourcesList, ResourcesListParams(context: c)) {
                lists.append(r.resources)
            }
        }
        catalog = KindCatalog(perContext: lists)
        if kind == nil {
            kind = catalog.kinds.first { $0.plural == "pods" && $0.group.isEmpty }
        }
    }

    /// Live namespace names of the first context; also feeds context.state into `health`.
    func watchNamespaces(_ engine: EngineClient) async {
        guard let first = scope.contexts.first else { return }
        var byUID: [String: String] = [:]
        let params = TableSubscribeParams(contexts: [first], resource: ResourceRef(version: "v1", plural: "namespaces"))
        do {
            for try await event in try await engine.subscribe(Method.tableSubscribe, params: params) {
                switch event.name {
                case EventName.snapshot:
                    if let s = try? event.decode(Snapshot.self) {
                        byUID = Dictionary(s.rows.map { ($0.uid, $0.name) }, uniquingKeysWith: { _, new in new })
                    }
                case EventName.upsert:
                    if let u = try? event.decode(Upsert.self) { byUID[u.row.uid] = u.row.name }
                case EventName.delete:
                    if let d = try? event.decode(Delete.self) { byUID[d.uid] = nil }
                case EventName.contextState:
                    if let c = try? event.decode(ContextStateChange.self) { apply(c) }
                default:
                    break
                }
                namespaces = byUID.values.sorted()
            }
        } catch {}
    }
}
