package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/client-go/rest"
)

// Usage is one pod's or node's usage from metrics-server: CPU in
// millicores, memory in bytes.
type Usage struct{ CPU, Mem int64 }

// Metrics polls metrics.k8s.io/v1beta1 per context, on pollers shared by
// everything that needs them and stopped when the last user leaves (ADR 0006,
// decision 3).
type Metrics struct {
	conns    *Conns
	Interval time.Duration // between polls
	Recheck  time.Duration // how long a context without metrics-server is left alone

	mu      sync.Mutex
	pollers map[metricsKey]*poller
	missing map[string]time.Time // contexts that answered 404, by when
}

// NewMetrics returns Metrics polling every 15 s and re-checking a missing
// metrics-server every 5 min.
func NewMetrics(conns *Conns) *Metrics {
	return &Metrics{conns: conns, Interval: 15 * time.Second, Recheck: 5 * time.Minute,
		pollers: map[metricsKey]*poller{}, missing: map[string]time.Time{}}
}

type metricsKey struct {
	context   string
	nodes     bool
	namespace string // "" for all namespaces (and for nodes)
}

// AcquirePods starts, or joins, the poller of one context's pod metrics in a
// namespace ("" for all) and returns its handle. Release it when done.
func (m *Metrics) AcquirePods(context, namespace string) *MetricsHandle {
	return m.acquire(metricsKey{context: context, namespace: namespace})
}

// AcquireNodes is Acquire for the context's node metrics.
func (m *Metrics) AcquireNodes(context string) *MetricsHandle {
	return m.acquire(metricsKey{context: context, nodes: true})
}

// NodeUsage returns the latest node usage of a context by node name, and
// whether metrics are available. It has data only while someone holds the
// context's node poller (AcquireNodes) and its first poll is done.
func (m *Metrics) NodeUsage(context string) (map[string]Usage, bool) {
	m.mu.Lock()
	p := m.pollers[metricsKey{context: context, nodes: true}]
	m.mu.Unlock()
	if p == nil {
		return nil, false
	}
	return p.snapshot()
}

func (m *Metrics) acquire(k metricsKey) *MetricsHandle {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.pollers[k]
	if p == nil {
		ctx, cancel := context.WithCancel(context.Background())
		p = &poller{m: m, key: k, cancel: cancel, first: make(chan struct{}), subs: map[int]func(){}}
		m.pollers[k] = p
		go p.run(ctx)
	}
	p.refs++
	return &MetricsHandle{m: m, p: p}
}

// MetricsHandle is one user of a poller.
type MetricsHandle struct {
	m    *Metrics
	p    *poller
	once sync.Once
}

// Release drops the handle; the poller stops with its last one.
func (h *MetricsHandle) Release() {
	h.once.Do(func() {
		h.m.mu.Lock()
		defer h.m.mu.Unlock()
		if h.p.refs--; h.p.refs == 0 {
			delete(h.m.pollers, h.p.key)
			h.p.cancel()
		}
	})
}

// Wait blocks until the first poll is done or ctx ends.
func (h *MetricsHandle) Wait(ctx context.Context) {
	select {
	case <-h.p.first:
	case <-ctx.Done():
	}
}

// Usage returns the latest usage by "namespace/name" (pods) or name (nodes)
// and whether metrics are available.
func (h *MetricsHandle) Usage() (map[string]Usage, bool) { return h.p.snapshot() }

// OnChange calls fn after each poll that changed the data or availability,
// until the returned stop is called. fn must not block.
func (h *MetricsHandle) OnChange(fn func()) (stop func()) {
	h.p.mu.Lock()
	defer h.p.mu.Unlock()
	id := h.p.nextSub
	h.p.nextSub++
	h.p.subs[id] = fn
	return func() {
		h.p.mu.Lock()
		delete(h.p.subs, id)
		h.p.mu.Unlock()
	}
}

