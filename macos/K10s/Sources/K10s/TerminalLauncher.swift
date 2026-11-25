import Foundation

/// Opens a command in your terminal through a short-lived .command script (ADR 0005, decision 3).
enum TerminalLauncher {
    struct Failure: LocalizedError {
        let status: Int32
        var errorDescription: String? { "Could not open your terminal (open exited with \(status))." }
    }

    @MainActor
    static func open(argv: [String]) throws {
        let fm = FileManager.default
        let dir = fm.homeDirectoryForCurrentUser
            .appendingPathComponent("Library/Caches/com.mechanomythia.k10s", isDirectory: true)
        try fm.createDirectory(at: dir, withIntermediateDirectories: true, attributes: [.posixPermissions: 0o700])
        // An existing directory keeps its mode; tighten it.
        try fm.setAttributes([.posixPermissions: 0o700], ofItemAtPath: dir.path)
        let file = dir.appendingPathComponent("k10s-\(UUID().uuidString).command")
        let created = fm.createFile(atPath: file.path, contents: Data(TerminalCommand.script(argv: argv).utf8),
                                    attributes: [.posixPermissions: 0o700])
        guard created else { throw CocoaError(.fileWriteUnknown) }
        let open = Process()
        open.executableURL = URL(fileURLWithPath: "/usr/bin/open")
        open.arguments = [file.path]
        try open.run()
        open.waitUntilExit()
        guard open.terminationStatus == 0 else {
            try? fm.removeItem(at: file)
            throw Failure(status: open.terminationStatus)
        }
        Task.detached {
            try? await Task.sleep(nanoseconds: 60_000_000_000)
            try? FileManager.default.removeItem(at: file)
        }
    }
}
