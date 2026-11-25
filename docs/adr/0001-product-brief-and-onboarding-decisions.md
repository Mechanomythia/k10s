# ADR 0001: k10s product brief and onboarding decisions

- **Status:** Accepted
- **Date:** 2026-11-25
- **Code:** none yet (repository had no commits; `main` now holds one empty commit)
- **Builds on:** nothing
- **Werkbank:** proposal #1, revision 1

## Context

`Mechanomythia/k10s` was empty: no architecture to map, no records, no CI. This record holds the brief and the decisions the first build proposals rest on.

## The brief (from the owner)

k10s is a native macOS app version of k9s.

- **Parity.** What k9s does, which is close to everything kubectl does: every resource kind including CRDs, describe, YAML, events, logs, exec, port-forward, scale, restart, edit, delete.
- **Design.** The hyad design system as iris uses it in SwiftUI. Sources: `mechanomythia/hyad` `docs/v2/design-system/` (README.md brand book, tokens.json values) and `mechanomythia/iris` `macos/Iris/Sources/Iris/Theme.swift`.
- **Multi-context.** Read several kube contexts at once (one list across clusters, context as a column) and switch between them easily.

## Decisions

1. **Kubernetes client: Go core + SwiftUI, iris style** (question 1). A Go engine on client-go, bundled in the .app as a child process, talks to the SwiftUI app over a small JSON protocol.
2. **Multi-context: tabs hold a context set** (question 2). Each native macOS tab has a scope of one context or several. A one-context tab behaves like k9s; a multi-context tab merges lists with a Context column and shows per-cluster health. Cmd-1..9 switches tabs; context sets can be saved.
3. **v1 scope: read-only, multi-context** (question 3). All resource kinds incl. CRDs, live lists, filter, describe, YAML, events, streamed logs. No writes, exec or port-forward in v1.
4. **Write safety: protected contexts + typed confirm** (question 4). Contexts can be marked protected (default: names matching `prod`). A write in a protected context needs the resource name typed; a write spanning several contexts always lists every target and needs typing; other single-context writes get a plain confirm. Applies from the first proposal that adds writes.
5. **iris: k10s bypasses it** (question 5). k10s uses your own kubeconfig and credentials. iris only redirects `KUBECONFIG` inside Claude sessions, so this needs no work beyond never reading `~/.local/share/iris/claude/`.
6. **Interaction: k9s keys plus Mac** (question 6). `:` command bar (`:deploy`, `:ctx prod`), `/` filter, k9s hotkeys (d describe, l logs, y YAML), plus full mouse, menus and Cmd shortcuts.
7. **Platform: macOS 14+, iris pipeline** (question 7). Developer ID signing, notarization, Sparkle updates from GitHub releases. Not the App Store: its sandbox blocks kubeconfig and exec auth plugins.
8. **First commit** (question 8). `main` was created with one empty commit so onboarding could open a pull request (Mechanomythia/k10s#1).

## Consequences

- Two languages (Go, Swift) and a small IPC protocol, as in iris.
- A Finder-launched app does not inherit the shell's `PATH` or `KUBECONFIG`, so exec auth plugins (gke-gcloud-auth-plugin, aws, kubelogin) would fail unless the engine resolves them from the login shell. Proposal #2 handles it.
- Writes cannot ship until decision 4 is built.

## Later, not in this decision

- Writes (delete, scale, restart, edit), exec shell, port-forward: milestone 2, with decision 4.
- k9s extras (pulses, xray, popeye, benchmarks, plugins, skins): only if missed in daily use.
- Plate and lantern themes: night only until someone asks.
