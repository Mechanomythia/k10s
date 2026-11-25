# ADR 0006: Dive into children

- **Status:** Accepted
- **Date:** 2026-11-25
- **Code:** `internal/engine/` (new children.go, tables.go, detail.go, protocol.go, testdata/protocol), `macos/K10s/Sources/K10sCore/` (Protocol.swift, new ChildKinds.swift), `macos/K10s/Sources/K10s/` (TabModel.swift, ListPane.swift, new ContainersPane.swift, MainWindow.swift, WindowKeys.swift, RowActions.swift, InspectorPane.swift, LogsView.swift)
- **Builds on:** ADR 0002 (protocol, table.subscribe), ADR 0003 (main window, keys), ADR 0004 (row actions)
- **Werkbank:** proposal #6, revision 2

## Context

In k9s you press Enter on a deployment and see its pods, Enter on a pod shows its containers, and Esc takes you back. k10s has no way to go from a parent to its children: you switch to Pods and type a label selector by hand.

What is there now:
- `table.subscribe` takes a `labelSelector` (`internal/engine/protocol.go` TableSubscribeParams, `tables.go` open), but no field selector and no owner filter. The list pane uses the label selector when the filter text contains `=` (`K10sCore/TableModel.swift`).
- The app never sees a parent's selector: rows carry only identity, cells and health (`Row`). The full object is in the engine (`includeObject=Object` in tables.go, `object.yaml` in detail.go).
- `pod.containers` returns container names only (`detail.go` podContainers); the logs view uses it for its container picker (`LogsView.swift`).
- Return and double-click open the inspector on Describe (`WindowKeys.swift` handle, `ListPane.swift` open), which `d` also does. Esc closes the inspector, then clears the filter.
- Everything that acts on a row reads `tab.kind`, `tab.selection` and `tab.scope.contexts` (`RowActions.swift`, `InspectorPane.swift`, `InspectorEdit.swift`, `WindowKeys.swift`).

No design phase: a child level is the existing list pane (ADR 0003, frame 1) with a breadcrumb line over it; the containers level is the same grid with fixed columns.

## Decision

1. **Which parents.** (question 18: all of them)
   | Parent | Children | Found by |
   | --- | --- | --- |
   | Deployment, StatefulSet, DaemonSet, ReplicaSet, Job | Pods | `spec.selector` (LabelSelector) |
   | Service | Pods | `spec.selector` (map) |
   | CronJob | Jobs | owner reference to the CronJob's uid |
   | Node | Pods, all namespaces | field selector `spec.nodeName=<node>` |
   | Pod | Containers | the pod's spec and status |
   Deployments go straight to pods, as k9s does, not through their ReplicaSets. So paths nest: cronjob → jobs → pods → containers, deployment → pods → containers.
