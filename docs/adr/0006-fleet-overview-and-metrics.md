# ADR 0006: Fleet overview and CPU/memory metrics

- **Status:** Accepted
- **Date:** 2026-11-25
- **Code:** `internal/engine/` (overview.go, metrics.go, tables.go, protocol.go), `macos/K10s/Sources/` (Overview views, sidebar, TableModel health filter, new-tab sheet)
- **Builds on:** ADR 0001 (decision 2), ADR 0002 (decisions 3, 5), ADR 0003
- **Werkbank:** proposal #7, revision 0
- **Design:** https://claude.ai/artifact/XymkWEon85o3frSjoFY1QF (version 11), frames 14–16; markup in `docs/design/p7/`

## Context

Multi-context is what k9s can't do, but k10s still shows clusters only through lists of one kind. With 25 contexts, the first question every morning is "which clusters need me?". k10s also has no CPU or memory figures, which k9s shows on every pod and node. The engine already holds most of the inputs: shared Table watches with the full object per row, health per row, and connection state with server version per context.

## Design

| # | Frame | Markup | Shows |
| --- | --- | --- | --- |
| 14 | Overview, multi-context | `Fleet.dc.html` | One row per cluster: state star, context, version, nodes ready/total, CPU % and memory % of allocatable, pods, failing pods, degraded, workloads not ready, warnings in the last hour. Unreachable clusters as a row of dashes. |
| 15 | Overview, one context | `Pulse.dc.html` | Plates: Capacity, Workloads, Nodes (busiest five), Warnings in the last hour. |
| 16 | Pods with metrics | `PodMetrics.dc.html` | CPU, %CPU/R, %CPU/L, Mem, %MEM/R, %MEM/L; "—" without requests or limits. |

## Decision

1. **Overview is a sidebar entry in every tab**, above Workloads. Multi-context tabs open on it; single-context tabs open on Pods (werkbank question 19). A fleet is a context set; the new-tab sheet gets an "All contexts" entry. Nothing connects until a tab with the context opens.
2. **Engine aggregates.** `overview.subscribe {contexts}` emits a `summary` event per context, at most every 5 s and only when it changed, built in-process from the shared Table watches (pods, nodes, deployments, statefulsets, daemonsets in all namespaces; core events with type=Warning). A summary holds state and version; nodes ready/total; CPU and memory used and allocatable; pods running/total and capacity; failing and degraded pod counts; workloads not ready/total; Warning events in the last hour; the five busiest nodes; the ten newest warnings. No rows cross the pipe.
3. **Metrics from metrics-server.** Polled from `metrics.k8s.io/v1beta1` every 15 s per context and namespace while an overview or a pods/nodes table needs it. Pod tables gain CPU, %CPU/R, %CPU/L, Mem, %MEM/R, %MEM/L (from container requests and limits; "—" when unset); node tables gain CPU, %CPU, Mem, %MEM of allocatable. Without metrics-server the columns are left out and the overview says "metrics-server not found".
4. **Thresholds:** orange from 80 %, red from 95 % (werkbank question 20). Failing counts red, degraded and not-ready orange, zero faint.
5. **Click through:** Return or double-click on a fleet row opens that cluster in a new one-context tab on Overview; a failing or degraded count opens its Pods with a health filter; not-ready opens Deployments with a health filter; warnings open the cluster's Overview. The list shows "Showing failing pods · Clear"; Esc clears it.
6. **Overview of one context** (frame 15): the same summary as four plates.

## Later, not in this decision

- History and sparklines; alerts; custom overview columns, per-namespace overviews, cost estimates; bulk actions from the overview.

## Consequences

- **Load:** per context in an open overview, one watch each on pods, nodes, three workload kinds and Warning events, plus a metrics poll every 15 s; watches linger 30 s after.
- **Exec plugins:** opening `all` connects every context in it.
