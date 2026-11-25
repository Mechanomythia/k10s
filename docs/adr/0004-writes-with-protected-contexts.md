# ADR 0004: Writes with protected contexts

- **Status:** Accepted
- **Date:** 2026-11-25
- **Code:** `internal/engine/` (writes.go, tables.go, conns.go, protocol.go), `macos/K10s/Sources/` (K10sCore protection model; row menu, sheets, inspector edit mode, settings, toolbar context menu, WindowKeys)
- **Builds on:** ADR 0001 (decisions 3, 4, 6), ADR 0002, ADR 0003
- **Werkbank:** proposal #4, revision 1
- **Design:** https://claude.ai/artifact/XymkWEon85o3frSjoFY1QF (version 9), frames 7–11 and 13; markup copies in `docs/design/p4/`

## Context

k10s is read-only (ADR 0001, decision 3). ADR 0001 decision 4 already settled the safety model for writes: contexts can be protected; a write in a protected context needs the object's name typed; other writes get a plain confirm.

Two things in the current code matter here:
- A 403 on a list or watch is classified as `unauthorized` for the whole context (`conns.go` classify, `tables.go` state reports). On clusters with namespace-scoped RBAC this marks a working context as broken, and writes would hit it constantly.
- The inspector's `e` key opens Events, while in k9s `e` means edit. The writes need `e`, `s`, `r` and `⌃D`, as k9s has them.

## Design

| # | Frame | Markup | Shows |
| --- | --- | --- | --- |
| 7 | Row actions | `RowMenu.dc.html` | Right-click menu on a row with keys: Describe d, YAML y, Events v, Edit e, Scale s, Restart r, Delete ⌃D, Copy name c |
| 8 | Delete, unprotected | `ConfirmDelete.dc.html` | Sheet: "Delete pod?", context and namespace in mono, what happens next, Cancel and a danger button that names the action |
| 9 | Delete, protected | `ConfirmProtected.dc.html` | Same with a PROTECTED tag and a name field; the danger button stays disabled until the name matches |
| 10 | Scale | `Scale.dc.html` | Replicas field with "now 3 · 3 ready", primary "Scale to 5" |
| 11 | Edit YAML | `EditYaml.dc.html` | Inspector YAML in editing mode; changed lines tinted; "changed since you opened it" notice with Reload; Discard, Save anyway, Save |
| 13 | Settings | `Protected.dc.html` | Protected words, plus every context with Automatic · <word> / Protected / Not protected |

## Decision

1. **Engine writes.** New requests, each on one object in one context:
   - `object.delete {context, resource, namespace, name}`: background propagation, default grace period.
   - `workload.scale {context, resource, namespace, name, replicas}`: through the scale subresource (Deployment, StatefulSet, ReplicaSet).
   - `workload.restart {…}`: patches `spec.template.metadata.annotations["kubectl.kubernetes.io/restartedAt"]` with the current time, as `kubectl rollout restart` does (Deployment, StatefulSet, DaemonSet).
   - `object.replace {context, resource, namespace, name, yaml}`: PUT of the edited object. The `resourceVersion` in the YAML makes the server refuse with 409 when the object changed since it was opened. The error code `conflict` is new.
   Errors come back as protocol errors with the API server's message. A 403 is the new code `forbidden`.
2. **403 is per kind, not per context.** A 403 on a list, watch or write is reported for that request only. Tables get a `forbidden {context, message}` event, and the context stays `live`. Only 401, an exec-plugin failure or a refused /version probe make a context `unauthorized`.
3. **Protected contexts: words plus per-context override.** Each context has a setting: **Automatic** (the default), **Protected** or **Not protected**.
   - Automatic means protected when the name contains one of the words; the defaults are `prod, live`.
   - Protected and Not protected override the words either way.
   - Both settings are kept in the app's settings (UserDefaults): the words and a map from context name to override. A context that disappears from the kubeconfig keeps its override.
   - Where you set it: ⌘, → Protected contexts (frame 13) lists every context with its effective state and why ("Automatic · prod"); and right-click on a context in the toolbar → Protect / Stop protecting / Use automatic.
   - The toolbar shows a PROTECTED tag next to a protected context.
4. **Confirm.** Every write goes through a sheet (frames 8, 9, 10). In a protected context, delete, scale, restart and save need the object's name typed. Elsewhere it's one click on the named button. The sheet says what follows.
5. **Where the actions are.** A right-click menu on a row (frame 7), the Actions menu, and k9s keys through `WindowKeys`: `e` edit, `s` scale, `r` restart, `⌃D` delete, `c` copy name. Events moves from `e` to `v`. Actions that don't apply to the kind are left out.
6. **Edit YAML.** `e` opens the inspector's YAML in an editable monospace text view, with status and managedFields left out. Save sends `object.replace`. On `conflict`, the frame-11 notice appears and your text is kept. Reload starts from the current version; Save anyway re-sends your text with the current resourceVersion. Invalid YAML gets one line with the parser's message.
7. **After a write.** The list updates through its existing watch. A failed write shows its error in the sheet, and the sheet stays open.
8. **Status bar.** "read-only" becomes "protected" in protected contexts and disappears elsewhere.

## Later, not in this decision

- Multi-select and bulk writes across rows or contexts.
- Force delete / kill, cordon/drain, rollout undo, labels and annotations.
- An audit log of writes.
- Server-side apply and field-level diff in the editor.
- Syncing protected settings across Macs.

## Consequences

- **Safety.** No write without a sheet, and typed names in protected contexts. The engine refuses nothing on its own: the protection is a UI decision.
- **RBAC.** The 403 change also fixes reads on namespace-scoped RBAC clusters.
- **Keys.** `e` changes meaning in the inspector (Events moves to `v`).
