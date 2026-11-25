import Foundation

// Codable mirror of internal/engine/protocol.go. JSON keys match the Go tags;
// a field is optional where Go has omitempty. ProtocolTests decodes and
// re-encodes every golden file in internal/engine/testdata/protocol.

public enum Method {
    public static let contextsList = "contexts.list"
    public static let contextsWatch = "contexts.watch"
    public static let resourcesList = "resources.list"
    public static let tableSubscribe = "table.subscribe"
    public static let objectYAML = "object.yaml"
    public static let objectDescribe = "object.describe"
    public static let objectEvents = "object.events"
    public static let logsSubscribe = "logs.subscribe"
    public static let unsubscribe = "unsubscribe"
}

public enum EventName {
    public static let contextsChanged = "contexts.changed"
    public static let snapshot = "snapshot"
    public static let upsert = "upsert"
    public static let delete = "delete"
    public static let contextState = "context.state"
    public static let lines = "lines"
    public static let end = "end"
}

public enum ErrorCode {
    public static let notImplemented = "not_implemented"
    public static let unknownMethod = "unknown_method"
    public static let invalidParams = "invalid_params"
    public static let `internal` = "internal"
}

/// Any JSON value: the raw params, result and data of the envelopes.
public enum JSONValue: Codable, Equatable {
    case null
    case bool(Bool)
    case number(Double)
    case string(String)
    case array([JSONValue])
    case object([String: JSONValue])

    public init(from decoder: Decoder) throws {
        let c = try decoder.singleValueContainer()
        if c.decodeNil() { self = .null }
        else if let v = try? c.decode(Bool.self) { self = .bool(v) }
        else if let v = try? c.decode(Double.self) { self = .number(v) }
        else if let v = try? c.decode(String.self) { self = .string(v) }
        else if let v = try? c.decode([JSONValue].self) { self = .array(v) }
        else { self = .object(try c.decode([String: JSONValue].self)) }
    }

    public func encode(to encoder: Encoder) throws {
        var c = encoder.singleValueContainer()
        switch self {
        case .null: try c.encodeNil()
        case .bool(let v): try c.encode(v)
        case .number(let v): try c.encode(v)
        case .string(let v): try c.encode(v)
        case .array(let v): try c.encode(v)
        case .object(let v): try c.encode(v)
        }
    }
}

public struct Request: Codable {
    public let id: String
    public let method: String
    public var params: JSONValue?

    public init(id: String, method: String, params: JSONValue? = nil) {
        self.id = id
        self.method = method
        self.params = params
    }
}

public struct Response: Codable {
    public let id: String
    public var result: JSONValue?
    public var error: ProtocolError?
}

/// The Go type `Error`: a stable machine code and a human message.
public struct ProtocolError: Codable, Error, LocalizedError {
    public let code: String
    public let message: String
    public var errorDescription: String? { message }
}

public struct Event: Codable {
    public let sub: String
    public let event: String
    public let data: JSONValue
}

public struct Empty: Codable {
    public init() {}
}

public struct SubscribeResult: Codable {
    public let sub: String
}

public struct UnsubscribeParams: Codable {
    public let sub: String
    public init(sub: String) { self.sub = sub }
}

public struct ContextInfo: Codable, Hashable {
    public let name: String
    public let cluster: String
    public let user: String
    public let namespace: String
    public let current: Bool
}

public struct ContextsListResult: Codable {
    public let contexts: [ContextInfo]
}

public struct ContextsChanged: Codable {
    public let contexts: [ContextInfo]
}

public struct ResourcesListParams: Codable {
    public let context: String
    public init(context: String) { self.context = context }
}

public struct ResourceKind: Codable {
    public var group: String?
    public let version: String
    public let kind: String
    public let plural: String
    public var shortNames: [String]?
    public let namespaced: Bool
}

public struct ResourcesListResult: Codable {
    public let resources: [ResourceKind]
}

public struct ResourceRef: Codable {
    public var group: String?
    public let version: String
    public let plural: String

    public init(group: String? = nil, version: String, plural: String) {
        self.group = group
        self.version = version
        self.plural = plural
    }
}

public struct TableSubscribeParams: Codable {
    public let contexts: [String]
    public let resource: ResourceRef
    public var namespace: String?
    public var allNamespaces: Bool?
    public var labelSelector: String?

    public init(contexts: [String], resource: ResourceRef, namespace: String? = nil,
                allNamespaces: Bool? = nil, labelSelector: String? = nil) {
        self.contexts = contexts
        self.resource = resource
        self.namespace = namespace
        self.allNamespaces = allNamespaces
        self.labelSelector = labelSelector
    }
}

/// The state behind a row's star glyph. The Go value "none" is `noGlyph`
/// (a case named `none` would clash with Optional.none).
public enum Health: String, Codable {
    case ok, pending, degraded, failed, stopped
    case noGlyph = "none"
}

public struct Row: Codable {
    public let context: String
    public var namespace: String?
    public let name: String
    public let uid: String
    public let cells: [String]
    public let health: Health
}

public struct Column: Codable {
    public let name: String
    /// 0 is always shown; higher only in a wide view (kubectl's -o wide).
    public let priority: Int?
}

public struct Snapshot: Codable {
    public let columns: [Column]
    public let rows: [Row]
}

public struct Upsert: Codable {
    public let row: Row
}

public struct Delete: Codable {
    public let context: String
    public var namespace: String?
    public let name: String
    public let uid: String
}

public enum ContextState: String, Codable {
    case connecting, live, unreachable, unauthorized
}

public struct ContextStateChange: Codable {
    public let context: String
    public let state: ContextState
    public var error: String?
}

public struct ObjectParams: Codable {
    public let context: String
    public let resource: ResourceRef
    public var namespace: String?
    public let name: String
}

public struct TextResult: Codable {
    public let text: String
}

public struct ObjectEventsParams: Codable {
    public let context: String
    public var namespace: String?
    public let uid: String
}

public struct ObjectEvent: Codable {
    public let type: String
    public let reason: String
    public let message: String
    public let count: Int
    public let lastSeen: String
}

public struct ObjectEventsResult: Codable {
    public let events: [ObjectEvent]
}

public struct LogsSubscribeParams: Codable {
    public let context: String
    public let namespace: String
    public let pod: String
    public var container: String?
    public var allContainers: Bool?
    public var previous: Bool?
    public let tailLines: Int
    public let follow: Bool
    public var timestamps: Bool?
}

public struct Lines: Codable {
    public let lines: [String]
}

public struct End: Codable {
    public var error: String?
}
