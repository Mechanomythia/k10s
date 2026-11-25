import K10sCore
import XCTest

private func entry(_ group: String, _ kind: String) -> KindEntry {
    let g = group.isEmpty ? "" : "\"group\":\"\(group)\","
    let json = "{\(g)\"version\":\"v1\",\"kind\":\"\(kind)\",\"plural\":\"\(kind.lowercased())s\",\"namespaced\":true}"
    let r = try! JSONDecoder().decode(ResourceKind.self, from: Data(json.utf8))
    return KindCatalog(perContext: [[r]]).kinds[0]
}

private func row(_ name: String) -> Row {
    let json = "{\"context\":\"c\",\"name\":\"\(name)\",\"uid\":\"u-\(name)\",\"cells\":[\"\(name)\"],\"health\":\"ok\"}"
    return try! JSONDecoder().decode(Row.self, from: Data(json.utf8))
}

final class LevelsTests: XCTestCase {
    private func level(_ name: String, filter: String, sort: String?, selection: Row?) -> Level {
        Level(parent: row(name), parentKind: entry("apps", "Deployment"), children: .containers,
              covered: CoveredList(filter: filter, sortColumn: sort, ascending: false, wide: true, selection: selection))
    }

    func testPushPopRestoresCoveredState() {
        var s = LevelStack()
        XCTAssertTrue(s.isEmpty)
        XCTAssertNil(s.top)
        s.push(level("a", filter: "app=web", sort: "Age", selection: row("a")))
        s.push(level("b", filter: "", sort: nil, selection: nil))
        XCTAssertEqual(s.top?.parent.name, "b")
        XCTAssertNil(s.pop()?.covered.selection)
        let covered = s.pop()?.covered
        XCTAssertEqual(covered?.filter, "app=web")
        XCTAssertEqual(covered?.sortColumn, "Age")
        XCTAssertEqual(covered?.ascending, false)
        XCTAssertEqual(covered?.wide, true)
        XCTAssertEqual(covered?.selection?.uid, "u-a")
        XCTAssertTrue(s.isEmpty)
        XCTAssertNil(s.pop())
    }

    func testClear() {
        var s = LevelStack()
        s.push(level("a", filter: "", sort: nil, selection: nil))
        s.push(level("b", filter: "", sort: nil, selection: nil))
        s.clear()
        XCTAssertTrue(s.isEmpty)
        XCTAssertNil(s.top)
    }

    func testChildQueryFromResult() throws {
        let json = "{\"resource\":{\"version\":\"v1\",\"plural\":\"pods\"},\"namespace\":\"d\",\"labelSelector\":\"app=web\"}"
        let r = try JSONDecoder().decode(ChildrenResult.self, from: Data(json.utf8))
        let q = ChildQuery(kind: entry("", "Pod"), result: r)
        XCTAssertEqual(q.labelSelector, "app=web")
        XCTAssertEqual(q.namespace, "d")
        XCTAssertFalse(q.none)
    }

    func testChildKinds() {
        for (g, k) in [("apps", "Deployment"), ("apps", "StatefulSet"), ("apps", "DaemonSet"), ("apps", "ReplicaSet"),
                       ("batch", "Job"), ("batch", "CronJob"), ("", "Service"), ("", "Node")] {
            XCTAssertEqual(entry(g, k).childKind, .table, "\(g)/\(k)")
        }
        XCTAssertEqual(entry("", "Pod").childKind, .containers)
        XCTAssertNil(entry("", "ConfigMap").childKind)
        XCTAssertNil(entry("example.com", "Deployment").childKind)
        XCTAssertNil(entry("", "Deployment").childKind)
    }

    func testJoinSelectors() {
        XCTAssertEqual(joinSelectors("app=web", "tier=a"), "app=web,tier=a")
        XCTAssertEqual(joinSelectors("app=web", nil), "app=web")
        XCTAssertEqual(joinSelectors("", " tier=a "), "tier=a")
        XCTAssertNil(joinSelectors(nil, nil))
        XCTAssertNil(joinSelectors("", " "))
    }
}
