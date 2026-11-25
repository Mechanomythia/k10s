// Package engine implements the k10s engine protocol: newline-delimited JSON
// over stdio between the app and the engine (ADR 0002, decision 1).
//
// This file is the single definition of the wire format; the Swift app mirrors
// it in Protocol.swift and decodes the golden files in testdata/protocol.
// A line with an "id" is a request or a response, a line with a "sub" is a
// subscription event.
package engine

import "encoding/json"

// Request methods.
const (
	MethodContextsList    = "contexts.list"
	MethodContextsWatch   = "contexts.watch"
	MethodResourcesList   = "resources.list"
	MethodTableSubscribe  = "table.subscribe"
	MethodObjectYAML      = "object.yaml"
	MethodObjectDescribe  = "object.describe"
	MethodObjectEvents    = "object.events"
	MethodObjectChildren  = "object.children"
	MethodPodContainers   = "pod.containers"
	MethodLogsSubscribe   = "logs.subscribe"
	MethodObjectDelete    = "object.delete"
	MethodWorkloadScale   = "workload.scale"
	MethodWorkloadRestart = "workload.restart"
	MethodObjectReplace   = "object.replace"
	MethodUnsubscribe     = "unsubscribe"
)

// Event names, carried in Event.Event.
const (
	EventContextsChanged = "contexts.changed" // contexts.watch: data is ContextsChanged
	EventSnapshot        = "snapshot"         // table.subscribe: data is Snapshot
	EventUpsert          = "upsert"           // table.subscribe: data is Upsert
	EventDelete          = "delete"           // table.subscribe: data is Delete
	EventContextState    = "context.state"    // table.subscribe: data is ContextStateChange
	EventForbidden       = "forbidden"        // table.subscribe: data is Forbidden
	EventLines           = "lines"            // logs.subscribe: data is Lines
	EventEnd             = "end"              // logs.subscribe: data is End
)

// Error codes.
const (
	CodeNotImplemented = "not_implemented"
	CodeUnknownMethod  = "unknown_method"
	CodeInvalidParams  = "invalid_params"
	CodeInternal       = "internal"
	CodeForbidden      = "forbidden" // the API server answered 403
	CodeConflict       = "conflict"  // the API server answered 409
)

