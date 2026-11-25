import K10sCore
import XCTest

private func codec<T: Codable>(_ type: T.Type) -> (Data) throws -> Data {
    { try JSONEncoder().encode(try JSONDecoder().decode(type, from: $0)) }
}

/// Every golden file in internal/engine/testdata/protocol must decode into its
/// Swift type and encode back to the same JSON.
final class ProtocolTests: XCTestCase {
    private let types: [String: (Data) throws -> Data] = [
        "contexts.changed.event.json": codec(ContextsChanged.self),
        "contexts.list.result.json": codec(ContextsListResult.self),
        "envelope.event.json": codec(Event.self),
        "envelope.request.json": codec(Request.self),
        "envelope.response.error.json": codec(Response.self),
        "envelope.response.result.json": codec(Response.self),
        "logs.end.error.event.json": codec(End.self),
        "logs.end.event.json": codec(End.self),
        "logs.lines.event.json": codec(Lines.self),
        "logs.subscribe.params.json": codec(LogsSubscribeParams.self),
        "object.describe.params.json": codec(ObjectParams.self),
        "object.describe.result.json": codec(TextResult.self),
        "object.events.params.json": codec(ObjectEventsParams.self),
        "object.events.result.json": codec(ObjectEventsResult.self),
        "object.yaml.params.json": codec(ObjectParams.self),
        "object.yaml.result.json": codec(TextResult.self),
        "resources.list.params.json": codec(ResourcesListParams.self),
        "resources.list.result.json": codec(ResourcesListResult.self),
        "subscribe.result.json": codec(SubscribeResult.self),
        "table.context.state.event.json": codec(ContextStateChange.self),
        "table.context.state.live.event.json": codec(ContextStateChange.self),
        "table.delete.event.json": codec(Delete.self),
        "table.snapshot.event.json": codec(Snapshot.self),
        "table.subscribe.all.params.json": codec(TableSubscribeParams.self),
        "table.subscribe.params.json": codec(TableSubscribeParams.self),
        "table.upsert.event.json": codec(Upsert.self),
        "unsubscribe.params.json": codec(UnsubscribeParams.self),
    ]

    func testGoldenFilesRoundTrip() throws {
        // macos/K10s/Tests/K10sTests/ProtocolTests.swift -> repository root.
        var root = URL(fileURLWithPath: #filePath)
        for _ in 0..<5 { root.deleteLastPathComponent() }
        let dir = root.appendingPathComponent("internal/engine/testdata/protocol")
        let names = try FileManager.default.contentsOfDirectory(atPath: dir.path)
            .filter { $0.hasSuffix(".json") }
            .sorted()
        XCTAssertFalse(names.isEmpty, "no golden files in \(dir.path)")
        for name in names {
            guard let roundTrip = types[name] else {
                XCTFail("golden file \(name) has no Swift type")
                continue
            }
            let golden = try Data(contentsOf: dir.appendingPathComponent(name))
            let again = try roundTrip(golden)
            let a = try XCTUnwrap(JSONSerialization.jsonObject(with: golden) as? NSDictionary, name)
            let b = try XCTUnwrap(JSONSerialization.jsonObject(with: again) as? NSDictionary, name)
            XCTAssertEqual(a, b, name)
        }
    }
}
