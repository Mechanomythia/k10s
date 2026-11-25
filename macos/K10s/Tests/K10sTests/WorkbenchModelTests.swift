import K10sCore
import XCTest

private func decode<T: Decodable>(_ json: String) -> T {
    try! JSONDecoder().decode(T.self, from: Data(json.utf8))
}

private func kind(_ group: String?, _ kind: String, _ plural: String, _ short: [String] = []) -> ResourceKind {
    let g = group.map { "\"group\":\"\($0)\"," } ?? ""
    let s = short.map { "\"\($0)\"" }.joined(separator: ",")
    return decode("{\(g)\"version\":\"v1\",\"kind\":\"\(kind)\",\"plural\":\"\(plural)\",\"shortNames\":[\(s)],\"namespaced\":true}")
}

private func row(_ ctx: String, _ name: String, _ cells: [String], uid: String? = nil) -> Row {
    let c = ([name] + cells + ["10.0.0.1"]).map { "\"\($0)\"" }.joined(separator: ",")
    return decode("{\"context\":\"\(ctx)\",\"name\":\"\(name)\",\"uid\":\"\(uid ?? ctx + name)\",\"cells\":[\(c)],\"health\":\"ok\"}")
}

private func upsert(_ r: Row) -> Upsert {
    let enc = String(data: try! JSONEncoder().encode(r), encoding: .utf8)!
    return decode("{\"row\":\(enc)}")
}

private func change(_ c: String, _ s: String) -> ContextStateChange {
    decode("{\"context\":\"\(c)\",\"state\":\"\(s)\"}")
}

private func snapshot(_ rows: [Row]) -> Snapshot {
    let enc = String(data: try! JSONEncoder().encode(rows), encoding: .utf8)!
    return decode("{\"columns\":[{\"name\":\"Name\"},{\"name\":\"Restarts\",\"priority\":0},{\"name\":\"IP\",\"priority\":1}],\"rows\":\(enc)}")
}

final class WorkbenchModelTests: XCTestCase {
    private let a = [
        kind("", "Pod", "pods", ["po"]), kind("apps", "Deployment", "deployments", ["deploy"]),
        kind("", "Event", "events", ["ev"]), kind("events.k8s.io", "Event", "events"),
        kind("", "ConfigMap", "configmaps", ["cm"]), kind("", "Node", "nodes", ["no"]),
        kind("acme.io", "Widget", "widgets"),
    ]
    private let b = [kind("", "Pod", "pods", ["po"]), kind("acme.io", "Gadget", "gadgets"), kind("acme.io", "Widget", "widgets")]

    func testResolveAndRanking() {
        let cat = KindCatalog(perContext: [a])
        for q in ["dp", "deploy", "deployments", "Deployment"] { XCTAssertEqual(cat.resolve(q)?.plural, "deployments", q) }
        XCTAssertEqual(cat.resolve("po")?.plural, "pods")
        XCTAssertEqual(cat.resolve("events")?.group, "")
        XCTAssertNil(cat.resolve("dep"))
        // exact alias first, then plural prefix, then kind prefix
        let c = KindCatalog(perContext: [[kind("", "Pod", "pods", ["po"]), kind("x.io", "Pool", "pools"), kind("x.io", "Pocket", "things")]])
        XCTAssertEqual(c.complete("po").map(\.plural), ["pods", "pools", "things"])
        XCTAssertEqual(c.complete("po").first?.plural, "pods")
    }

    func testSectionsAndUnionDedupe() {
        let cat = KindCatalog(perContext: [a, b])
        XCTAssertEqual(cat.kinds.filter { $0.plural == "pods" }.count, 1)
        XCTAssertEqual(cat.kinds.filter { $0.plural == "widgets" }.count, 1)
        let s = Dictionary(uniqueKeysWithValues: cat.sections.map { ($0.title, $0.kinds.map(\.plural)) })
        XCTAssertEqual(s["Workloads"], ["pods", "deployments"])
        XCTAssertEqual(s["Config"], ["configmaps"])
        XCTAssertEqual(s["Cluster"], ["nodes", "events"])
        XCTAssertEqual(s["Custom resources"], ["gadgets", "widgets"])
        XCTAssertNil(s["Network"])
    }

    func testMergeUpsertDelete() {
        var t = TableModel(contexts: ["c1", "c2"])
        t.apply(snapshot([row("c1", "web", ["1"]), row("c2", "web", ["2"])]))
        XCTAssertEqual(t.totalCount, 2)
        t.apply(upsert(row("c1", "web", ["9"])))
        XCTAssertEqual(t.totalCount, 2)
        t.apply(upsert(row("c2", "db", ["0"])))
        XCTAssertEqual(t.totalCount, 3)
        let d: Delete = decode("{\"context\":\"c1\",\"name\":\"web\",\"uid\":\"c1web\"}")
        t.apply(d)
        XCTAssertEqual(t.rows.map { $0.context + "/" + $0.name }, ["c2/db", "c2/web"])
    }

