import XCTest
@testable import K10sCore

final class ProtectionTests: XCTestCase {
    private var suites: [String] = []

    private func makeDefaults() -> UserDefaults {
        let suite = "k10s.protection.tests.\(UUID().uuidString)"
        suites.append(suite)
        return UserDefaults(suiteName: suite)!
    }

    override func tearDown() {
        for s in suites { UserDefaults(suiteName: s)?.removePersistentDomain(forName: s) }
        suites = []
        super.tearDown()
    }

    func testDefaultWords() {
        let p = Protection(defaults: makeDefaults())
        XCTAssertEqual(p.words, ["prod", "live"])
        XCTAssertTrue(p.isProtected("eu-prod-1"))
        XCTAssertFalse(p.isProtected("kind-dev"))
    }

    func testCaseInsensitiveMatch() {
        let p = Protection(defaults: makeDefaults())
        XCTAssertTrue(p.isProtected("gke_meethue-LIVE_x"))
        XCTAssertEqual(p.reason("gke_meethue-LIVE_x"), .word("live"))
    }

    func testOverrideProtectedBeatsNoWord() {
        let p = Protection(defaults: makeDefaults())
        p.setOverride("kind-dev", .protected)
        XCTAssertTrue(p.isProtected("kind-dev"))
    }

    func testOverrideNotProtectedBeatsWord() {
        let p = Protection(defaults: makeDefaults())
        p.setOverride("eu-prod-1", .notProtected)
        XCTAssertFalse(p.isProtected("eu-prod-1"))
        p.setOverride("eu-prod-1", nil)
        XCTAssertTrue(p.isProtected("eu-prod-1"))
    }

    func testReasonAndLabel() {
        let p = Protection(defaults: makeDefaults())
        p.setOverride("a", .protected)
        p.setOverride("b", .notProtected)
        XCTAssertEqual(p.reason("a"), .override(.protected))
        XCTAssertEqual(p.reason("a").label, "Protected")
        XCTAssertEqual(p.reason("b"), .override(.notProtected))
        XCTAssertEqual(p.reason("b").label, "Not protected")
        XCTAssertEqual(p.reason("eu-prod-1"), .word("prod"))
        XCTAssertEqual(p.reason("eu-prod-1").label, "Automatic · prod")
        XCTAssertEqual(p.reason("kind-dev"), .none)
        XCTAssertEqual(p.reason("kind-dev").label, "Automatic")
    }

    func testFirstMatchingWordInListOrder() {
        let p = Protection(defaults: makeDefaults())
        p.setWords("live, prod")
        XCTAssertEqual(p.reason("prod-live"), .word("live"))
    }

    func testSetWordsParsing() {
        let p = Protection(defaults: makeDefaults())
        p.setWords(" prod , ,live,, staging ")
        XCTAssertEqual(p.words, ["prod", "live", "staging"])
        p.setWords(" , ")
        XCTAssertEqual(p.words, [])
        XCTAssertFalse(p.isProtected("eu-prod-1"))
    }

    func testPersistenceRoundTrip() {
        let d = makeDefaults()
        let a = Protection(defaults: d)
        a.setWords("prod, eu")
        a.setOverride("kind-dev", .protected)
        a.setOverride("gone-context", .notProtected)
        let b = Protection(defaults: d)
        XCTAssertEqual(b.words, ["prod", "eu"])
        XCTAssertEqual(b.overrides, ["kind-dev": .protected, "gone-context": .notProtected])
    }

    func testStoredEmptyWordsStaysEmpty() {
        let d = makeDefaults()
        Protection(defaults: d).setWords("")
        XCTAssertEqual(Protection(defaults: d).words, [])
    }

    func testConfirmRule() {
        XCTAssertTrue(ConfirmRule.needsTypedName(protected: true))
        XCTAssertFalse(ConfirmRule.needsTypedName(protected: false))
        XCTAssertTrue(ConfirmRule.matches(typed: "  web-1 \n", name: "web-1"))
        XCTAssertFalse(ConfirmRule.matches(typed: "Web-1", name: "web-1"))
        XCTAssertFalse(ConfirmRule.matches(typed: "", name: "web-1"))
    }
}
