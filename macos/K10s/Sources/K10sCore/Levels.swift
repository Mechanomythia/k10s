import Foundation

/// The table query behind a level: object.children's answer for a parent, with
/// the child kind it lists.
public struct ChildQuery {
    public let kind: KindEntry
    public var namespace: String?
    public var labelSelector: String?
    public var fieldSelector: String?
    public var ownerUID: String?
    /// The parent selects nothing: show an empty table, do not subscribe.
    public var none: Bool

    public init(kind: KindEntry, namespace: String? = nil, labelSelector: String? = nil,
                fieldSelector: String? = nil, ownerUID: String? = nil, none: Bool = false) {
        self.kind = kind
        self.namespace = namespace
        self.labelSelector = labelSelector
        self.fieldSelector = fieldSelector
        self.ownerUID = ownerUID
        self.none = none
    }

    public init(kind: KindEntry, result r: ChildrenResult) {
        self.init(kind: kind, namespace: r.namespace, labelSelector: r.labelSelector,
                  fieldSelector: r.fieldSelector, ownerUID: r.ownerUID, none: r.none ?? false)
    }
}

public enum LevelChildren {
    case table(ChildQuery)
    /// The containers of the parent pod.
    case containers
}

/// The list state a level covers, restored when it is popped.
public struct CoveredList {
    public var filter: String
    public var sortColumn: String?
    public var ascending: Bool
    public var wide: Bool
    public var selection: Row?

    public init(filter: String, sortColumn: String?, ascending: Bool, wide: Bool, selection: Row?) {
        self.filter = filter
        self.sortColumn = sortColumn
        self.ascending = ascending
        self.wide = wide
        self.selection = selection
    }
}

/// One step into a parent's children.
public struct Level {
    public let parent: Row
    public let parentKind: KindEntry
    public let children: LevelChildren
    public let covered: CoveredList

    public init(parent: Row, parentKind: KindEntry, children: LevelChildren, covered: CoveredList) {
        self.parent = parent
        self.parentKind = parentKind
        self.children = children
        self.covered = covered
    }
}

public struct LevelStack {
    public private(set) var levels: [Level] = []

    public init() {}

    public var top: Level? { levels.last }
    public var isEmpty: Bool { levels.isEmpty }

    public mutating func push(_ level: Level) { levels.append(level) }

    /// Removes and returns the top level; its `covered` is the state to restore.
    @discardableResult
    public mutating func pop() -> Level? { levels.popLast() }

    public mutating func clear() { levels.removeAll() }
}

/// A parent's labelSelector plus a typed "=" filter, joined with a comma.
/// Nil when both are nil or blank.
public func joinSelectors(_ parent: String?, _ typed: String?) -> String? {
    let parts = [parent, typed].compactMap { $0?.trimmingCharacters(in: .whitespaces) }.filter { !$0.isEmpty }
    return parts.isEmpty ? nil : parts.joined(separator: ",")
}
