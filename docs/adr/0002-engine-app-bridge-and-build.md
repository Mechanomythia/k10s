# ADR 0002: Multi-context read engine, app bridge and build pipeline

- **Status:** Accepted
- **Date:** 2026-11-25
- **Code:** `cmd/k10s-engine/`, `internal/engine/`, `macos/K10s/`, `Makefile`, `.github/workflows/build.yml`
- **Builds on:** ADR 0001 (decisions 1, 2, 3, 5, 7)
- **Werkbank:** proposal #2, revision 1

## Context

The repository is empty. ADR 0001 settled a Go engine on client-go behind a SwiftUI app (decision 1), tabs that hold a set of contexts (2), a read-only v1 (3), plain kubeconfig without iris (5) and macOS 14+ with iris's release pipeline (7).

The v1 is too big for one build, and its UI needs a design phase first. This record is everything under the window: the engine, the protocol, the Swift client and the build. The main window is the next proposal, designed first.

Two facts shape the engine:
- **Server-side tables.** The API server prints columns for any kind when asked with `Accept: application/json;as=Table;v=v1;g=meta.k8s.io`, including CRDs' `additionalPrinterColumns`, and watches work in that format too. This is how kubectl prints. It gives k9s-like columns for every kind without writing a column map per kind.
- **GUI environment.** An app launched from Finder gets a minimal `PATH` and no `KUBECONFIG`, so exec auth plugins in `/opt/homebrew/bin` and multi-file kubeconfigs silently fail.

## Decision

1. **Process and protocol.** `k10s-engine` is a Go binary the app starts as a child process. They talk over stdin/stdout in newline-delimited JSON: requests with an `id` get one response; subscriptions get a stream of events until `unsubscribe`. Stderr is the engine log, with no credentials in it. If the engine exits, the app restarts it once and then shows the error. The protocol is defined once in `internal/engine/protocol.go` and mirrored in `macos/K10s/Sources/K10s/Engine/Protocol.swift`, with a golden JSON test on both sides.
2. **Kubeconfig and environment.** At start the engine reads `PATH` and `KUBECONFIG` from your login shell (`$SHELL -l -c env`, 5 s timeout, fallback to `~/.kube/config` and a `PATH` with `/opt/homebrew/bin:/usr/local/bin`). It loads kubeconfig with clientcmd's standard loading rules and ignores any path under `~/.local/share/iris/claude/`. It watches those files and sends `contexts.changed` when they change. `contexts.list` returns name, cluster, user, namespace, and whether it is the current context.
3. **Lazy connections.** A context connects on first use, never at start, because exec plugins can open browser tabs or prompts. Each connection reports a state: `connecting`, `live`, `unreachable` or `unauthorized`, with the error text. One REST config and discovery cache per context, kept for the app's lifetime.
4. **Discovery.** `resources.list {context}` returns every listable kind (group, version, kind, plural, short names, namespaced) from discovery, CRDs included, so `:deploy` and `:dp` can resolve on the Swift side.
5. **Live tables across contexts.** `table.subscribe {contexts[], resource, namespace|all, labelSelector?}` lists and then watches in Table format, one watch per (context, resource, namespace). Watches are shared between subscribers and stopped 30 s after the last one leaves. Events: `snapshot` (columns + rows), `upsert`, `delete`, `context.state`. Each row carries context, namespace, name, uid, the printed cells and a `health` (`ok`, `pending`, `degraded`, `failed`, `stopped`, `none`) for the star glyph, from a small rule set: Pod phase and container states (CrashLoopBackOff is failed, restarts climbing in the last 10 minutes is degraded), workload ready vs desired replicas (0 desired is stopped), Job conditions, Node Ready; everything else `none`. Columns come from the first context that answers; other contexts' cells are matched by column name. A context that fails does not stop the others.
6. **Detail requests.** `object.yaml` (managedFields stripped), `object.describe` (kubectl's own `k8s.io/kubectl/pkg/describe`, falling back to its generic describer for CRDs), `object.events` (events whose involvedObject is the uid, newest first).
7. **Logs.** `logs.subscribe {context, namespace, pod, container?, previous?, tailLines=500, follow=true, timestamps?}` streams lines in batches every 100 ms at most. A pod with several containers needs a container, except `allContainers=true`, which prefixes each line with the container name.
8. **Swift client.** `macos/K10s` is a Swift package like `iris/macos/Iris`: `EngineClient` (starts the engine from `Contents/Resources/bin`, async requests, `AsyncStream` subscriptions) and the theme copied verbatim from iris's `Theme.swift`. The window in this record is a stub: a context picker and a plain list of one `table.subscribe`, enough to prove the bridge end to end. The real window is the next proposal.
9. **Build and release.** `Makefile` (`test`, `vet`, `build`, `app`) and `macos/K10s/build-app.sh` and `.github/workflows/build.yml`, adapted from iris: universal Go and Swift binaries, fonts, ad-hoc signing without secrets, Developer ID signing, notarization and Sparkle when the secrets exist, releases only by workflow_dispatch. Bundle id `com.mechanomythia.k10s`.
10. **Tests.** Engine tests use client-go's fake dynamic client and an `httptest` API server that returns Table responses and watch streams; no real clusters, never the owner's kubeconfig. Covered: login-shell env parsing and fallback, iris path ignored, watch sharing and teardown, a failing context not affecting others, health rules, protocol golden files. CI runs Go tests on Linux and builds the app on macOS.

Size: 7 tickets, about 1,900 lines of Go and Swift plus 1,000 of tests.

## Later, not in this decision

- The main window (tabs with context sets, lists, inspector, command bar, k9s keys): the next proposal, with a design phase.
- Writes, exec, port-forward and protected contexts: milestone 2.
- Metrics columns (CPU/memory from metrics-server): when the list view exists and you miss them.
- Persisting watches across app restarts, offline caches: not needed for a live tool.

## Consequences

- **Security.** Credentials stay in the engine's memory as client-go handles them. The engine never logs kubeconfig contents, tokens or exec-plugin output. Exec plugins only run when you open a context.
- **Load on clusters.** One watch per (context, kind, namespace) in use, shared across tabs, closed 30 s after use. Opening all Pods across 10 contexts is 10 watches.
- **Operations.** Signing, notarization and Sparkle need the six secrets named in the playbook; without them the build is ad-hoc signed and runs on your Mac only after `xattr -cr`.