    func testColumns() {
        var one = TableModel(contexts: ["c1"])
        one.apply(snapshot([]))
        XCTAssertEqual(one.columns, ["Name", "Restarts"])
        one.wide = true
        XCTAssertEqual(one.columns, ["Name", "Restarts", "IP"])
        var two = TableModel(contexts: ["c1", "c2"])
        two.apply(snapshot([]))
        XCTAssertEqual(two.columns, ["Context", "Name", "Restarts"])
        let r = row("c2", "x", ["3"])
        XCTAssertEqual(two.cells(of: r), ["c2", "x", "3"])
    }

    func testSort() {
        var t = TableModel(contexts: ["c1", "c2"])
        t.apply(snapshot([row("c2", "web", ["10"]), row("c1", "web", ["9"]), row("c1", "api", ["100"])]))
        XCTAssertEqual(t.rows.map { $0.context + "/" + $0.name }, ["c1/api", "c1/web", "c2/web"])
        t.sortColumn = "Restarts"
        XCTAssertEqual(t.rows.map(\.name), ["web", "web", "api"])
        XCTAssertEqual(t.rows.map { $0.cells[1] }, ["9", "10", "100"])
        t.ascending = false
        XCTAssertEqual(t.rows.map { $0.cells[1] }, ["100", "10", "9"])
    }

    func testFilter() {
        var t = TableModel(contexts: ["c1"])
        t.apply(snapshot([row("c1", "Web-1", ["1"]), row("c1", "db", ["1"])]))
        t.filter = "WEB"
        XCTAssertEqual(t.rows.map(\.name), ["Web-1"])
        XCTAssertEqual(t.filteredCount, 1)
        XCTAssertEqual(t.totalCount, 2)
        XCTAssertNil(t.labelSelector)
        t.filter = "app=web"
        XCTAssertEqual(t.labelSelector, "app=web")
        XCTAssertEqual(t.filteredCount, 2)
    }

    func testHealth() {
        var h = ContextHealth()
        h.apply(change("a", "live"))
        XCTAssertEqual(h.worst(of: ["a"]), .live)
        XCTAssertEqual(h.worst(of: ["a", "unknown"]), .connecting)
        h.apply(change("b", "unauthorized"))
        XCTAssertEqual(h.worst(of: ["a", "unknown", "b"]), .unauthorized)
        h.setVersion("v1.30", for: "a")
        XCTAssertEqual(h.statuses["a"]?.version, "v1.30")
        XCTAssertEqual(GlyphState(ContextState.connecting), .building)
        XCTAssertEqual(GlyphState(ContextState.unreachable), .failed)
        XCTAssertEqual(GlyphState(Health.noGlyph), .noGlyph)
        XCTAssertEqual(GlyphState(Health.pending), .building)
    }

    func testDisplayName() {
        XCTAssertEqual(TabScope(contexts: ["prod-eu"]).displayName, "prod-eu")
        XCTAssertEqual(TabScope(contexts: ["a", "b"], name: "Mine").displayName, "Mine")
        XCTAssertEqual(TabScope(contexts: ["prod-eu-1", "prod-eu-2"]).displayName, "prod-eu · 2 clusters")
        XCTAssertEqual(TabScope(contexts: ["prod-a", "prod-b"]).displayName, "prod · 2 clusters")
        XCTAssertEqual(TabScope(contexts: ["x", "y", "z"]).displayName, "3 clusters")
    }

    func testSavedSetsAndRecent() {
        let suite = "k10s.tests." + UUID().uuidString
        let d = UserDefaults(suiteName: suite)!
        defer { d.removePersistentDomain(forName: suite) }
        let s = SavedSets(defaults: d)
        s.save(name: "prod", contexts: ["a", "b"])
        s.save(name: "dev", contexts: ["c"])
        XCTAssertEqual(SavedSets(defaults: d).list(), [TabScope(contexts: ["c"], name: "dev"), TabScope(contexts: ["a", "b"], name: "prod")])
        s.delete(name: "dev")
        XCTAssertEqual(s.list().map { $0.name }, ["prod"])
        for i in 1...6 { s.noteRecent(TabScope(contexts: ["c\(i)"])) }
        s.noteRecent(TabScope(contexts: ["c4"]))
        XCTAssertEqual(SavedSets(defaults: d).recent.map { $0.contexts[0] }, ["c4", "c6", "c5", "c3", "c2"])
    }
}
