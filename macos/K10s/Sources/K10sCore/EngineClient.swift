import Foundation
import Observation
import os

private let log = Logger(subsystem: "com.mechanomythia.k10s", category: "engine")

/// One event of a subscription: its name and the raw JSON of its data.
public struct EngineEvent {
    public let name: String
    public let data: Data

    public func decode<T: Decodable>(_ type: T.Type = T.self) throws -> T {
        try JSONDecoder().decode(type, from: data)
    }
}

public struct EngineFailure: Error, LocalizedError {
    public let errorDescription: String?
    init(_ message: String) { errorDescription = message }
}

/// Runs the engine process and speaks the line protocol over its stdio. If the
/// engine exits, pending requests and streams fail, it is restarted once, and
/// a second exit sets `errorMessage` and leaves it stopped.
@MainActor @Observable
public final class EngineClient {
    /// Set when the engine could not be started or stopped for good.
    public private(set) var errorMessage: String?
    /// How often the engine was restarted. Views key their requests on it.
    public private(set) var restarts = 0

    @ObservationIgnored private var process: Process?
    @ObservationIgnored private var stdin: FileHandle?
    @ObservationIgnored private var generation = 0
    @ObservationIgnored private var stopped = false
    @ObservationIgnored private var nextID = 0
    @ObservationIgnored private var pending: [String: (Result<JSONValue?, Error>) -> Void] = [:]
    @ObservationIgnored private var subs: [String: AsyncThrowingStream<EngineEvent, Error>.Continuation] = [:]

    public init() {}

    public func request<P: Encodable, R: Decodable>(_ method: String, _ params: P) async throws -> R {
        guard let result = try await call(method, params) else { throw EngineFailure("Empty result from \(method).") }
        return try JSONDecoder().decode(R.self, from: JSONEncoder().encode(result))
    }

    /// Cancelling (or finishing) the stream sends `unsubscribe`.
    public func subscribe<P: Encodable>(_ method: String, params: P) async throws -> AsyncThrowingStream<EngineEvent, Error> {
        let (stream, cont) = AsyncThrowingStream<EngineEvent, Error>.makeStream()
        // Registered while the response is handled, so no event can arrive first.
        let result = try await call(method, params) { [self] value in
            guard let value,
                  let r = try? JSONDecoder().decode(SubscribeResult.self, from: JSONEncoder().encode(value)) else { return }
            subs[r.sub] = cont
            cont.onTermination = { [weak self] _ in
                Task { @MainActor in self?.endSub(r.sub) }
            }
        }
        if result == nil { throw EngineFailure("No subscription from \(method).") }
        return stream
    }

    // MARK: calls

    private func call<P: Encodable>(_ method: String, _ params: P,
                                    onResult: @escaping (JSONValue?) -> Void = { _ in }) async throws -> JSONValue? {
        if process == nil { start() }
        return try await withCheckedThrowingContinuation { cont in
            do {
                let id = String(nextID)
                nextID += 1
                let p = try JSONDecoder().decode(JSONValue.self, from: JSONEncoder().encode(params))
                try write(Request(id: id, method: method, params: p))
                pending[id] = { r in
                    if case .success(let v) = r { onResult(v) }
                    cont.resume(with: r)
                }
            } catch {
                cont.resume(throwing: error)
            }
        }
    }

    private func write(_ req: Request) throws {
        guard let stdin else { throw EngineFailure(errorMessage ?? "The engine is not running.") }
        try stdin.write(contentsOf: JSONEncoder().encode(req) + Data("\n".utf8))
    }

    private func endSub(_ sub: String) {
        guard subs.removeValue(forKey: sub) != nil else { return }
        try? write(Request(id: "u\(nextID)", method: Method.unsubscribe,
                           params: .object(["sub": .string(sub)])))
        nextID += 1
    }

    // MARK: process

    private func engineURL() -> URL? {
        if let url = Bundle.main.resourceURL?.appendingPathComponent("bin/k10s-engine"),
           FileManager.default.isExecutableFile(atPath: url.path) { return url }
        // `swift run` during development.
        if let path = ProcessInfo.processInfo.environment["K10S_ENGINE"], !path.isEmpty {
            return URL(fileURLWithPath: path)
        }
        return nil
    }

