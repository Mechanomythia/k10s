# k10s playbook

From onboarding (ADR 0001) and the engine (ADR 0002).

## What k10s is

A native macOS app version of k9s: every kubectl-level operation in a SwiftUI interface, with multi-context (read several kube contexts at once, switch between them easily).

## Layout

- `cmd/k10s-engine`, `internal/engine/`: Go engine on client-go, a child process of the app speaking newline-delimited JSON over stdio. `protocol.go` is the source of truth; `testdata/protocol/*.json` are golden files the Swift tests decode too, so a protocol change updates protocol.go, its golden file and `macos/K10s/Sources/K10sCore/Protocol.swift` together.
- `cmd/k10s-release`, `internal/sparkle/`: appcast signing for releases.
- `macos/K10s/`: SwiftUI app (Swift Package, like `iris/macos/Iris`). Minimum macOS 14. Targets: `K10sCore` (protocol + EngineClient, no UI), `K10s` (app, Sparkle), `K10sTests`. Run from source with `K10S_ENGINE=$(pwd)/bin/k10s-engine swift run` after `make build`.
- `docs/adr/`: decision records, numbered `NNNN-slug.md`. ADR 0001 holds the product brief and the founding decisions.

## Install, test, build

- Requires Go (version from go.mod) and, for the app, macOS with Xcode command line tools.
- `make test`: `go test -race ./...` (runs on Linux too).
- `make vet`: `go vet ./...`.
- `make app`: builds `dist/K10s.app` and `dist/K10s-$VERSION.zip` (universal, ad-hoc signed without a signing identity). macOS only.
- `cd macos/K10s && swift test`: Swift protocol tests. macOS only.
- Linux sessions (werkbank routines) can run Go tests and cross-compile darwin (`GOOS=darwin GOARCH=arm64 go build ./...`), not the Swift app. CI (GitHub Actions, macos-15 runner) builds the app.
- Tests never need a real cluster: use client-go fake clients or envtest. Never point tests at the owner's kubeconfig.

## Design

- The hyad design system, night theme. Brand book: `mechanomythia/hyad` `docs/v2/design-system/README.md`; values: `tokens.json` there.
- SwiftUI translation to reuse: `mechanomythia/iris` `macos/Iris/Sources/Iris/Theme.swift` (Hy tokens, HyLabel, StarGlyph, HyTag, HyButton, HyToggle, Plate). Copy it; do not re-derive colours.
- Light is an event: only live/selected/focused things glow. Colour only for state (spectral classes). Square plates, 1 px neatlines, no gradients, no emoji, no space vocabulary in copy. Pod/workload state maps to Star glyphs: Running live, Pending building, degraded (e.g. restarts, partial ready), Failed/CrashLoop failed, Completed/scaled-to-zero stopped.
- Copy: plain, dry, exact, sentence case, "you" never "we". Resource names, contexts and namespaces in mono.

## Release

- Developer ID signing, notarization and Sparkle updates from GitHub releases, copied from iris (`build-app.sh`, `.github/workflows/build.yml`, `docs/notarization.md`). Not the App Store: its sandbox blocks kubeconfig and exec auth plugins.
- Secrets (names only, values in GitHub Actions secrets): MACOS_CERTIFICATE_P12, MACOS_CERTIFICATE_PASSWORD, NOTARY_KEY_P8, NOTARY_KEY_ID, NOTARY_ISSUER_ID, SPARKLE_ED_PRIVATE_KEY. The owner adds them; agents never handle their values.
- Releases are cut by the owner, or by an agent when the owner asks (workflow_dispatch on main with `release_version`). Agents never tag or publish otherwise.
- Sparkle compares `CFBundleVersion`, which is the CI run number (`BUILD`), so any later build outranks an earlier one, release or dev. `CFBundleShortVersionString` and `sparkle:shortVersionString` carry the human version (`0.1.1`, or `0.1.2-dev.N` for dev builds). Never put a version string with `-` into `CFBundleVersion`: Sparkle treats `-` as a separator, so `0.1.0-dev.30` would outrank `0.1.0`.
- After each release X.Y.Z, bump the dev prefix in `.github/workflows/build.yml` to `X.Y.(Z+1)-dev.{0}` and `VERSION` in the Makefile. This is only for the displayed version.

## Never

- Never push to main; work on a branch and open a pull request.
- Never read, copy or log kubeconfig contents, tokens or exec-plugin output beyond what the engine needs in memory. No credentials in logs, crash reports or test fixtures.
- Never run writes against a real cluster from a session.
- Never read kubeconfig from `~/.local/share/iris/claude/`: that is iris's Claude-only config; k10s uses your own (ADR 0001, decision 5).
- No telemetry or network calls other than to the user's clusters and the Sparkle feed.
