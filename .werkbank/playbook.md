# k10s playbook (draft)

Draft from onboarding (proposal #1). The repository is empty: commands below follow the iris layout and become real once the foundation proposal lands. Anything marked (pending q1) depends on the Kubernetes client decision.

## What k10s is

A native macOS app version of k9s: every kubectl-level operation in a SwiftUI interface, with multi-context (read several kube contexts at once, switch between them easily).

## Layout (pending q1)

- `cmd/`, `internal/`: Go engine on client-go (kubeconfig, discovery, watches, logs, exec, port-forward).
- `macos/K10s/`: SwiftUI app (Swift Package, like `iris/macos/Iris`). Minimum macOS 14.
- `docs/adr/`: decision records (werkbank commits approved proposals here).

## Install, test, build

- Requires Go (version from go.mod) and, for the app, macOS with Xcode command line tools.
- `make test`: `go test -race ./...` (runs on Linux too).
- `make vet`: `go vet ./...`.
- `make app`: builds `dist/K10s.app` (universal, ad-hoc signed without a signing identity). macOS only.
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
- Releases are cut by the owner (workflow_dispatch with a version). Agents never tag or publish.

## Never

- Never push to main; work on a branch and open a pull request.
- Never read, copy or log kubeconfig contents, tokens or exec-plugin output beyond what the engine needs in memory. No credentials in logs, crash reports or test fixtures.
- Never run writes against a real cluster from a session.
- No telemetry or network calls other than to the user's clusters and the Sparkle feed.
