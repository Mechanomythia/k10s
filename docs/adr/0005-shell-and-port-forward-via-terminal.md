# ADR 0005: Shell and port-forward through your own terminal

- **Status:** Accepted
- **Date:** 2026-11-25
- **Code:** `macos/K10s/Sources/K10sCore/TerminalCommand.swift`, `macos/K10s/Sources/K10s/` (terminal launcher, port-forward sheet, row-menu entries and keys)
- **Builds on:** ADR 0001 (decisions 3, 4, 6), ADR 0003, ADR 0004 (row menu, confirm sheet, protected contexts)
- **Werkbank:** proposal #5, revision 1
- **Design:** https://claude.ai/artifact/XymkWEon85o3frSjoFY1QF (version 9), frames 7 and 12; markup in `docs/design/p4/PortForward.dc.html` and `RowMenu.dc.html`

## Context

Exec and port-forward were planned as engine streams with a terminal emulator inside the inspector. The owner prefers their own terminal, which already has `kubectl` with the same kubeconfig and login-shell PATH the engine uses (ADR 0002).

## Decision

1. **Open shell** (`s` on a pod, row menu, Actions menu) opens your terminal running `kubectl --context <ctx> -n <ns> exec -it <pod> -c <container> -- sh -c 'command -v bash >/dev/null && exec bash || exec sh'`. A pod with several containers gets a picker first (from `pod.containers`); the default is the first container.
2. **Port-forward** (`⇧F` on pods, services and deployments): the frame-12 sheet with local port → target port (prefilled from a service's PORT(S) column; typed for pods and deployments), a port already in use on your Mac is refused, then `kubectl --context <ctx> -n <ns> port-forward <kind>/<name> <local>:<remote>` opens in your terminal. The forward lives as long as that window.
3. **How the terminal opens:** a short-lived `.command` script in `~/Library/Caches/com.mechanomythia.k10s/` (mode 0700, removed after a minute) opened with `open`, so macOS uses your default `.command` app (Terminal unless changed). No Automation permission. The script checks `command -v kubectl` first and prints "kubectl is not on your PATH. Install it or add it to your shell profile." when missing, then `exec`s the command.
4. **Quoting:** every value in the script is single-quoted with embedded quotes escaped.
5. **Protected contexts:** Open shell gets a plain confirm, no typed name. Port-forward needs no confirm.

## Later, not in this decision

- A terminal inside k10s; remembering forwards; choosing a terminal app in Settings.

## Consequences

- No engine changes. Requires kubectl on your Mac. The script holds no credentials.