// Request is a call from the app. It gets exactly one Response with the same ID.
type Request struct {
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// Response answers one Request: Result on success, Error on failure, never both.
type Response struct {
	ID     string          `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *Error          `json:"error,omitempty"`
}

// Error is a failed request: a stable machine code and a human message.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

// Event is pushed by the engine for a subscription until it is unsubscribed.
type Event struct {
	Sub   string          `json:"sub"`
	Event string          `json:"event"`
	Data  json.RawMessage `json:"data"`
}

// Empty is the result of requests that return nothing: {}.
type Empty struct{}

// SubscribeResult is the result of every subscribing request; its Sub tags the
// subscription's events and is passed to unsubscribe.
type SubscribeResult struct {
	Sub string `json:"sub"`
}

// UnsubscribeParams are the params of unsubscribe (result: Empty). Unsubscribing
// an unknown or finished subscription is not an error.
type UnsubscribeParams struct {
	Sub string `json:"sub"`
}

// ContextInfo describes one kubeconfig context.
type ContextInfo struct {
	Name      string `json:"name"`
	Cluster   string `json:"cluster"`
	User      string `json:"user"`
	Namespace string `json:"namespace"`
	Current   bool   `json:"current"`
}

// ContextsListResult is the result of contexts.list (no params).
type ContextsListResult struct {
	Contexts []ContextInfo `json:"contexts"`
}

// ContextsChanged is the data of a contexts.changed event, sent after
// contexts.watch (no params, result SubscribeResult) when kubeconfig changes.
type ContextsChanged struct {
	Contexts []ContextInfo `json:"contexts"`
}

// ResourcesListParams are the params of resources.list.
type ResourcesListParams struct {
	Context string `json:"context"`
}

// ResourceKind is one listable kind from discovery.
type ResourceKind struct {
	Group      string   `json:"group,omitempty"` // empty for the core group
	Version    string   `json:"version"`
	Kind       string   `json:"kind"`
	Plural     string   `json:"plural"`
	ShortNames []string `json:"shortNames,omitempty"`
	Namespaced bool     `json:"namespaced"`
}

// ResourcesListResult is the result of resources.list.
type ResourcesListResult struct {
	Resources []ResourceKind `json:"resources"`
}

// ResourceRef names a kind by group, version and plural.
type ResourceRef struct {
	Group   string `json:"group,omitempty"` // empty for the core group
	Version string `json:"version"`
	Plural  string `json:"plural"`
}

// TableSubscribeParams are the params of table.subscribe (result SubscribeResult).
// The namespace is Namespace, or every namespace when AllNamespaces is true.
type TableSubscribeParams struct {
	Contexts      []string    `json:"contexts"`
	Resource      ResourceRef `json:"resource"`
	Namespace     string      `json:"namespace,omitempty"`
	AllNamespaces bool        `json:"allNamespaces,omitempty"`
	LabelSelector string      `json:"labelSelector,omitempty"`
	// FieldSelector goes to the API server like LabelSelector. OwnerUID keeps
	// the rows whose object has an owner reference with that uid.
	FieldSelector string `json:"fieldSelector,omitempty"`
	OwnerUID      string `json:"ownerUID,omitempty"`
}

// Health is the state behind a row's star glyph.
type Health string

// Health values.
const (
	HealthOK       Health = "ok"
	HealthPending  Health = "pending"
	HealthDegraded Health = "degraded"
	HealthFailed   Health = "failed"
	HealthStopped  Health = "stopped"
	HealthNone     Health = "none"
)

// Row is one object in a table: its identity, the server-printed cells in
// column order, and its health.
type Row struct {
	Context   string   `json:"context"`
	Namespace string   `json:"namespace,omitempty"` // empty for cluster-scoped kinds
	Name      string   `json:"name"`
	UID       string   `json:"uid"`
	Cells     []string `json:"cells"`
	Health    Health   `json:"health"`
}

// Column is one table column as printed by the API server.
type Column struct {
	Name     string `json:"name"`
	Priority int    `json:"priority,omitempty"` // above 0: shown only in the wide view, as kubectl does
}

// Snapshot is the data of a snapshot event: the full table, sent first and
// again whenever the columns change.
type Snapshot struct {
	Columns []Column `json:"columns"`
	Rows    []Row    `json:"rows"`
}

// Upsert is the data of an upsert event: a row added or changed.
type Upsert struct {
	Row Row `json:"row"`
}

// Delete is the data of a delete event: the identity of the removed row.
type Delete struct {
	Context   string `json:"context"`
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name"`
	UID       string `json:"uid"`
}

// ContextState is the connection state of a context.
type ContextState string

// ContextState values.
const (
	StateConnecting   ContextState = "connecting"
	StateLive         ContextState = "live"
	StateUnreachable  ContextState = "unreachable"
	StateUnauthorized ContextState = "unauthorized"
)

// ContextStateChange is the data of a context.state event.
type ContextStateChange struct {
	Context string       `json:"context"`
	State   ContextState `json:"state"`
	Error   string       `json:"error,omitempty"`   // set for unreachable and unauthorized
	Version string       `json:"version,omitempty"` // set for live: the cluster's gitVersion from /version
}

// PodContainersParams are the params of pod.containers.
type PodContainersParams struct {
	Context   string `json:"context"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

// PodContainersResult is the result of pod.containers: the container names
// from the pod spec, in spec order, and each container's status (regular
// containers first, then init containers); all lists are always present.
type PodContainersResult struct {
	Containers     []string          `json:"containers"`
	InitContainers []string          `json:"initContainers"`
	Statuses       []ContainerStatus `json:"statuses"`
}

// ContainerStatus is one container's image, readiness, state and restarts.
// State is the kubectl word: Running, Waiting: <reason>, Terminated: <reason>.
type ContainerStatus struct {
	Name     string `json:"name"`
	Init     bool   `json:"init,omitempty"`
	Image    string `json:"image"`
	Ready    bool   `json:"ready"`
	State    string `json:"state"`
	Restarts int    `json:"restarts"`
}

// ObjectParams are the params of object.yaml, object.describe and object.children.
type ObjectParams struct {
	Context   string      `json:"context"`
	Resource  ResourceRef `json:"resource"`
	Namespace string      `json:"namespace,omitempty"`
	Name      string      `json:"name"`
}

// TextResult is the result of object.yaml and object.describe.
type TextResult struct {
	Text string `json:"text"`
}

// ChildrenResult is the result of object.children: the table.subscribe filters
// that list a parent's children. Namespace is the parent's, or empty for every
// namespace. None is true when the parent selects nothing (no selector), so the
// app must not subscribe: an empty selector would list everything.
type ChildrenResult struct {
	Resource      ResourceRef `json:"resource"`
	Namespace     string      `json:"namespace,omitempty"`
	LabelSelector string      `json:"labelSelector,omitempty"`
	FieldSelector string      `json:"fieldSelector,omitempty"`
	OwnerUID      string      `json:"ownerUID,omitempty"`
	None          bool        `json:"none,omitempty"`
}

// ObjectEventsParams are the params of object.events.
type ObjectEventsParams struct {
	Context   string `json:"context"`
	Namespace string `json:"namespace,omitempty"`
	UID       string `json:"uid"` // uid of the involved object
}

// ObjectEvent is one Kubernetes event.
type ObjectEvent struct {
	Type     string `json:"type"` // Normal or Warning
	Reason   string `json:"reason"`
	Message  string `json:"message"`
	Count    int    `json:"count"`
	LastSeen string `json:"lastSeen"` // RFC 3339
}

// ObjectEventsResult is the result of object.events, newest first.
type ObjectEventsResult struct {
	Events []ObjectEvent `json:"events"`
}

// LogsSubscribeParams are the params of logs.subscribe (result SubscribeResult).
// The app sends every field explicitly: TailLines 500 and Follow true are its
// defaults; TailLines 0 means all lines. Container is required for a pod with
// several containers unless AllContainers is true, which prefixes each line
// with the container name.
type LogsSubscribeParams struct {
	Context       string `json:"context"`
	Namespace     string `json:"namespace"`
	Pod           string `json:"pod"`
	Container     string `json:"container,omitempty"`
	AllContainers bool   `json:"allContainers,omitempty"`
	Previous      bool   `json:"previous,omitempty"`
	TailLines     int    `json:"tailLines"`
	Follow        bool   `json:"follow"`
	Timestamps    bool   `json:"timestamps,omitempty"`
}

// Lines is the data of a lines event: a batch of log lines, at most one per 100 ms.
type Lines struct {
	Lines []string `json:"lines"`
}

// End is the data of an end event: the log stream finished. Error is set when
// it ended because of a failure.
type End struct {
	Error string `json:"error,omitempty"`
}

// Forbidden is the data of a forbidden event: the API server refused (403) to
// list or watch the kind for this subscription's context. The context stays
// live; the other contexts of the subscription are unaffected.
type Forbidden struct {
	Context string `json:"context"`
	Message string `json:"message"`
}

// ScaleParams are the params of workload.scale (result Empty): Deployment,
// StatefulSet or ReplicaSet.
type ScaleParams struct {
	Context   string      `json:"context"`
	Resource  ResourceRef `json:"resource"`
	Namespace string      `json:"namespace,omitempty"`
	Name      string      `json:"name"`
	Replicas  int         `json:"replicas"`
}

// ReplaceParams are the params of object.replace. The YAML's resourceVersion
// is sent as is: the server answers conflict when the object has changed.
type ReplaceParams struct {
	Context   string      `json:"context"`
	Resource  ResourceRef `json:"resource"`
	Namespace string      `json:"namespace,omitempty"`
	Name      string      `json:"name"`
	YAML      string      `json:"yaml"`
}

// ReplaceResult is the result of object.replace.
type ReplaceResult struct {
	ResourceVersion string `json:"resourceVersion"`
}
