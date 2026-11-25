import XCTest
@testable import K10sCore

final class TerminalCommandTests: XCTestCase {
    private func q(_ s: String) -> String { TerminalCommand.shellQuote(s) }

    func testQuoting() {
        XCTAssertEqual(q("plain"), "'plain'")
        XCTAssertEqual(q("a b"), "'a b'")
        XCTAssertEqual(q("it's"), "'it'\\''s'")
        XCTAssertEqual(q("$HOME"), "'$HOME'")
        XCTAssertEqual(q("`id`"), "'`id`'")
        XCTAssertEqual(q("x; rm -rf /"), "'x; rm -rf /'")
        XCTAssertEqual(q("a\nb"), "'a\nb'")
        XCTAssertEqual(q(""), "''")
    }

    func testExecScriptText() {
        let argv = TerminalCommand.execArgv(context: "prod's", namespace: "ns", pod: "p", container: "c")
        let expected = """
        #!/bin/sh
        if ! command -v kubectl >/dev/null 2>&1; then
          echo "kubectl is not on your PATH. Install it or add it to your shell profile."
          read -r _
          exit 1
        fi
        exec 'kubectl' '--context' 'prod'\\''s' '-n' 'ns' 'exec' '-it' 'p' '-c' 'c' '--' 'sh' '-c' 'command -v bash >/dev/null && exec bash || exec sh'

        """
        XCTAssertEqual(TerminalCommand.script(argv: argv), expected)
    }

    func testPortForwardArgv() {
        XCTAssertEqual(
            TerminalCommand.portForwardArgv(context: "c", namespace: "n", kind: "service", name: "web", local: 8080, remote: 80),
            ["kubectl", "--context", "c", "-n", "n", "port-forward", "service/web", "8080:80"])
    }
}
