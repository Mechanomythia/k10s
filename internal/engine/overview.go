package engine

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Overview serves overview.subscribe: one summary per context, aggregated
// in-process from the shared Table watches and the node metrics poller
// (ADR 0007, decision 2).
type Overview struct {
	Tables   *Tables
	Throttle time.Duration // least time between two summaries of a context
}

// NewOverview returns an Overview on t's watches with the ADR's 5 s throttle.
func NewOverview(t *Tables) *Overview { return &Overview{Tables: t, Throttle: 5 * time.Second} }

// Register wires overview.subscribe into s.
func (o *Overview) Register(s *Server) { s.Subs[MethodOverviewSubscribe] = o.subscribe }

type overviewKind int

const (
	kindPods overviewKind = iota
	kindNodes
	kindDeployments
	kindStatefulSets
	kindDaemonSets
	kindEvents
	numKinds
)

// overviewWatches are the shared watches behind a summary, by kind.
var overviewWatches = [numKinds]struct {
	res   ResourceRef
	field string
}{
	{ResourceRef{Version: "v1", Plural: "pods"}, ""},
	{ResourceRef{Version: "v1", Plural: "nodes"}, ""},
	{ResourceRef{Group: "apps", Version: "v1", Plural: "deployments"}, ""},
	{ResourceRef{Group: "apps", Version: "v1", Plural: "statefulsets"}, ""},
	{ResourceRef{Group: "apps", Version: "v1", Plural: "daemonsets"}, ""},
	{ResourceRef{Version: "v1", Plural: "events"}, "type=Warning"},
}

func (o *Overview) subscribe(ctx context.Context, params json.RawMessage) (func(Emit), error) {
	var p OverviewSubscribeParams
	if err := json.Unmarshal(params, &p); err != nil || len(p.Contexts) == 0 {
		return nil, &Error{Code: CodeInvalidParams, Message: "overview.subscribe needs contexts"}
	}
	return func(emit Emit) {
		t := o.Tables
		aggs := map[string]*ctxAgg{} // never written after this loop
		for _, name := range p.Contexts {
			aggs[name] = &ctxAgg{name: name, state: StateConnecting, kick: make(chan struct{}, 1)}
		}
		stopListen := t.conns.Listen(func(ch ContextStateChange) {
			if a := aggs[ch.Context]; a != nil {
				a.setState(ch)
			}
		})
		var wg sync.WaitGroup
		var cleanup []func()
		for name, a := range aggs {
			for k, spec := range overviewWatches {
				w := t.acquire(watchKey{context: name, resource: spec.res, field: spec.field})
				ks := &kindSub{a: a, kind: overviewKind(k)}
				w.join(ks)
				cleanup = append(cleanup, func() { w.leave(ks); t.release(w) })
			}
			a.mh = t.Metrics.AcquireNodes(name)
			stop := a.mh.OnChange(a.poke)
			cleanup = append(cleanup, func() { stop(); a.mh.Release() })
			wg.Add(2)
			go func() {
				defer wg.Done()
				a.mh.Wait(ctx)
				a.mu.Lock()
				a.metricsDone = true
				a.mu.Unlock()
				a.poke()
			}()
			go func() {
				defer wg.Done()
				a.loop(ctx, o.Throttle, emit)
			}()
		}
		<-ctx.Done()
		stopListen()
		wg.Wait() // no emits once run returns
		for _, f := range cleanup {
			f()
		}
	}, nil
}

// ctxAgg aggregates one context of an overview subscription.
type ctxAgg struct {
	name string
	mh   *MetricsHandle
	kick chan struct{} // something changed

	mu          sync.Mutex
	state       ContextState
	err         string
	version     string
	answered    [numKinds]bool // listed or refused
	rows        [numKinds]map[string]Row
	metricsDone bool // the first node metrics poll is done
}

func (a *ctxAgg) poke() {
	select {
	case a.kick <- struct{}{}:
	default:
	}
}

