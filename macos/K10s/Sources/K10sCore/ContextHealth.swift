import Foundation

/// The glyph states of the UI, as plain values (no SwiftUI here).
public enum GlyphState: Hashable {
    case live, building, degraded, failed, stopped, noGlyph

    public init(_ h: Health) {
        switch h {
        case .ok: self = .live
        case .pending: self = .building
        case .degraded: self = .degraded
        case .failed: self = .failed
        case .stopped: self = .stopped
        case .noGlyph: self = .noGlyph
        }
    }

    public init(_ s: ContextState) {
        switch s {
        case .live: self = .live
        case .connecting: self = .building
        case .unreachable, .unauthorized: self = .failed
        }
    }
}

public struct ContextStatus: Equatable {
    public var state: ContextState
    public var error: String?
    public var version: String?

    public init(state: ContextState, error: String? = nil, version: String? = nil) {
        self.state = state
        self.error = error
        self.version = version
    }
}

/// Per-context connection state, fed by context.state events.
public struct ContextHealth {
    public private(set) var statuses: [String: ContextStatus] = [:]

    public init() {}

    public mutating func apply(_ c: ContextStateChange) {
        let version = statuses[c.context]?.version
        statuses[c.context] = ContextStatus(state: c.state, error: c.error, version: version)
    }

    /// Set from outside (the protocol does not carry it yet).
    public mutating func setVersion(_ version: String?, for context: String) {
        var s = statuses[context] ?? ContextStatus(state: .connecting, error: nil, version: nil)
        s.version = version
        statuses[context] = s
    }

    /// Unknown contexts count as connecting.
    public func state(of context: String) -> ContextState { statuses[context]?.state ?? .connecting }

    /// The tab star: unauthorized/unreachable beat connecting beat live; empty is connecting.
    public func worst(of contexts: [String]) -> ContextState {
        let states = contexts.map(state(of:))
        if let bad = states.first(where: { $0 == .unauthorized || $0 == .unreachable }) { return bad }
        if states.isEmpty || states.contains(.connecting) { return .connecting }
        return .live
    }
}
