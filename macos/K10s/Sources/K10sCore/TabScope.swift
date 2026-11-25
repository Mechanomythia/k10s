import Foundation

public struct TabScope: Codable, Hashable {
    public var contexts: [String]
    public var name: String?
    /// Optional preset for a tab opened from the overview; nil keeps the defaults.
    public var overview: Bool?
    public var kindPlural: String?
    public var health: Set<Health>?

    public init(contexts: [String], name: String? = nil, overview: Bool? = nil,
                kindPlural: String? = nil, health: Set<Health>? = nil) {
        self.contexts = contexts
        self.name = name
        self.overview = overview
        self.kindPlural = kindPlural
        self.health = health
    }

    public var displayName: String {
        if contexts.count == 1 { return contexts[0] }
        if let name, !name.isEmpty { return name }
        let n = "\(contexts.count) clusters"
        guard var prefix = contexts.first else { return n }
        for c in contexts.dropFirst() {
            prefix = String(zip(prefix, c).prefix { $0 == $1 }.map { $0.0 })
        }
        while let last = prefix.last, last == "-" || last == "_" { prefix.removeLast() }
        return prefix.isEmpty ? n : "\(prefix) · \(n)"
    }
}

/// Named context sets and recent scopes, persisted as JSON in a UserDefaults.
public final class SavedSets {
    private let defaults: UserDefaults
    private let setsKey = "k10s.savedSets"
    private let recentKey = "k10s.recentScopes"

    public init(defaults: UserDefaults = .standard) { self.defaults = defaults }

    private func load<T: Decodable>(_ key: String, _ empty: T) -> T {
        defaults.data(forKey: key).flatMap { try? JSONDecoder().decode(T.self, from: $0) } ?? empty
    }

    private func store<T: Encodable>(_ value: T, _ key: String) {
        defaults.set(try? JSONEncoder().encode(value), forKey: key)
    }

    /// Saved sets as scopes, sorted by name.
    public func list() -> [TabScope] {
        load(setsKey, [String: [String]]()).sorted { $0.key < $1.key }
            .map { TabScope(contexts: $0.value, name: $0.key) }
    }

    public func save(name: String, contexts: [String]) {
        var sets = load(setsKey, [String: [String]]())
        sets[name] = contexts
        store(sets, setsKey)
    }

    public func delete(name: String) {
        var sets = load(setsKey, [String: [String]]())
        sets[name] = nil
        store(sets, setsKey)
    }

    /// Most recent first, at most 5.
    public var recent: [TabScope] { load(recentKey, [TabScope]()) }

    public func noteRecent(_ scope: TabScope) {
        var r = recent.filter { $0 != scope }
        r.insert(scope, at: 0)
        store(Array(r.prefix(5)), recentKey)
    }
}
