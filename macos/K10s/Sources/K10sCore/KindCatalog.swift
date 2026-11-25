import Foundation

public struct KindEntry: Hashable {
    public let group: String
    public let version: String
    public let kind: String
    public let plural: String
    public let shortNames: [String]
    public let namespaced: Bool

    public var ref: ResourceRef {
        ResourceRef(group: group.isEmpty ? nil : group, version: version, plural: plural)
    }
}

public struct KindSection {
    public let title: String
    public let kinds: [KindEntry]
}

/// The kinds a tab can show: the union of resources.list over its contexts.
public struct KindCatalog {
    /// Unique by group + plural, sorted by plural.
    public let kinds: [KindEntry]

    static let builtinGroups: [String] = [
        "", "apps", "batch", "networking.k8s.io", "policy", "rbac.authorization.k8s.io",
        "storage.k8s.io", "autoscaling", "coordination.k8s.io", "discovery.k8s.io",
        "events.k8s.io", "scheduling.k8s.io", "node.k8s.io", "apiextensions.k8s.io",
        "admissionregistration.k8s.io", "certificates.k8s.io", "flowcontrol.apiserver.k8s.io",
        "apiregistration.k8s.io",
    ]
    static let sectionPlurals: [(String, [String])] = [
        ("Workloads", ["pods", "deployments", "statefulsets", "daemonsets", "replicasets", "jobs", "cronjobs"]),
        ("Network", ["services", "ingresses", "endpoints", "networkpolicies"]),
        ("Config", ["configmaps", "secrets"]),
        ("Cluster", ["nodes", "namespaces", "events"]),
    ]
    /// Extra command-bar aliases the API does not advertise.
    static let extraAliases: [String: String] = ["dp": "deployments"]

    public init(perContext lists: [[ResourceKind]]) {
        var seen = Set<String>()
        var out: [KindEntry] = []
        for r in lists.joined() {
            let group = r.group ?? ""
            guard seen.insert(group + "/" + r.plural).inserted else { continue }
            out.append(KindEntry(group: group, version: r.version, kind: r.kind, plural: r.plural,
                                 shortNames: r.shortNames ?? [], namespaced: r.namespaced))
        }
        kinds = out.sorted { ($0.plural, $0.group) < ($1.plural, $1.group) }
    }

    private static func isBuiltin(_ group: String) -> Bool { builtinGroups.contains(group) }

    /// Sidebar sections; empty sections are dropped.
    public var sections: [KindSection] {
        var result: [KindSection] = []
        for (title, plurals) in Self.sectionPlurals {
            let found = plurals.compactMap { p in
                kinds.first { $0.plural == p && Self.isBuiltin($0.group) && $0.group != "events.k8s.io" }
            }
            if !found.isEmpty { result.append(KindSection(title: title, kinds: found)) }
        }
        let custom = kinds.filter { !Self.isBuiltin($0.group) }
            .sorted { ($0.kind, $0.group) < ($1.kind, $1.group) }
        if !custom.isEmpty { result.append(KindSection(title: "Custom resources", kinds: custom)) }
        return result
    }

    /// The kind an exact `:dp`, `:deploy`, `:deployments`, `:Deployment` names, if any.
    public func resolve(_ input: String) -> KindEntry? {
        let q = input.lowercased()
        guard !q.isEmpty else { return nil }
        return ranked(q).first { $0.rank == 0 }?.entry
    }

    /// Command-bar completions: exact alias/plural/kind, then plural prefix, then kind prefix.
    public func complete(_ prefix: String) -> [KindEntry] {
        let q = prefix.lowercased()
        guard !q.isEmpty else { return [] }
        return ranked(q).map(\.entry)
    }

    private func ranked(_ q: String) -> [(rank: Int, entry: KindEntry)] {
        let alias = Self.extraAliases[q]
        var out: [(rank: Int, entry: KindEntry)] = []
        for e in kinds {
            let kind = e.kind.lowercased()
            let rank: Int
            if e.plural == q || kind == q || e.shortNames.contains(q) || alias == e.plural { rank = 0 }
            else if e.plural.hasPrefix(q) { rank = 1 }
            else if kind.hasPrefix(q) { rank = 2 }
            else { continue }
            out.append((rank, e))
        }
        func groupOrder(_ g: String) -> Int { Self.builtinGroups.firstIndex(of: g) ?? Self.builtinGroups.count }
        return out.sorted {
            ($0.rank, groupOrder($0.entry.group), $0.entry.plural, $0.entry.group)
                < ($1.rank, groupOrder($1.entry.group), $1.entry.plural, $1.entry.group)
        }
    }
}
