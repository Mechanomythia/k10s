package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeCluster serves Table lists and watches of any collection from objs,
// /version and node (and pod) metrics; 500 for everything when fail.
type fakeCluster struct {
	srv       *httptest.Server
	fail      bool
	nodeUsage string // metrics items JSON; "" answers 404

	mu     sync.Mutex
	objs   map[string][]string    // collection path -> object JSONs
	events map[string]chan string // collection path -> watch event lines

	open atomic.Int32 // watches open now
	done chan struct{}
}

func newCluster(t *testing.T, objs map[string][]string, nodeUsage string) *fakeCluster {
	f := &fakeCluster{objs: objs, nodeUsage: nodeUsage, events: map[string]chan string{}, done: make(chan struct{})}
	f.srv = httptest.NewServer(f)
	t.Cleanup(func() { close(f.done); f.srv.Close() })
	return f
}

func (f *fakeCluster) eventsOf(path string) chan string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.events[path] == nil {
		f.events[path] = make(chan string)
	}
	return f.events[path]
}

func tableOf(objs ...string) string {
	var rows []string
	for _, o := range objs {
		rows = append(rows, `{"cells":["x"],"object":`+o+`}`)
	}
	return `{"kind":"Table","apiVersion":"meta.k8s.io/v1","metadata":{"resourceVersion":"7"},"columnDefinitions":[{"name":"Name","type":"string"}],"rows":[` + strings.Join(rows, ",") + `]}`
}