2. **Engine: `object.children`.** New request, params `ObjectParams {context, resource, namespace, name}`, result `ChildrenResult {resource: ResourceRef, namespace, labelSelector, fieldSelector, ownerUID, none}` for the table parents (not Pod). The engine gets the object with the dynamic client, as `object.yaml` does. Label selectors go through `metav1.LabelSelectorAsSelector` (matchLabels and matchExpressions) or `labels.SelectorFromSet` for a service. `namespace` is the parent's, or empty (all namespaces) for a node. `none: true` when the selector is absent or empty (a service without one, such as ExternalName): the app shows "selects no pods" and never subscribes, since an empty selector would list every pod. A kind outside decision 1 answers `invalid_params`. In `internal/engine/children.go`, registered by Detail.
3. **Engine: `table.subscribe` gains `fieldSelector` and `ownerUID`.** Both become part of the shared watch's key. `fieldSelector` is passed to the API server like `labelSelector`. `ownerUID` is filtered in the engine: a row is kept when its object (already in the Table response) has an owner reference with that uid; a MODIFIED row that loses it is sent as a delete. Kubernetes has no server-side owner filter, so a cronjob's jobs are a namespace-wide job watch filtered locally; a namespace has tens of jobs, not thousands.
4. **Engine: `pod.containers` gains statuses.** The result keeps `containers` and `initContainers` and adds `statuses: [{name, init, image, ready, state, restarts}]`, from the pod's spec and `status.containerStatuses` / `initContainerStatuses`. `state` is the kubectl word: Running, Waiting: <reason>, Terminated: <reason>. Golden files for `object.children`, the new table.subscribe params and pod.containers, mirrored in `Protocol.swift`.
5. **App: a level stack in TabModel.** `TabModel` gets `levels: [Level]`. A Level holds the parent row and kind, what the children are (a table query from `object.children`, or the containers of a pod), and the list state it covers (filter, sort, selection) so going back restores it. Computed `activeKind`, `activeContexts`, `activeNamespace` return the top level or the tab's own values; children always use only the parent row's context. Row actions, the inspector and the keys read these, so describe, YAML, events, logs, edit, scale, restart, delete and copy name work on a child pod or job as on any row. Choosing a kind in the sidebar or the command bar, or changing namespace, clears the stack. `K10sCore/ChildKinds.swift` lists the parent kinds of decision 1, so the app knows where Return dives without asking the engine.
6. **The pane.** Diving pushes a level; the list area shows the new level sliding in from the right (`.transition(.move(edge: .trailing))`, 0.2 s) over the level below, which stays subscribed so going back is instant. Its header is a breadcrumb in today's type: `deployments / web / web-7d9f-x2k` in dust, then the child kind and the usual count line (`4 in shop · watching`), with the context in mono when the tab has several. The `object.children` request in flight shows the existing Loading state; "selects no pods" uses the existing empty view; an error (forbidden, object gone) is a notice as for a failing context. If a `=` filter is typed inside a pane, it is joined to the parent's label selector with a comma.
7. **The containers level.** `ContainersPane` is the list grid with fixed columns Name, Image, Ready, State, Restarts, init containers after the others with an `init` tag, health stars from state (Running live, Waiting building, Terminated with error failed, Completed stopped). It calls `pod.containers` when it opens and again whenever the pod's row changes in the level below, so it follows restarts without a new watch. Keys on a container: Return and `l` open the logs inspector with that container picked; `c` copies its name; the object keys (d, y, v, e, s, r, ⌃D) act on the pod, as k9s does.
8. **Keys.** Return and double-click dive on a row whose kind is in ChildKinds; on other kinds they open Describe as now. `d` stays Describe everywhere. (question 17) Esc, in order: leave a text field, close the inspector, clear the filter, then pop one level. The inspector closes when you dive or go back, since its row is no longer in the list.
9. **Tests.** Go: `object.children` for every parent of decision 1 on fake dynamic objects, matchExpressions, a service without a selector, an unsupported kind, a 403; table.subscribe with a field selector and with an owner filter, including a row that loses its owner; pod.containers statuses for running, waiting, terminated and init containers; the golden files. Swift: golden files decode; TabModel push/pop restores filter, sort and selection and clears on kind change; ChildKinds lookup; selector joining.

**Size:** 4 tickets, about 1,200 lines in all.

## Later, not in this decision

- Namespace to its pods (k9s switches namespace on Enter): when the namespace picker is not quick enough.
- Shell into a container from the containers level, with the shell of ADR 0005 using the picked container.
- Owner-reference filtering for label-selector children too, to hide pods that share labels but belong to something else: when it misleads someone in practice.
- A breadcrumb you can click to jump several levels at once: when Esc-Esc-Esc gets tedious.
- More parents (PVC to pods, Ingress to services, HPA to its target): when you reach for them.

## Consequences

- One new read request and two new filters on an existing one; no writes, no permissions beyond `get` on the parent and `list/watch` on the children, which the user already needs.
- Up to four live watches in a deep path (cronjob list, its jobs, a job's pods, plus the pods list under the containers level). Watches are shared and linger 30 s (ADR 0002), so going back and forth costs no extra lists.
- The owner filter runs in the engine over a namespace's jobs; cost grows with the number of jobs in the namespace.
- Return no longer opens Describe on rows that have children; `d` does. A change for anyone used to the current build.
- The protocol changes in three places: protocol.go, the golden files and Protocol.swift change together, as the playbook requires.
