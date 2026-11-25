package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakePod struct{ ns, name, uid, phase string }

// fakeAPI is an API server with discovery and Table list/watch of pods.
type fakeAPI struct {
	srv      *httptest.Server
	cols     []string
	fail     bool            // 500 for everything
	status   int             // if set, table lists answer this status as a Status object
	listGate <-chan struct{} // if set, lists wait for it

	mu     sync.Mutex
	pods   []fakePod
	owners map[string]string // pod uid -> uid of its owner reference

	fieldSelector atomic.Value // the fieldSelector query of the last list

	lists, watches atomic.Int32
	opened         chan struct{} // closed when the first watch opens
	openOnce       sync.Once
	watchOpen      chan struct{} // one value per watch opened
	watchClosed    chan struct{} // one value per watch closed
	events         chan string   // watch event lines for the open watch
	done           chan struct{}
}

func newFake(t *testing.T, cols []string, pods ...fakePod) *fakeAPI {
	f := &fakeAPI{cols: cols, pods: pods, opened: make(chan struct{}), watchOpen: make(chan struct{}, 10),
		watchClosed: make(chan struct{}, 10), events: make(chan string), done: make(chan struct{})}
	f.srv = httptest.NewServer(f)
	t.Cleanup(func() { close(f.done); f.srv.Close() })
	return f
}

func (f *fakeAPI) podJSON(p fakePod) string {
	owner := ""
	if o := f.owners[p.uid]; o != "" {
		owner = fmt.Sprintf(`"ownerReferences":[{"apiVersion":"apps/v1","kind":"ReplicaSet","name":"rs","uid":%q}],`, o)
	}
	return fmt.Sprintf(`{"apiVersion":"v1","kind":"Pod","metadata":{%s"name":%q,"namespace":%q,"uid":%q,"resourceVersion":"7"},"status":{"phase":%q}}`, owner, p.name, p.ns, p.uid, p.phase)
}

// table prints pods in f.cols, with column definitions when withCols.
func (f *fakeAPI) table(withCols bool, pods ...fakePod) string {
	var defs, rows []string
	for i, c := range f.cols {
		defs = append(defs, fmt.Sprintf(`{"name":%q,"type":"string","format":"","description":"","priority":%d}`, c, i/3))
	}
	for _, p := range pods {
		var cells []string
		for _, c := range f.cols {
			switch c {
			case "Name":
				cells = append(cells, fmt.Sprintf("%q", p.name))
			case "Status":
				cells = append(cells, fmt.Sprintf("%q", p.phase))
			case "Ready":
				cells = append(cells, `"1/1"`)
			case "Restarts":
				cells = append(cells, `0`)
			default:
				cells = append(cells, `"5m"`)
			}
		}
		rows = append(rows, fmt.Sprintf(`{"cells":[%s],"object":%s}`, strings.Join(cells, ","), f.podJSON(p)))
	}
	cols := ""
	if withCols {
		cols = `"columnDefinitions":[` + strings.Join(defs, ",") + `],`
	}
	return `{"kind":"Table","apiVersion":"meta.k8s.io/v1","metadata":{"resourceVersion":"7"},` + cols + `"rows":[` + strings.Join(rows, ",") + `]}`
}

// send streams one watch event to the open watch.
func (f *fakeAPI) send(t *testing.T, typ string, p fakePod) {
	t.Helper()
	f.sendRaw(t, fmt.Sprintf(`{"type":%q,"object":%s}`, typ, f.table(false, p)))
}

