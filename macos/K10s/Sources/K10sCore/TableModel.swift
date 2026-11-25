import Foundation

/// A tab's merged table: rows from every context keyed by (context, uid).
public struct TableModel {
    public let contexts: [String]
    public var wide = false
    /// A visible column name (including "Context"); nil sorts by name then context.
    public var sortColumn: String?
    public var ascending = true
    public var filter = ""

    private var allColumns: [Column] = []
    private var store: [String: Row] = [:]

    public init(contexts: [String]) { self.contexts = contexts }

    private static func key(_ context: String, _ uid: String) -> String { context + "\u{0}" + uid }

    public mutating func apply(_ s: Snapshot) {
        allColumns = s.columns
        store = Dictionary(s.rows.map { (Self.key($0.context, $0.uid), $0) }, uniquingKeysWith: { $1 })
    }

    public mutating func apply(_ u: Upsert) { store[Self.key(u.row.context, u.row.uid)] = u.row }

    public mutating func apply(_ d: Delete) { store[Self.key(d.context, d.uid)] = nil }

    private var visibleIndexes: [Int] {
        allColumns.indices.filter { wide || (allColumns[$0].priority ?? 0) == 0 }
    }

    private var hasContextColumn: Bool { contexts.count > 1 }

    /// Header names of the visible columns, with a leading "Context" for multi-context scopes.
    public var columns: [String] {
        (hasContextColumn ? ["Context"] : []) + visibleIndexes.map { allColumns[$0].name }
    }

    /// The cells of a row, aligned with `columns`.
    public func cells(of row: Row) -> [String] {
        (hasContextColumn ? [row.context] : []) + visibleIndexes.map { $0 < row.cells.count ? row.cells[$0] : "" }
    }

    /// The labelSelector to resubscribe with, when the filter text contains "=".
    public var labelSelector: String? {
        let t = filter.trimmingCharacters(in: .whitespaces)
        return t.contains("=") ? t : nil
    }

    public var totalCount: Int { store.count }
    public var filteredCount: Int { rows.count }

    /// Filtered and sorted rows.
    public var rows: [Row] {
        var list = Array(store.values)
        let t = filter.trimmingCharacters(in: .whitespaces)
        if !t.isEmpty && labelSelector == nil {
            list = list.filter { $0.name.range(of: t, options: .caseInsensitive) != nil }
        }
        let col = sortColumn.flatMap { columns.firstIndex(of: $0) }
        func byName(_ a: Row, _ b: Row) -> Int {
            let n = a.name.localizedStandardCompare(b.name)
            if n != .orderedSame { return n.rawValue }
            return a.context.localizedStandardCompare(b.context).rawValue
        }
        return list.sorted { a, b in
            if let col {
                let x = cells(of: a)[col], y = cells(of: b)[col]
                var r: Int
                if let dx = Double(x), let dy = Double(y) {
                    r = dx < dy ? -1 : (dx > dy ? 1 : 0)
                } else {
                    r = x.localizedStandardCompare(y).rawValue
                }
                if r != 0 { return ascending ? r < 0 : r > 0 }
            }
            return byName(a, b) < 0
        }
    }
}