    private func start() {
        guard !stopped else { return }
        signal(SIGPIPE, SIG_IGN) // a dead engine must not kill the app on write
        guard let url = engineURL() else {
            fail("The engine binary was not found. Set K10S_ENGINE or reinstall the app.")
            return
        }
        let p = Process()
        let inPipe = Pipe(), outPipe = Pipe(), errPipe = Pipe()
        p.executableURL = url
        p.standardInput = inPipe
        p.standardOutput = outPipe
        p.standardError = errPipe
        do { try p.run() } catch {
            fail("The engine did not start: \(error.localizedDescription)")
            return
        }
        generation += 1
        let gen = generation
        process = p
        stdin = inPipe.fileHandleForWriting
        // FileHandle.bytes on a pipe hands data over in large chunks (often only at EOF), so a
        // reply from a long-running engine could sit in the buffer forever. Read what is there
        // as soon as it arrives instead, and deliver complete lines in order on the main thread.
        readLines(errPipe.fileHandleForReading) { line in log.info("\(line, privacy: .private)") }
        readLines(outPipe.fileHandleForReading, onEnd: { [weak self] in self?.exited(gen) }) { [weak self] line in
            guard let self, gen == self.generation else { return }
            self.handle(line)
        }
    }

    /// Splits a pipe into lines as data arrives; `onLine` and `onEnd` run on the main actor, in order.
    private func readLines(_ handle: FileHandle,
                           onEnd: @escaping @MainActor () -> Void = {},
                           onLine: @escaping @MainActor (String) -> Void) {
        let buffer = LineBuffer()
        handle.readabilityHandler = { fh in
            let chunk = fh.availableData
            if chunk.isEmpty { // EOF
                fh.readabilityHandler = nil
                let rest = buffer.data.isEmpty ? nil : String(decoding: buffer.data, as: UTF8.self)
                buffer.data.removeAll()
                DispatchQueue.main.async {
                    MainActor.assumeIsolated {
                        if let rest { onLine(rest) }
                        onEnd()
                    }
                }
                return
            }
            buffer.data.append(chunk)
            var lines: [String] = []
            while let nl = buffer.data.firstIndex(of: 0x0A) {
                lines.append(String(decoding: buffer.data[buffer.data.startIndex..<nl], as: UTF8.self))
                buffer.data.removeSubrange(buffer.data.startIndex...nl)
            }
            guard !lines.isEmpty else { return }
            DispatchQueue.main.async {
                MainActor.assumeIsolated { for line in lines { onLine(line) } }
            }
        }
    }

    private func fail(_ message: String) {
        stopped = true
        errorMessage = message
        log.error("\(message)")
    }

    private func exited(_ gen: Int) {
        guard gen == generation else { return }
        process?.terminate()
        process = nil
        stdin = nil
        let err = EngineFailure("The engine stopped.")
        let calls = pending
        let streams = subs
        pending = [:]
        subs = [:]
        for (_, done) in calls { done(.failure(err)) }
        for (_, s) in streams { s.finish(throwing: err) }
        if restarts < 1 {
            restarts += 1
            start()
        } else {
            fail("The engine stopped twice. Restart k10s.")
        }
    }

    // MARK: lines

    private func handle(_ line: String) {
        let data = Data(line.utf8)
        let decoder = JSONDecoder()
        if let r = try? decoder.decode(Response.self, from: data) {
            guard let done = pending.removeValue(forKey: r.id) else { return }
            done(r.error.map { .failure($0) } ?? .success(r.result))
        } else if let e = try? decoder.decode(Event.self, from: data) {
            guard let payload = try? JSONEncoder().encode(e.data) else { return }
            subs[e.sub]?.yield(EngineEvent(name: e.event, data: payload))
        } else {
            log.error("Unreadable line from the engine.")
        }
    }
}

/// Bytes read from a pipe that do not end in a newline yet. Only touched from the
/// FileHandle's own serial readability queue.
private final class LineBuffer: @unchecked Sendable {
    var data = Data()
}