func (a *ctxAgg) setState(ch ContextStateChange) {
	a.mu.Lock()
	a.state, a.err = ch.State, ch.Error
	if ch.Version != "" {
		a.version = ch.Version
	}
	a.mu.Unlock()
	a.poke()
}

// loop emits the context's summary after changes: the first at once, then at
// most one per throttle, and only when its JSON differs from the last one.
// A minute tick lets old warnings age out.
func (a *ctxAgg) loop(ctx context.Context, throttle time.Duration, emit Emit) {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	var last []byte
	var lastAt time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-a.kick:
		case <-tick.C:
		}
		if wait := throttle - time.Since(lastAt); !lastAt.IsZero() && wait > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			}
			select { // this build covers the changes meanwhile
			case <-a.kick:
			default:
			}
		}
		s, ok := a.build(time.Now())
		if !ok {
			continue
		}
		raw, _ := json.Marshal(s)
		if bytes.Equal(raw, last) || ctx.Err() != nil {
			continue
		}
		emit(EventSummary, s)
		last, lastAt = raw, time.Now()
	}
}

// build returns the current summary, or false while it has no data yet.
func (a *ctxAgg) build(now time.Time) (Summary, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	s := Summary{Context: a.name, State: a.state, Error: a.err}
	if a.state == StateUnreachable || a.state == StateUnauthorized {
		return s, true
	}
	if !a.metricsDone || slices.Contains(a.answered[:], false) {
		return s, false
	}
	s.Version = a.version
	usage, avail := a.mh.Usage()
	s.MetricsAvailable = avail
	f := &SummaryFigures{TopNodes: []TopNode{}, RecentWarnings: []WarningEvent{}}
	s.SummaryFigures = f

	type busy struct {
		node  TopNode
		score int
	}
	var nodes []busy
	for _, r := range a.rows[kindNodes] {
		fc := r.facts
		if fc == nil {
			fc = &rowFacts{}
		}
		b := busy{node: TopNode{Name: r.Name, Ready: r.Health == HealthOK}, score: -1}
		f.NodesTotal++
		if b.node.Ready {
			f.NodesReady++
		}
		f.CPUAllocatableMilli += fc.cpu
		f.MemAllocatableBytes += fc.mem
		f.PodCapacity += fc.pods
		if u, ok := usage[r.Name]; avail && ok {
			f.CPUUsedMilli += u.CPU
			f.MemUsedBytes += u.Mem
			if fc.cpu > 0 {
				c := percent(u.CPU, fc.cpu)
				b.node.CPUPercent, b.score = &c, c
			}
			if fc.mem > 0 {
				m := percent(u.Mem, fc.mem)
				b.node.MemPercent, b.score = &m, max(b.score, m)
			}
		}
		nodes = append(nodes, b)
	}
	slices.SortFunc(nodes, func(x, y busy) int {
		return cmp.Or(cmp.Compare(y.score, x.score), strings.Compare(x.node.Name, y.node.Name))
	})
	for _, b := range nodes[:min(5, len(nodes))] {
		f.TopNodes = append(f.TopNodes, b.node)
	}

	for _, r := range a.rows[kindPods] {
		f.PodsTotal++
		if r.facts != nil && r.facts.running {
			f.PodsRunning++
		}
		switch r.Health {
		case HealthFailed:
			f.PodsFailing++
		case HealthDegraded:
			f.PodsDegraded++
		}
	}
	for _, k := range []overviewKind{kindDeployments, kindStatefulSets, kindDaemonSets} {
		for _, r := range a.rows[k] {
			f.WorkloadsTotal++
			if r.Health == HealthPending || r.Health == HealthDegraded {
				f.WorkloadsNotReady++
			}
		}
	}

	var warns []*rowFacts
	for _, r := range a.rows[kindEvents] {
		if fc := r.facts; fc != nil && fc.warn != nil && now.Sub(fc.seen) <= time.Hour {
			warns = append(warns, fc)
		}
	}
	f.WarningsLastHour = len(warns)
	slices.SortFunc(warns, func(x, y *rowFacts) int {
		return cmp.Or(y.seen.Compare(x.seen), strings.Compare(x.warn.Object, y.warn.Object), strings.Compare(x.warn.Reason, y.warn.Reason))
	})
	for _, w := range warns[:min(10, len(warns))] {
		f.RecentWarnings = append(f.RecentWarnings, *w.warn)
	}
	return s, true
}

