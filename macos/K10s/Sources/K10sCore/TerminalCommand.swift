import Foundation

/// Builds the kubectl command lines k10s hands to your terminal (ADR 0005). Pure; no I/O.
public enum TerminalCommand {
    /// Single-quotes `s` for a POSIX shell; embedded single quotes become '\''.
    public static func shellQuote(_ s: String) -> String {
        "'" + s.replacingOccurrences(of: "'", with: "'\\''") + "'"
    }

    public static func execArgv(context: String, namespace: String, pod: String, container: String) -> [String] {
        ["kubectl", "--context", context, "-n", namespace, "exec", "-it", pod, "-c", container,
         "--", "sh", "-c", "command -v bash >/dev/null && exec bash || exec sh"]
    }

    /// `kind` is the lowercase singular: pod, service, deployment.
    public static func portForwardArgv(context: String, namespace: String, kind: String, name: String,
                                       local: Int, remote: Int) -> [String] {
        ["kubectl", "--context", context, "-n", namespace, "port-forward", "\(kind)/\(name)", "\(local):\(remote)"]
    }

    /// The .command script: checks for kubectl, then execs argv with every element quoted.
    public static func script(argv: [String]) -> String {
        """
        #!/bin/sh
        if ! command -v kubectl >/dev/null 2>&1; then
          echo "kubectl is not on your PATH. Install it or add it to your shell profile."
          read -r _
          exit 1
        fi
        exec \(argv.map(shellQuote).joined(separator: " "))

        """
    }
}