func (f *fakeAPI) sendRaw(t *testing.T, line string) {
	t.Helper()
	select {
	case f.events <- line:
	case <-time.After(5 * time.Second):
		t.Fatal("no open watch took the event")
	}
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if f.fail {
		http.Error(w, "boom", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/version":
		fmt.Fprint(w, `{"major":"1","minor":"31","gitVersion":"v1.31.0"}`)
	case "/api":
		fmt.Fprint(w, `{"kind":"APIVersions","versions":["v1"],"serverAddressByClientCIDRs":null}`)
	case "/apis":
		fmt.Fprint(w, `{"kind":"APIGroupList","apiVersion":"v1","groups":[{"name":"apps","versions":[{"groupVersion":"apps/v1","version":"v1"}],"preferredVersion":{"groupVersion":"apps/v1","version":"v1"}}]}`)
	case "/api/v1":
		fmt.Fprint(w, `{"kind":"APIResourceList","apiVersion":"v1","groupVersion":"v1","resources":[
			{"name":"pods","singularName":"pod","namespaced":true,"kind":"Pod","verbs":["get","list","watch"],"shortNames":["po"]},
			{"name":"pods/log","singularName":"","namespaced":true,"kind":"Pod","verbs":["get"]},
			{"name":"bindings","singularName":"","namespaced":true,"kind":"Binding","verbs":["create"]},
			{"name":"nodes","singularName":"node","namespaced":false,"kind":"Node","verbs":["get","list","watch"]}]}`)
	case "/apis/apps/v1":
		fmt.Fprint(w, `{"kind":"APIResourceList","apiVersion":"v1","groupVersion":"apps/v1","resources":[
			{"name":"deployments","singularName":"deployment","namespaced":true,"kind":"Deployment","verbs":["get","list","watch"],"shortNames":["deploy"]}]}`)
	case "/api/v1/pods", "/api/v1/namespaces/default/pods":
		if r.Header.Get("Accept") != tableAccept || r.URL.Query().Get("includeObject") != "Object" {
			http.Error(w, "not a table request", http.StatusNotAcceptable)
			return
		}
		if r.URL.Query().Get("watch") != "" {
			f.serveWatch(w, r)
			return
		}
		f.lists.Add(1)
		f.fieldSelector.Store(r.URL.Query().Get("fieldSelector"))
		if f.status != 0 {
			w.WriteHeader(f.status)
			fmt.Fprintf(w, `{"kind":"Status","apiVersion":"v1","status":"Failure","message":"pods is forbidden: nope","code":%d}`, f.status)
			return
		}
		if f.listGate != nil {
			<-f.listGate
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		fmt.Fprint(w, f.table(true, f.pods...))
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeAPI) serveWatch(w http.ResponseWriter, r *http.Request) {
	f.watches.Add(1)
	w.WriteHeader(http.StatusOK)
	w.(http.Flusher).Flush()
	f.openOnce.Do(func() { close(f.opened) })
	f.watchOpen <- struct{}{}
	defer func() { f.watchClosed <- struct{}{} }()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-f.done:
			return
		case line := <-f.events:
			fmt.Fprintln(w, line)
			w.(http.Flusher).Flush()
		}
	}
}

// connsFor writes one kubeconfig file per context pointing at its server.
func connsFor(t *testing.T, servers map[string]string) *Conns {
	home := isolate(t)
	var paths []string
	for name, url := range servers {
		p := filepath.Join(home, name)
		writeKube(t, p, name, map[string]string{name: ""}, url)
		paths = append(paths, p)
	}
	t.Setenv("KUBECONFIG", strings.Join(paths, string(os.PathListSeparator)))
	return NewConns(NewKubeconfig())
}

func newTables(c *Conns) *Tables {
	tb := NewTables(c)
	tb.Linger = 50 * time.Millisecond
	tb.SnapshotWait = 10 * time.Second // snapshots in these tests must not need the wait
	return tb
}

type got struct {
	event string
	data  any
}

// testSub runs one table subscription with a recording emit.
type testSub struct {
	ch     chan got
	log    []got
	cancel context.CancelFunc
	done   chan struct{}
}

func startSub(t *testing.T, tb *Tables, p TableSubscribeParams) *testSub {
	t.Helper()
	raw, _ := json.Marshal(p)
	ctx, cancel := context.WithCancel(context.Background())
	run, err := tb.subscribe(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	s := &testSub{ch: make(chan got, 1000), cancel: cancel, done: make(chan struct{})}
	var returned atomic.Bool
	go func() {
		run(func(e string, d any) {
			if returned.Load() {
				t.Errorf("emit %s after the subscription ended", e)
			}
			s.ch <- got{e, d}
		})
		returned.Store(true)
		close(s.done)
	}()
	t.Cleanup(s.stop)
	return s
}

func (s *testSub) stop() {
	s.cancel()
	<-s.done
}

// next returns the next event of the given name, logging the ones it skips.
func (s *testSub) next(t *testing.T, event string) any {
	t.Helper()
	for {
		select {
		case g := <-s.ch:
			s.log = append(s.log, g)
			if g.event == event {
				return g.data
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("no %s event; saw %v", event, s.log)
		}
	}
}

// saw waits for an event, seen already or not, that matches ok.
func (s *testSub) saw(t *testing.T, ok func(got) bool) {
	t.Helper()
	for _, g := range s.log {
		if ok(g) {
			return
		}
	}
	for {
		select {
		case g := <-s.ch:
			s.log = append(s.log, g)
			if ok(g) {
				return
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("no matching event; saw %v", s.log)
		}
	}
}

func waitSignal(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

var pods = ResourceRef{Version: "v1", Plural: "pods"}

func TestResourcesList(t *testing.T) {
	f := newFake(t, nil)
	c := connsFor(t, map[string]string{"a": f.srv.URL})
	kinds, err := c.Resources(context.Background(), "a")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]ResourceKind{
		"pods":        {Version: "v1", Kind: "Pod", Plural: "pods", ShortNames: []string{"po"}, Namespaced: true},
		"nodes":       {Version: "v1", Kind: "Node", Plural: "nodes"},
		"deployments": {Group: "apps", Version: "v1", Kind: "Deployment", Plural: "deployments", ShortNames: []string{"deploy"}, Namespaced: true},
	}
	if len(kinds) != len(want) {
		t.Fatalf("got %+v", kinds)
	}
	for _, k := range kinds {
		if !reflect.DeepEqual(k, want[k.Plural]) {
			t.Errorf("got %+v, want %+v", k, want[k.Plural])
		}
	}
}

func TestTableMergeAndFlow(t *testing.T) {
	a := newFake(t, []string{"Name", "Ready", "Status", "Restarts"}, fakePod{"default", "web-1", "ua1", "Running"})
	b := newFake(t, []string{"Status", "Name", "Age"}, fakePod{"default", "db-1", "ub1", "Pending"})
	b.listGate = a.opened // a answers first, so its columns win
	tb := newTables(connsFor(t, map[string]string{"a": a.srv.URL, "b": b.srv.URL}))
	s := startSub(t, tb, TableSubscribeParams{Contexts: []string{"a", "b"}, Resource: pods, AllNamespaces: true})

	snap := s.next(t, EventSnapshot).(Snapshot)
	wantCols := []Column{{Name: "Name"}, {Name: "Ready"}, {Name: "Status"}, {Name: "Restarts", Priority: 1}}
	wantRows := []Row{
		{Context: "a", Namespace: "default", Name: "web-1", UID: "ua1", Cells: []string{"web-1", "1/1", "Running", "0"}, Health: HealthOK},
		{Context: "b", Namespace: "default", Name: "db-1", UID: "ub1", Cells: []string{"db-1", "", "Pending", ""}, Health: HealthPending},
	}
	if !reflect.DeepEqual(snap.Columns, wantCols) || !reflect.DeepEqual(snap.Rows, wantRows) {
		t.Fatalf("snapshot %+v", snap)
	}

	a.send(t, "ADDED", fakePod{"default", "web-2", "ua2", "Running"})
	if u := s.next(t, EventUpsert).(Upsert); u.Row.Context != "a" || u.Row.Name != "web-2" || u.Row.Health != HealthOK {
		t.Errorf("upsert %+v", u)
	}
	a.send(t, "MODIFIED", fakePod{"default", "web-2", "ua2", "Failed"})
	if u := s.next(t, EventUpsert).(Upsert); u.Row.UID != "ua2" || u.Row.Health != HealthFailed || u.Row.Cells[2] != "Failed" {
		t.Errorf("upsert %+v", u)
	}
	b.send(t, "ADDED", fakePod{"default", "db-2", "ub2", "Running"})
	if u := s.next(t, EventUpsert).(Upsert); u.Row.Context != "b" || !reflect.DeepEqual(u.Row.Cells, []string{"db-2", "", "Running", ""}) {
		t.Errorf("upsert from b not reordered: %+v", u)
	}
	a.send(t, "DELETED", fakePod{"default", "web-2", "ua2", "Failed"})
	if d := s.next(t, EventDelete).(Delete); d != (Delete{Context: "a", Namespace: "default", Name: "web-2", UID: "ua2"}) {
		t.Errorf("delete %+v", d)
	}
}

func TestTableFailingContext(t *testing.T) {
	a := newFake(t, []string{"Name"}, fakePod{"default", "web-1", "ua1", "Running"})
	b := newFake(t, nil)
	b.fail = true
	tb := newTables(connsFor(t, map[string]string{"a": a.srv.URL, "b": b.srv.URL}))
	s := startSub(t, tb, TableSubscribeParams{Contexts: []string{"a", "b"}, Resource: pods, AllNamespaces: true})

	snap := s.next(t, EventSnapshot).(Snapshot)
	if len(snap.Rows) != 1 || snap.Rows[0].Context != "a" {
		t.Fatalf("snapshot %+v", snap)
	}
	s.saw(t, func(g got) bool {
		st, ok := g.data.(ContextStateChange)
		return ok && st.Context == "b" && st.State == StateUnreachable && st.Error != ""
	})
	a.send(t, "ADDED", fakePod{"default", "web-2", "ua2", "Running"})
	if u := s.next(t, EventUpsert).(Upsert); u.Row.Name != "web-2" {
		t.Errorf("upsert %+v", u)
	}
}

func TestTableSharedWatchLinger(t *testing.T) {
	a := newFake(t, []string{"Name"}, fakePod{"default", "web-1", "ua1", "Running"})
	tb := newTables(connsFor(t, map[string]string{"a": a.srv.URL}))
	p := TableSubscribeParams{Contexts: []string{"a"}, Resource: pods, AllNamespaces: true}
	s1 := startSub(t, tb, p)
	s1.next(t, EventSnapshot)
	waitSignal(t, a.watchOpen, "watch")
	s2 := startSub(t, tb, p)
	if snap := s2.next(t, EventSnapshot).(Snapshot); len(snap.Rows) != 1 {
		t.Errorf("second snapshot %+v", snap)
	}
	if a.lists.Load() != 1 || a.watches.Load() != 1 {
		t.Errorf("%d lists, %d watches; want one shared", a.lists.Load(), a.watches.Load())
	}

	s1.stop()
	select {
	case <-a.watchClosed:
		t.Fatal("watch closed while a subscriber remains")
	case <-time.After(4 * tb.Linger):
	}
	s2.stop()
	waitSignal(t, a.watchClosed, "watch to close after linger")
}

func TestTableRelistOnGone(t *testing.T) {
	a := newFake(t, []string{"Name"}, fakePod{"default", "web-1", "ua1", "Running"})
	tb := newTables(connsFor(t, map[string]string{"a": a.srv.URL}))
	s := startSub(t, tb, TableSubscribeParams{Contexts: []string{"a"}, Resource: pods, Namespace: "default", LabelSelector: "app=web"})
	s.next(t, EventSnapshot)
	waitSignal(t, a.watchOpen, "watch")

	a.mu.Lock()
	a.pods = append(a.pods, fakePod{"default", "web-3", "ua3", "Running"})
	a.mu.Unlock()
	a.sendRaw(t, `{"type":"ERROR","object":{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"Expired","code":410}}`)
	waitSignal(t, a.watchClosed, "expired watch to close")
	s.saw(t, func(g got) bool { u, ok := g.data.(Upsert); return ok && u.Row.Name == "web-3" })
	waitSignal(t, a.watchOpen, "new watch")
	if a.lists.Load() != 2 || a.watches.Load() != 2 {
		t.Errorf("%d lists, %d watches; want a re-list and a new watch", a.lists.Load(), a.watches.Load())
	}
}

func TestTableFieldSelectorReachesServer(t *testing.T) {
	a := newFake(t, []string{"Name"}, fakePod{"default", "web-1", "u1", "Running"})
	tb := newTables(connsFor(t, map[string]string{"a": a.srv.URL}))
	s := startSub(t, tb, TableSubscribeParams{Contexts: []string{"a"}, Resource: pods, AllNamespaces: true, FieldSelector: "spec.nodeName=n1"})
	s.next(t, EventSnapshot)
	if got := a.fieldSelector.Load(); got != "spec.nodeName=n1" {
		t.Errorf("fieldSelector query = %v", got)
	}
	// The selector is part of the watch's key: another one is another watch.
	s2 := startSub(t, tb, TableSubscribeParams{Contexts: []string{"a"}, Resource: pods, AllNamespaces: true, FieldSelector: "spec.nodeName=n2"})
	s2.next(t, EventSnapshot)
	if a.lists.Load() != 2 {
		t.Errorf("%d lists, want one per field selector", a.lists.Load())
	}
}

func TestTableOwnerFilter(t *testing.T) {
	a := newFake(t, []string{"Name"},
		fakePod{"default", "mine", "u1", "Running"}, fakePod{"default", "other", "u2", "Running"}, fakePod{"default", "orphan", "u3", "Running"})
	a.owners = map[string]string{"u1": "job-1", "u2": "job-2"}
	tb := newTables(connsFor(t, map[string]string{"a": a.srv.URL}))
	s := startSub(t, tb, TableSubscribeParams{Contexts: []string{"a"}, Resource: pods, Namespace: "default", OwnerUID: "job-1"})
	snap := s.next(t, EventSnapshot).(Snapshot)
	if len(snap.Rows) != 1 || snap.Rows[0].Name != "mine" {
		t.Fatalf("snapshot rows = %+v, want only mine", snap.Rows)
	}
	waitSignal(t, a.watchOpen, "watch")

	// A deletion of a row that was never shown is ignored; a new owned row and
	// a foreign one: only the owned one is sent.
	a.send(t, "DELETED", fakePod{"default", "other", "u2", "Running"})
	a.send(t, "ADDED", fakePod{"default", "foreign", "u4", "Pending"})
	a.owners["u5"] = "job-1"
	a.send(t, "ADDED", fakePod{"default", "new", "u5", "Pending"})
	s.saw(t, func(g got) bool { u, ok := g.data.(Upsert); return ok && u.Row.Name == "new" })

	// A MODIFIED row that lost its owner is removed.
	a.mu.Lock()
	delete(a.owners, "u1")
	a.mu.Unlock()
	a.send(t, "MODIFIED", fakePod{"default", "mine", "u1", "Running"})
	s.saw(t, func(g got) bool { d, ok := g.data.(Delete); return ok && d.UID == "u1" })
	for _, g := range s.log {
		switch v := g.data.(type) {
		case Upsert:
			if v.Row.Name != "new" {
				t.Errorf("upsert of %s passed the owner filter", v.Row.Name)
			}
		case Delete:
			if v.UID != "u1" {
				t.Errorf("delete of %s, never shown", v.Name)
			}
		}
	}
}