func percent(use, of int64) int { return int((use*100 + of/2) / of) }

// kindSub joins one shared watch on behalf of a ctxAgg.
type kindSub struct {
	a    *ctxAgg
	kind overviewKind
}

func (k *kindSub) reset(_ string, _ []Column, rows map[string]Row) {
	m := make(map[string]Row, len(rows))
	for uid, r := range rows {
		m[uid] = r
	}
	k.a.mu.Lock()
	k.a.rows[k.kind], k.a.answered[k.kind] = m, true
	k.a.mu.Unlock()
	k.a.poke()
}

func (k *kindSub) relayout(name string, cols []Column, rows map[string]Row) {
	k.reset(name, cols, rows)
}

func (k *kindSub) upsert(_ string, _ []Column, r Row) {
	k.a.mu.Lock()
	if k.a.rows[k.kind] == nil {
		k.a.rows[k.kind] = map[string]Row{}
	}
	k.a.rows[k.kind][r.UID] = r
	k.a.mu.Unlock()
	k.a.poke()
}

func (k *kindSub) remove(r Row) {
	k.a.mu.Lock()
	delete(k.a.rows[k.kind], r.UID)
	k.a.mu.Unlock()
	k.a.poke()
}

// forbidden: a refused kind counts as answered, with nothing in it.
func (k *kindSub) forbidden(string, string) {
	k.a.mu.Lock()
	k.a.rows[k.kind], k.a.answered[k.kind] = nil, true
	k.a.mu.Unlock()
	k.a.poke()
}

// rowFacts are what a summary needs from an object beyond its Row.
type rowFacts struct {
	running        bool          // a pod in phase Running
	cpu, mem, pods int64         // a node's allocatable
	warn           *WarningEvent // a Warning event
	seen           time.Time     // when warn was last seen
}

// factsOf extracts the rowFacts of pods, nodes and Warning events.
func factsOf(res ResourceRef, obj map[string]any) *rowFacts {
	if res.Group != "" {
		return nil
	}
	str := func(m map[string]any, k string) string { s, _ := m[k].(string); return s }
	sub := func(m map[string]any, k string) map[string]any { s, _ := m[k].(map[string]any); return s }
	switch res.Plural {
	case "pods":
		return &rowFacts{running: str(sub(obj, "status"), "phase") == "Running"}
	case "nodes":
		c := nodeCapacity(obj)
		pods := parseQty(str(sub(sub(obj, "status"), "allocatable"), "pods")).Value()
		return &rowFacts{cpu: c.cpuReq, mem: c.memReq, pods: pods}
	case "events":
		if str(obj, "type") != "Warning" {
			return nil
		}
		meta := sub(obj, "metadata")
		var seen time.Time
		for _, s := range []string{str(obj, "lastTimestamp"), str(obj, "eventTime"), str(meta, "creationTimestamp")} {
			if t, err := time.Parse(time.RFC3339, s); err == nil {
				seen = t
				break
			}
		}
		count, _, _ := unstructured.NestedInt64(obj, "count")
		if count == 0 {
			count, _, _ = unstructured.NestedInt64(obj, "series", "count")
		}
		inv := sub(obj, "involvedObject")
		return &rowFacts{seen: seen, warn: &WarningEvent{
			LastSeen:  seen.UTC().Format(time.RFC3339),
			Reason:    str(obj, "reason"),
			Message:   str(obj, "message"),
			Object:    str(inv, "kind") + "/" + str(inv, "name"),
			Namespace: str(meta, "namespace"),
			Count:     max(int(count), 1),
		}}
	}
	return nil
}
