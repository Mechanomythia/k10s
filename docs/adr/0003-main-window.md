# ADR 0003: Main window: context-set tabs, live lists, inspector, command bar

- **Status:** Accepted
- **Date:** 2026-11-25
- **Code:** `macos/K10s/`, small engine additions in `internal/engine/` where the window needs them
- **Builds on:** ADR 0001 (decisions 2, 3, 6), ADR 0002 (the engine)
- **Werkbank:** proposal #3, revision 1 (approved as its design text)
- **Design:** https://claude.ai/artifact/XymkWEon85o3frSjoFY1QF (version 7); markup copies in `docs/design/p3/`

## Design

| # | Screen | Markup |
| --- | --- | --- |
| 1 | Single-context tab: Pods, row selected, inspector on Describe | `docs/design/p3/Main.dc.html` |
| 2 | Multi-context tab: Deployments across prod-eu, prod-us, prod-ap; prod-ap unreachable shown as one notice row, its rows left out | `docs/design/p3/MultiContext.dc.html` |
| 3 | Command bar `:dep` with kind completions and `:ctx` / `:ns` / `:set` | `docs/design/p3/CommandBar.dc.html` |
| 4 | Logs in a widened inspector: container picker, follow, previous run, timestamps, find | `docs/design/p3/Logs.dc.html` |
| 5 | New tab sheet: saved sets and recent on the left, contexts with state on the right, save as set | `docs/design/p3/NewTab.dc.html` |
| 6 | Empty and error states: no kubeconfig, unauthorized (exec plugin missing), unreachable, no resources | `docs/design/p3/States.dc.html` |

`docs/design/p3/k10s.css` holds the night-theme values the screens use; they are the same values as `Hy` in `macos/K10s/Sources/K10s/Theme.swift`.

### Layout, as drawn

- **Toolbar** (52 px): the tab's contexts, each with its connection star; namespace picker; `/` filter; `:` hint.
- **Tabs** (32 px): native window tabs, each named after its context or set ("prod · 3 clusters") with the worst connection state as its star; Cmd-1..9; "New tab ⌘T" opens screen 5.
- **Kinds sidebar** (208 px, collapsible with Cmd-0): Workloads, Network, Config, Cluster, Custom resources, with counts.
- **List**: kind title in Literata, count and "watching" in mono; server-printed columns; star per row; selected row on `sky-high` with a starlight edge. Multi-context adds a Context column; rows sort by name, then context, so the same object across clusters sits together.
- **Inspector** (480 px, 720 px for logs): Describe, YAML, Events, Logs, each with its k9s key (d, y, e, l).
- **Status bar** (28 px): k9s key hints on the left; context, cluster, server version and "read-only" on the right.
- **States**: an unreachable or unauthorized context never blanks the tab; it becomes a notice above the list (screen 2) or a pane with the exact error and one next step (screen 6). Copy follows hyad's voice: what happened, then what to do.

### Decided in the design phase

- Kinds sidebar stays, collapsible (werkbank question 11).
- Merged rows sort by name, then context (werkbank question 12).

## Scope (from ADR 0001)

- Tabs hold a context set; a one-context tab behaves like k9s, a multi-context tab merges lists with a Context column; Cmd-1..9 switches tabs; context sets can be saved.
- Per-cluster health as a star glyph with its name.
- Live lists for any kind, server-printed columns, star health per row, `/` filter, namespace scope.
- Inspector: Describe, YAML, Events, Logs.
- Command bar `:` with kind and context completion; k9s hotkeys alongside menus and Cmd shortcuts.
- Empty and error states.

Read-only: no write actions appear in v1.
