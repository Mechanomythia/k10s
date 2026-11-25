import K10sCore
import SwiftUI

/// Proves the bridge end to end: a context picker and one live list of pods.
/// The real window is the next proposal.
struct StubWindow: View {
    @Environment(EngineClient.self) private var engine
    @State private var contexts: [ContextInfo] = []
    @State private var selection = ""
    @State private var rows: [String: Row] = [:] // by uid
    @State private var contextState: ContextStateChange?
    @State private var failure: String?

    var body: some View {
        VStack(alignment: .leading, spacing: Hy.s3) {
            Picker("Context", selection: $selection) {
                ForEach(contexts, id: \.name) { c in
                    Text(c.name).font(Hy.mono(12)).tag(c.name)
                }
            }
            .pickerStyle(.menu)
            .frame(width: 320)

            if let line = stateLine {
                Text(line).font(Hy.ui(12.5)).foregroundStyle(Hy.dust)
            }
            if let message = engine.errorMessage ?? failure {
                HStack(spacing: Hy.s2) {
                    StarGlyph(state: .failed, size: 12)
                    Text(message).font(Hy.ui(12.5)).foregroundStyle(Hy.classM)
                }
            }

            List(sortedRows, id: \.uid) { row in
                HStack(spacing: Hy.s2) {
                    if let glyph = row.health.glyph {
                        StarGlyph(state: glyph, size: 12)
                    } else {
                        Color.clear.frame(width: 12, height: 12)
                    }
                    Text(row.namespace ?? "").font(Hy.mono(12)).foregroundStyle(Hy.dust)
                    Text(row.name).font(Hy.mono(12)).foregroundStyle(Hy.starlight)
                }
                .listRowBackground(Color.clear)
            }
            .scrollContentBackground(.hidden)
        }
        .padding(Hy.s4)
        .frame(minWidth: 640, minHeight: 400)
        .background(Hy.sky)
        .environment(\.colorScheme, .dark)
        .task(id: engine.restarts) {
            do {
                let r: ContextsListResult = try await engine.request(Method.contextsList, Empty())
                contexts = r.contexts
                if !contexts.contains(where: { $0.name == selection }) {
                    selection = (contexts.first(where: { $0.current }) ?? contexts.first)?.name ?? ""
                }
            } catch {
                failure = error.localizedDescription
            }
        }
        .task(id: "\(engine.restarts)|\(selection)") {
            rows = [:]
            contextState = nil
            failure = nil
            guard !selection.isEmpty else { return }
            let params = TableSubscribeParams(
                contexts: [selection],
                resource: ResourceRef(version: "v1", plural: "pods"),
                allNamespaces: true)
            do {
                for try await event in try await engine.subscribe(Method.tableSubscribe, params: params) {
                    apply(event)
                }
            } catch {
                if !Task.isCancelled { failure = error.localizedDescription }
            }
        }
    }

    private var sortedRows: [Row] {
        rows.values.sorted { ($0.namespace ?? "", $0.name) < ($1.namespace ?? "", $1.name) }
    }

    private var stateLine: String? {
        guard let s = contextState else { return nil }
        return "\(s.context) is \(s.state.rawValue)" + (s.error.map { ": \($0)" } ?? "")
    }

    private func apply(_ event: EngineEvent) {
        switch event.name {
        case EventName.snapshot:
            if let s = try? event.decode(Snapshot.self) {
                rows = Dictionary(s.rows.map { ($0.uid, $0) }, uniquingKeysWith: { _, new in new })
            }
        case EventName.upsert:
            if let u = try? event.decode(Upsert.self) { rows[u.row.uid] = u.row }
        case EventName.delete:
            if let d = try? event.decode(Delete.self) { rows[d.uid] = nil }
        case EventName.contextState:
            contextState = try? event.decode(ContextStateChange.self)
        default:
            break
        }
    }
}

extension Health {
    /// Health to star glyph; none for rows with no state.
    var glyph: StarGlyph.State? {
        switch self {
        case .ok: return .live
        case .pending: return .building
        case .degraded: return .degraded
        case .failed: return .failed
        case .stopped: return .stopped
        case .noGlyph: return nil
        }
    }
}
