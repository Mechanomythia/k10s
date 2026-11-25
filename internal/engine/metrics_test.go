package engine

import (
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

// metricsAPI serves pod and node tables (one pod, one node) and
// metrics.k8s.io; 404 when !metrics.
type metricsAPI struct {
	srv       *httptest.Server
	podSpec   string // JSON of the pod's spec
	mu        sync.Mutex
	metrics   bool
	podUsage  string // JSON containers array
	nodeUsage string // JSON usage object
	hits      atomic.Int32
	done      chan struct{}
}

func newMetricsAPI(t *testing.T, podSpec string) *metricsAPI {
	f := &metricsAPI{podSpec: podSpec, metrics: true, done: make(chan struct{}),
		podUsage:  `[{"usage":{"cpu":"300m","memory":"200Mi"}},{"usage":{"cpu":"112m","memory":"188Mi"}}]`,
		nodeUsage: `{"cpu":"2","memory":"4Gi"}`}
	f.srv = httptest.NewServer(f)
	t.Cleanup(func() { close(f.done); f.srv.Close() })
	return f
}

func (f *metricsAPI) set(fn func()) { f.mu.Lock(); fn(); f.mu.Unlock() }

func (f *metricsAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	f.mu.Lock()
	defer f.mu.Unlock()
	p := r.URL.Path
	switch {
	case p == "/version":
		fmt.Fprint(w, `{"gitVersion":"v1.31.0"}`)
	case strings.HasPrefix(p, "/apis/metrics.k8s.io/"):
		f.hits.Add(1)
		if !f.metrics {
			http.NotFound(w, r)
		} else if strings.HasSuffix(p, "/nodes") {
			fmt.Fprintf(w, `{"items":[{"metadata":{"name":"n1"},"usage":%s}]}`, f.nodeUsage)
		} else {
			fmt.Fprintf(w, `{"items":[{"metadata":{"name":"web","namespace":"default"},"containers":%s}]}`, f.podUsage)
		}
	case p == "/api/v1/pods" || p == "/api/v1/nodes":
		kind, obj, cells := "Pod", fmt.Sprintf(`{"metadata":{"name":"web","namespace":"default","uid":"u1","resourceVersion":"7"},"spec":%s,"status":{"phase":"Running"}}`, f.podSpec), `"web","1/1"`
		cols := `{"name":"Name"},{"name":"Ready"}`
		if p == "/api/v1/nodes" {
			kind, cols, cells = "Node", `{"name":"Name"}`, `"n1"`
			obj = `{"metadata":{"name":"n1","uid":"u2","resourceVersion":"7"},"status":{"allocatable":{"cpu":"4","memory":"8Gi"}}}`
		}
		_ = kind
		if r.URL.Query().Get("watch") != "" {
			w.WriteHeader(http.StatusOK)
			w.(http.Flusher).Flush()
			f.mu.Unlock()
			<-r.Context().Done()
			f.mu.Lock()
			return
		}
		fmt.Fprintf(w, `{"kind":"Table","apiVersion":"meta.k8s.io/v1","metadata":{"resourceVersion":"7"},"columnDefinitions":[%s],"rows":[{"cells":[%s],"object":%s}]}`, cols, cells, obj)
	default:
		http.NotFound(w, r)
	}
}

func metricsTables(c *Conns) *Tables {
	tb := newTables(c)
	tb.Metrics.Interval = 30 * time.Millisecond
	tb.Metrics.Recheck = time.Hour
	return tb
}

func names(cols []Column) []string {
	var n []string
	for _, c := range cols {
		n = append(n, c.Name)
	}
	return n
}

const twoContainers = `{"containers":[
 {"resources":{"requests":{"cpu":"200m","memory":"100Mi"},"limits":{"cpu":"400m","memory":"1Gi"}}},
 {"resources":{"requests":{"cpu":"200m","memory":"100Mi"},"limits":{"cpu":"400m","memory":"1Gi"}}}]}`

func TestPodMetricsColumnsAndMath(t *testing.T) {
	f := newMetricsAPI(t, twoContainers)
	tb := metricsTables(connsFor(t, map[string]string{"a": f.srv.URL}))
	s := startSub(t, tb, TableSubscribeParams{Contexts: []string{"a"}, Resource: pods, AllNamespaces: true})
	snap := s.next(t, EventSnapshot).(Snapshot)
	want := []string{"Name", "Ready", "CPU", "%CPU/R", "%CPU/L", "Mem", "%MEM/R", "%MEM/L"}
	if !reflect.DeepEqual(names(snap.Columns), want) {
		t.Fatalf("columns %v", names(snap.Columns))
	}
	// usage 412m / 388Mi; requests 400m / 200Mi; limits 800m / 2Gi
	if got := snap.Rows[0].Cells[2:]; !reflect.DeepEqual(got, []string{"412m", "103 %", "52 %", "388Mi", "194 %", "19 %"}) {
		t.Fatalf("cells %v", got)
	}
}

func TestPodMetricsWithoutRequestsOrPartial(t *testing.T) {
	// One container has no limits and no memory request: those percentages are dashes.
	f := newMetricsAPI(t, `{"containers":[
 {"resources":{"requests":{"cpu":"500m"},"limits":{"cpu":"1"}}},
 {"resources":{"requests":{"cpu":"500m"}}}]}`)
	tb := metricsTables(connsFor(t, map[string]string{"a": f.srv.URL}))
	s := startSub(t, tb, TableSubscribeParams{Contexts: []string{"a"}, Resource: pods, AllNamespaces: true})
	snap := s.next(t, EventSnapshot).(Snapshot)
	if got := snap.Rows[0].Cells[2:]; !reflect.DeepEqual(got, []string{"412m", "41 %", "—", "388Mi", "—", "—"}) {
		t.Fatalf("cells %v", got)
	}

	// No resources at all.
	g := newMetricsAPI(t, `{"containers":[{"name":"c"}]}`)
	tb = metricsTables(connsFor(t, map[string]string{"b": g.srv.URL}))
	s = startSub(t, tb, TableSubscribeParams{Contexts: []string{"b"}, Resource: pods, AllNamespaces: true})
	snap = s.next(t, EventSnapshot).(Snapshot)
	if got := snap.Rows[0].Cells[2:]; !reflect.DeepEqual(got, []string{"412m", "—", "—", "388Mi", "—", "—"}) {
		t.Fatalf("cells %v", got)
	}
}

func TestNodeMetrics(t *testing.T) {
	f := newMetricsAPI(t, "{}")
	tb := metricsTables(connsFor(t, map[string]string{"a": f.srv.URL}))
	s := startSub(t, tb, TableSubscribeParams{Contexts: []string{"a"}, Resource: ResourceRef{Version: "v1", Plural: "nodes"}})
	snap := s.next(t, EventSnapshot).(Snapshot)
	if !reflect.DeepEqual(names(snap.Columns), []string{"Name", "CPU", "%CPU", "Mem", "%MEM"}) {
		t.Fatalf("columns %v", names(snap.Columns))
	}
	if got := snap.Rows[0].Cells[1:]; !reflect.DeepEqual(got, []string{"2000m", "50 %", "4Gi", "50 %"}) {
		t.Fatalf("cells %v", got)
	}
	u, ok := tb.Metrics.NodeUsage("a")
	if !ok || u["n1"] != (Usage{CPU: 2000, Mem: 4 << 30}) {
		t.Fatalf("NodeUsage %v %v", u, ok)
	}
}

func TestMetricsMissingIsNotAnError(t *testing.T) {
	f := newMetricsAPI(t, twoContainers)
	f.set(func() { f.metrics = false })
	tb := metricsTables(connsFor(t, map[string]string{"a": f.srv.URL}))
	s := startSub(t, tb, TableSubscribeParams{Contexts: []string{"a"}, Resource: pods, AllNamespaces: true})
	snap := s.next(t, EventSnapshot).(Snapshot)
	if !reflect.DeepEqual(names(snap.Columns), []string{"Name", "Ready"}) || len(snap.Rows[0].Cells) != 2 {
		t.Fatalf("snapshot %+v", snap)
	}
	time.Sleep(150 * time.Millisecond) // several intervals: a 404 is remembered, not retried
	if n := f.hits.Load(); n != 1 {
		t.Fatalf("%d metrics requests while unavailable, want 1", n)
	}
	for _, g := range s.log {
		if g.event == EventForbidden {
			t.Fatalf("unexpected %v", g)
		}
	}
}

func TestMetricsAppearLater(t *testing.T) {
	f := newMetricsAPI(t, twoContainers)
	f.set(func() { f.metrics = false })
	tb := metricsTables(connsFor(t, map[string]string{"a": f.srv.URL}))
	tb.Metrics.Recheck = 40 * time.Millisecond
	s := startSub(t, tb, TableSubscribeParams{Contexts: []string{"a"}, Resource: pods, AllNamespaces: true})
	if snap := s.next(t, EventSnapshot).(Snapshot); len(snap.Columns) != 2 {
		t.Fatalf("columns %v", names(snap.Columns))
	}
	f.set(func() { f.metrics = true })
	snap := s.next(t, EventSnapshot).(Snapshot) // a fresh one, with the columns
	if len(snap.Columns) != 8 || snap.Rows[0].Cells[2] != "412m" {
		t.Fatalf("snapshot %+v", snap)
	}
}

func TestMetricsUsageChangeUpserts(t *testing.T) {
	f := newMetricsAPI(t, twoContainers)
	tb := metricsTables(connsFor(t, map[string]string{"a": f.srv.URL}))
	s := startSub(t, tb, TableSubscribeParams{Contexts: []string{"a"}, Resource: pods, AllNamespaces: true})
	s.next(t, EventSnapshot)
	f.set(func() { f.podUsage = `[{"usage":{"cpu":"800m","memory":"1536Mi"}}]` })
	up := s.next(t, EventUpsert).(Upsert)
	if got := up.Row.Cells[2:]; !reflect.DeepEqual(got, []string{"800m", "200 %", "100 %", "1.5Gi", "768 %", "75 %"}) {
		t.Fatalf("cells %v", got)
	}
}

func TestMetricsPollerLifecycle(t *testing.T) {
	f := newMetricsAPI(t, twoContainers)
	tb := metricsTables(connsFor(t, map[string]string{"a": f.srv.URL}))
	s := startSub(t, tb, TableSubscribeParams{Contexts: []string{"a"}, Resource: pods, AllNamespaces: true})
	s.next(t, EventSnapshot)
	time.Sleep(100 * time.Millisecond)
	if f.hits.Load() < 2 {
		t.Fatalf("poller did not poll: %d", f.hits.Load())
	}
	s.stop()
	time.Sleep(200 * time.Millisecond) // linger is 50 ms
	n := f.hits.Load()
	time.Sleep(150 * time.Millisecond)
	if f.hits.Load() != n {
		t.Fatalf("still polling after the last unsubscribe: %d -> %d", n, f.hits.Load())
	}
	tb.Metrics.mu.Lock()
	left := len(tb.Metrics.pollers)
	tb.Metrics.mu.Unlock()
	if left != 0 {
		t.Fatalf("%d pollers left", left)
	}
}