type poller struct {
	m      *Metrics
	key    metricsKey
	cancel context.CancelFunc
	refs   int // guarded by Metrics.mu
	first  chan struct{}

	mu      sync.Mutex
	avail   bool
	data    map[string]Usage
	subs    map[int]func()
	nextSub int
}

func (p *poller) snapshot() (map[string]Usage, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.data, p.avail
}

func (p *poller) run(ctx context.Context) {
	var hc *http.Client
	var base string
	for first := true; ; first = false {
		wait := p.m.Interval
		if cn, err := p.m.conns.Get(ctx, p.key.context); err == nil {
			if hc == nil {
				if hc, err = rest.HTTPClientFor(cn.Config); err == nil {
					base = strings.TrimSuffix(cn.Config.Host, "/")
				}
			}
			if hc != nil {
				if !p.poll(ctx, hc, base) {
					wait = p.m.Recheck
				}
			}
		}
		if first {
			close(p.first)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

// poll fetches once. It reports false when the context has no metrics-server.
func (p *poller) poll(ctx context.Context, hc *http.Client, base string) bool {
	m := p.m
	m.mu.Lock()
	at, missing := m.missing[p.key.context]
	if missing && time.Since(at) >= m.Recheck {
		missing = false
	}
	m.mu.Unlock()
	if missing {
		p.set(nil, false)
		return false
	}
	path := "/apis/metrics.k8s.io/v1beta1/nodes"
	if !p.key.nodes {
		path = "/apis/metrics.k8s.io/v1beta1"
		if p.key.namespace != "" {
			path += "/namespaces/" + url.PathEscape(p.key.namespace)
		}
		path += "/pods"
	}
	rctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(rctx, http.MethodGet, base+path, nil)
	if err != nil {
		return true
	}
	resp, err := hc.Do(req)
	if err != nil {
		return true // keep what we have, try again next tick
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		m.mu.Lock()
		m.missing[p.key.context] = time.Now()
		m.mu.Unlock()
		p.set(nil, false)
		return false
	case resp.StatusCode != http.StatusOK:
		return true
	}
	var list struct {
		Items []struct {
			Metadata struct{ Name, Namespace string }
			Usage    map[string]string
			// Containers is set on pod metrics.
			Containers []struct{ Usage map[string]string }
		}
	}
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return true
	}
	m.mu.Lock()
	delete(m.missing, p.key.context)
	m.mu.Unlock()
	data := make(map[string]Usage, len(list.Items))
	for _, it := range list.Items {
		var u Usage
		add := func(use map[string]string) {
			u.CPU += parseQty(use["cpu"]).MilliValue()
			u.Mem += parseQty(use["memory"]).Value()
		}
		add(it.Usage)
		for _, c := range it.Containers {
			add(c.Usage)
		}
		k := it.Metadata.Name
		if !p.key.nodes {
			k = it.Metadata.Namespace + "/" + k
		}
		data[k] = u
	}
	p.set(data, true)
	return true
}

// set stores a poll result and tells the subscribers if it changed anything.
func (p *poller) set(data map[string]Usage, avail bool) {
	p.mu.Lock()
	changed := avail != p.avail || len(data) != len(p.data)
	for k, u := range data {
		if p.data[k] != u {
			changed = true
		}
	}
	p.avail, p.data = avail, data
	var fns []func()
	if changed {
		for _, fn := range p.subs {
			fns = append(fns, fn)
		}
	}
	p.mu.Unlock()
	for _, fn := range fns {
		fn()
	}
}

func parseQty(s string) *resource.Quantity {
	q, _ := resource.ParseQuantity(s)
	return &q
}

// Table columns that metrics add.
var (
	podMetricCols  = []string{"CPU", "%CPU/R", "%CPU/L", "Mem", "%MEM/R", "%MEM/L"}
	nodeMetricCols = []string{"CPU", "%CPU", "Mem", "%MEM"}
)

// capacity is what a row's percentages are of: requests and limits of a pod
// (sums over containers, zero and not ok if any container lacks one) or the
// allocatable of a node (in the request fields).
type capacity struct {
	cpuReq, cpuLim, memReq, memLim         int64
	okCPUReq, okCPULim, okMemReq, okMemLim bool
}

// podCapacity reads requests and limits from a pod object.
func podCapacity(obj map[string]any) capacity {
	c := capacity{okCPUReq: true, okCPULim: true, okMemReq: true, okMemLim: true}
	spec, _ := obj["spec"].(map[string]any)
	cs, _ := spec["containers"].([]any)
	if len(cs) == 0 {
		return capacity{}
	}
	for _, x := range cs {
		res, _ := x.(map[string]any)["resources"].(map[string]any)
		get := func(kind, name string) (resource.Quantity, bool) {
			m, _ := res[kind].(map[string]any)
			s, ok := m[name].(string)
			if !ok {
				return resource.Quantity{}, false
			}
			q, err := resource.ParseQuantity(s)
			return q, err == nil && !q.IsZero()
		}
		if q, ok := get("requests", "cpu"); ok {
			c.cpuReq += q.MilliValue()
		} else {
			c.okCPUReq = false
		}
		if q, ok := get("limits", "cpu"); ok {
			c.cpuLim += q.MilliValue()
		} else {
			c.okCPULim = false
		}
		if q, ok := get("requests", "memory"); ok {
			c.memReq += q.Value()
		} else {
			c.okMemReq = false
		}
		if q, ok := get("limits", "memory"); ok {
			c.memLim += q.Value()
		} else {
			c.okMemLim = false
		}
	}
	return c
}

// nodeCapacity reads status.allocatable from a node object.
func nodeCapacity(obj map[string]any) capacity {
	st, _ := obj["status"].(map[string]any)
	al, _ := st["allocatable"].(map[string]any)
	cpu, _ := al["cpu"].(string)
	mem, _ := al["memory"].(string)
	c := capacity{cpuReq: parseQty(cpu).MilliValue(), memReq: parseQty(mem).Value()}
	c.okCPUReq, c.okMemReq = c.cpuReq > 0, c.memReq > 0
	return c
}

const dash = "—"

// metricCells returns the metric cells of a row: pods when !nodes.
func metricCells(nodes bool, u Usage, have bool, c capacity) []string {
	n := 6
	if nodes {
		n = 4
	}
	out := make([]string, n)
	for i := range out {
		out[i] = dash
	}
	if !have {
		return out
	}
	pct := func(use, of int64, ok bool) string {
		if !ok || of <= 0 {
			return dash
		}
		return fmt.Sprintf("%d %%", (use*100+of/2)/of)
	}
	out[0] = fmt.Sprintf("%dm", u.CPU)
	if nodes {
		out[1] = pct(u.CPU, c.cpuReq, c.okCPUReq)
		out[2], out[3] = fmtMem(u.Mem), pct(u.Mem, c.memReq, c.okMemReq)
		return out
	}
	out[1], out[2] = pct(u.CPU, c.cpuReq, c.okCPUReq), pct(u.CPU, c.cpuLim, c.okCPULim)
	out[3] = fmtMem(u.Mem)
	out[4], out[5] = pct(u.Mem, c.memReq, c.okMemReq), pct(u.Mem, c.memLim, c.okMemLim)
	return out
}

// fmtMem prints bytes as kubectl top does: whole Mi, or Gi with one decimal
// from 1 Gi up; whole Ki below 1 Mi.
func fmtMem(b int64) string {
	const ki, mi, gi = 1 << 10, 1 << 20, 1 << 30
	switch {
	case b >= gi:
		s := fmt.Sprintf("%.1f", float64(b)/gi)
		return strings.TrimSuffix(s, ".0") + "Gi"
	case b >= mi:
		return fmt.Sprintf("%dMi", (b+mi/2)/mi)
	default:
		return fmt.Sprintf("%dKi", (b+ki/2)/ki)
	}
}