func (f *fakeCluster) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if f.fail {
		http.Error(w, "boom", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	p := r.URL.Path
	switch {
	case p == "/version":
		fmt.Fprint(w, `{"gitVersion":"v1.33.4"}`)
	case strings.HasPrefix(p, "/apis/metrics.k8s.io/"):
		switch {
		case f.nodeUsage == "":
			http.NotFound(w, r)
		case strings.HasSuffix(p, "/nodes"):
			fmt.Fprintf(w, `{"items":%s}`, f.nodeUsage)
		default:
			fmt.Fprint(w, `{"items":[]}`)
		}
	default:
		if p == "/api/v1/events" && r.URL.Query().Get("fieldSelector") != "type=Warning" {
			http.Error(w, "events without the Warning selector", http.StatusBadRequest)
			return
		}
		if r.Header.Get("Accept") != tableAccept {
			http.Error(w, "not a table request", http.StatusNotAcceptable)
			return
		}
		if r.URL.Query().Get("watch") != "" {
			f.open.Add(1)
			defer f.open.Add(-1)
			w.WriteHeader(http.StatusOK)
			w.(http.Flusher).Flush()
			ch := f.eventsOf(p)
			for {
				select {
				case <-r.Context().Done():
					return
				case <-f.done:
					return
				case line := <-ch:
					fmt.Fprintln(w, line)
					w.(http.Flusher).Flush()
				}
			}
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		fmt.Fprint(w, tableOf(f.objs[p]...))
	}
}

// push sends one watch event on a collection's open watch.
func (f *fakeCluster) push(t *testing.T, path, typ, obj string) {
	t.Helper()
	select {
	case f.eventsOf(path) <- fmt.Sprintf(`{"type":%q,"object":%s}`, typ, tableOf(obj)):
	case <-time.After(5 * time.Second):
		t.Fatalf("no open watch on %s", path)
	}
}

func kobj(apiVersion, kind, ns, name, rest string) string {
	nsField := ""
	if ns != "" {
		nsField = fmt.Sprintf(`"namespace":%q,`, ns)
	}
	return fmt.Sprintf(`{"apiVersion":%q,"kind":%q,"metadata":{"name":%q,%s"uid":"%s-%s-%s","resourceVersion":"7"},%s}`,
		apiVersion, kind, name, nsField, kind, ns, name, rest)
}

func knode(name string, ready bool, cpu, mem, pods string) string {
	st := "False"
	if ready {
		st = "True"
	}
	return kobj("v1", "Node", "", name, fmt.Sprintf(`"status":{"conditions":[{"type":"Ready","status":%q}],"allocatable":{"cpu":%q,"memory":%q,"pods":%q}}`, st, cpu, mem, pods))
}

func kpod(name, phase, statuses string) string {
	return kobj("v1", "Pod", "default", name, fmt.Sprintf(`"status":{"phase":%q,"containerStatuses":[%s]}`, phase, statuses))
}

func kwarning(name, reason, extra string) string {
	return kobj("v1", "Event", "default", name, fmt.Sprintf(`"type":"Warning","reason":%q,"message":"m-%s","involvedObject":{"kind":"Pod","name":"p-%s"}%s`, reason, name, name, extra))
}

const (
	readyC    = `{"ready":true}`
	notReadyC = `{"ready":false}`
	crashC    = `{"ready":false,"state":{"waiting":{"reason":"CrashLoopBackOff"}}}`
)

// prodObjs: 3 nodes (one not ready), 5 pods (3 running, 2 failing, 1
// degraded), 5 workloads (2 not ready) and 3 warnings (2 in the last hour).
func prodObjs(now time.Time) map[string][]string {
	ts := func(d time.Duration) string { return now.Add(-d).UTC().Format(time.RFC3339) }
	return map[string][]string{
		"/api/v1/nodes": {
			knode("n1", true, "4", "8Gi", "110"),
			knode("n2", false, "2", "4Gi", "110"),
			knode("n3", true, "2", "4Gi", "10"),
		},
		"/api/v1/pods": {
			kpod("p1", "Running", readyC),
			kpod("p2", "Running", notReadyC),
			kpod("p3", "Pending", ""),
			kpod("p4", "Failed", ""),
			kpod("p5", "Running", crashC),
		},
		"/apis/apps/v1/deployments": {
			kobj("apps/v1", "Deployment", "default", "d1", `"spec":{"replicas":2},"status":{"readyReplicas":2,"updatedReplicas":2}`),
			kobj("apps/v1", "Deployment", "default", "d2", `"spec":{"replicas":3},"status":{"readyReplicas":1,"updatedReplicas":3}`),
			kobj("apps/v1", "Deployment", "default", "d3", `"spec":{"replicas":0},"status":{}`),
		},
		"/apis/apps/v1/statefulsets": {
			kobj("apps/v1", "StatefulSet", "default", "s1", `"spec":{"replicas":1},"status":{"updatedReplicas":0}`),
		},
		"/apis/apps/v1/daemonsets": {
			kobj("apps/v1", "DaemonSet", "default", "ds1", `"status":{"desiredNumberScheduled":3,"numberReady":3,"updatedNumberScheduled":3}`),
		},
		"/api/v1/events": {
			kwarning("e1", "BackOff", fmt.Sprintf(`,"count":3,"lastTimestamp":%q`, ts(5*time.Minute))),
			kwarning("e2", "FailedScheduling", fmt.Sprintf(`,"eventTime":%q`, now.Add(-10*time.Minute).UTC().Format("2006-01-02T15:04:05.000000Z07:00"))),
			kwarning("e3", "Old", fmt.Sprintf(`,"count":7,"lastTimestamp":%q`, ts(2*time.Hour))),
		},
	}
}

const prodUsage = `[{"metadata":{"name":"n1"},"usage":{"cpu":"3","memory":"2Gi"}},
 {"metadata":{"name":"n2"},"usage":{"cpu":"1","memory":"3584Mi"}},
 {"metadata":{"name":"n3"},"usage":{"cpu":"100m","memory":"1Gi"}}]`

func smallObjs() map[string][]string {
	return map[string][]string{
		"/api/v1/nodes": {knode("m1", true, "1", "2Gi", "20")},
		"/api/v1/pods":  {kpod("q1", "Running", readyC)},
	}
}

const smallUsage = `[{"metadata":{"name":"m1"},"usage":{"cpu":"500m","memory":"1Gi"}}]`

func newOverview(c *Conns, throttle time.Duration) *Overview {
	o := NewOverview(metricsTables(c))
	o.Throttle = throttle
	return o
}

func startOverview(t *testing.T, o *Overview, contexts ...string) *testSub {
	t.Helper()
	raw, _ := json.Marshal(OverviewSubscribeParams{Contexts: contexts})
	ctx, cancel := context.WithCancel(context.Background())
	run, err := o.subscribe(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	return startRun(t, run, cancel)
}

// nextSummary returns the next summary of a context.
func nextSummary(t *testing.T, s *testSub, context string) Summary {
	t.Helper()
	for {
		if sum := s.next(t, EventSummary).(Summary); sum.Context == context {
			return sum
		}
	}
}

// firstSummaries returns the first summary of each context, in any order.
func firstSummaries(t *testing.T, s *testSub, contexts ...string) map[string]Summary {
	t.Helper()
	m := map[string]Summary{}
	for len(m) < len(contexts) {
		sum := s.next(t, EventSummary).(Summary)
		if _, ok := m[sum.Context]; !ok {
			m[sum.Context] = sum
		}
	}
	return m
}

// quiet fails if a summary arrives within d.
func quiet(t *testing.T, s *testSub, d time.Duration) {
	t.Helper()
	deadline := time.After(d)
	for {
		select {
		case g := <-s.ch:
			s.log = append(s.log, g)
			if g.event == EventSummary {
				t.Fatalf("unexpected summary %+v", g.data)
			}
		case <-deadline:
			return
		}
	}
}

func intp(i int) *int { return &i }

func TestOverviewCounts(t *testing.T) {
	now := time.Now()
	a := newCluster(t, prodObjs(now), prodUsage)
	b := newCluster(t, smallObjs(), smallUsage)
	o := newOverview(connsFor(t, map[string]string{"a": a.srv.URL, "b": b.srv.URL}), time.Hour)
	s := startOverview(t, o, "a", "b")
	first := firstSummaries(t, s, "a", "b")

	got := first["a"]
	f := got.SummaryFigures
	if f == nil || got.State != StateLive || got.Version != "v1.33.4" || !got.MetricsAvailable {
		t.Fatalf("summary %+v", got)
	}
	gi := int64(1 << 30)
	wantTop := []TopNode{
		{Name: "n2", CPUPercent: intp(50), MemPercent: intp(88)},
		{Name: "n1", Ready: true, CPUPercent: intp(75), MemPercent: intp(25)},
		{Name: "n3", Ready: true, CPUPercent: intp(5), MemPercent: intp(25)},
	}
	if !reflect.DeepEqual(f.TopNodes, wantTop) {
		t.Errorf("top nodes %s", mustJSON(f.TopNodes))
	}
	f.TopNodes = nil
	if len(f.RecentWarnings) != 2 || f.RecentWarnings[0].Reason != "BackOff" || f.RecentWarnings[0].Count != 3 ||
		f.RecentWarnings[0].Object != "Pod/p-e1" || f.RecentWarnings[0].Namespace != "default" ||
		f.RecentWarnings[1].Reason != "FailedScheduling" || f.RecentWarnings[1].Count != 1 {
		t.Errorf("warnings %s", mustJSON(f.RecentWarnings))
	}
	f.RecentWarnings = nil
	want := SummaryFigures{
		NodesReady: 2, NodesTotal: 3,
		CPUUsedMilli: 4100, CPUAllocatableMilli: 8000,
		MemUsedBytes: 6*gi + gi/2, MemAllocatableBytes: 16 * gi,
		PodsRunning: 3, PodsTotal: 5, PodCapacity: 230,
		PodsFailing: 2, PodsDegraded: 1,
		WorkloadsNotReady: 2, WorkloadsTotal: 5,
		WarningsLastHour: 2,
	}
	if !reflect.DeepEqual(*f, want) {
		t.Errorf("figures\n got %+v\nwant %+v", *f, want)
	}

	gb := first["b"]
	if gb.SummaryFigures == nil || gb.NodesTotal != 1 || gb.PodsRunning != 1 || gb.CPUUsedMilli != 500 ||
		!reflect.DeepEqual(gb.TopNodes, []TopNode{{Name: "m1", Ready: true, CPUPercent: intp(50), MemPercent: intp(50)}}) ||
		len(gb.RecentWarnings) != 0 || gb.RecentWarnings == nil {
		t.Errorf("summary b %s", mustJSON(gb))
	}
}

func mustJSON(v any) string { b, _ := json.Marshal(v); return string(b) }

func TestOverviewThrottleAndDedupe(t *testing.T) {
	a := newCluster(t, smallObjs(), smallUsage)
	const throttle = 400 * time.Millisecond
	o := newOverview(connsFor(t, map[string]string{"a": a.srv.URL}), throttle)
	s := startOverview(t, o, "a")
	if got := nextSummary(t, s, "a"); got.PodsTotal != 1 {
		t.Fatalf("first summary %+v", got)
	}
	first := time.Now()

	// Two changes within the window: one summary, after the window.
	a.push(t, "/api/v1/pods", "ADDED", kpod("q2", "Running", readyC))
	a.push(t, "/api/v1/pods", "ADDED", kpod("q3", "Pending", ""))
	got := nextSummary(t, s, "a")
	if got.PodsTotal != 3 || got.PodsRunning != 2 {
		t.Fatalf("summary %+v; want both changes in one", got)
	}
	if d := time.Since(first); d < throttle*3/4 {
		t.Errorf("second summary after %v, want the %v throttle", d, throttle)
	}

	// An event that changes nothing: no summary.
	a.push(t, "/api/v1/pods", "MODIFIED", kpod("q3", "Pending", ""))
	quiet(t, s, 3*throttle)

	a.push(t, "/api/v1/pods", "DELETED", kpod("q3", "Pending", ""))
	if got := nextSummary(t, s, "a"); got.PodsTotal != 2 {
		t.Fatalf("summary %+v", got)
	}
}

func TestOverviewFailingContext(t *testing.T) {
	a := newCluster(t, smallObjs(), smallUsage)
	b := newCluster(t, nil, "")
	b.fail = true
	o := newOverview(connsFor(t, map[string]string{"a": a.srv.URL, "b": b.srv.URL}), 50*time.Millisecond)
	s := startOverview(t, o, "a", "b")
	first := firstSummaries(t, s, "a", "b")

	gb := first["b"]
	if gb.State != StateUnreachable || gb.Error == "" || gb.SummaryFigures != nil || gb.Version != "" || gb.MetricsAvailable {
		t.Fatalf("failing summary %s", mustJSON(gb))
	}
	if ga := first["a"]; ga.State != StateLive || ga.PodsTotal != 1 {
		t.Fatalf("summary a %+v", ga)
	}
	a.push(t, "/api/v1/pods", "ADDED", kpod("q2", "Running", readyC))
	if ga := nextSummary(t, s, "a"); ga.PodsTotal != 2 {
		t.Fatalf("summary a %+v", ga)
	}
}

func TestOverviewWithoutMetrics(t *testing.T) {
	a := newCluster(t, prodObjs(time.Now()), "")
	o := newOverview(connsFor(t, map[string]string{"a": a.srv.URL}), time.Hour)
	s := startOverview(t, o, "a")
	got := nextSummary(t, s, "a")
	if got.MetricsAvailable || got.SummaryFigures == nil || got.CPUUsedMilli != 0 || got.MemUsedBytes != 0 ||
		got.CPUAllocatableMilli != 8000 || got.MemAllocatableBytes != 16<<30 {
		t.Fatalf("summary %s", mustJSON(got))
	}
	wantTop := []TopNode{{Name: "n1", Ready: true}, {Name: "n2"}, {Name: "n3", Ready: true}}
	if !reflect.DeepEqual(got.TopNodes, wantTop) {
		t.Errorf("top nodes %s", mustJSON(got.TopNodes))
	}
}

func TestOverviewTeardown(t *testing.T) {
	a := newCluster(t, smallObjs(), smallUsage)
	o := newOverview(connsFor(t, map[string]string{"a": a.srv.URL}), 50*time.Millisecond)
	tb := o.Tables
	s := startOverview(t, o, "a")
	nextSummary(t, s, "a")
	eventually(t, "six watches open", func() bool { return a.open.Load() == int32(numKinds) })
	tb.mu.Lock()
	n := len(tb.watches)
	tb.mu.Unlock()
	if n != int(numKinds) {
		t.Fatalf("%d shared watches, want %d", n, numKinds)
	}

	s.stop()
	eventually(t, "watches and pollers released", func() bool {
		tb.mu.Lock()
		w := len(tb.watches)
		tb.mu.Unlock()
		tb.Metrics.mu.Lock()
		p := len(tb.Metrics.pollers)
		tb.Metrics.mu.Unlock()
		return w == 0 && p == 0 && a.open.Load() == 0
	})
}

// eventually polls cond for up to 5 s.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); !cond(); {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
