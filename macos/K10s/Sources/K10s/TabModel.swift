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

    @ObservationIgnored private var namespaceInitialized = false

    init(scope: TabScope) {
        self.scope = scope
        self.table = TableModel(contexts: scope.contexts)
    }

    var filter: String {
        get { table.filter }
        set { table.filter = newValue }
    }

    func select(kind: KindEntry) {
        self.kind = kind
        selection = nil
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
