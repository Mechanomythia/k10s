package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	dynfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes"
	kfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
)

// detailEnv returns a Detail whose context "c" is live against an httptest
// server (handler serves everything except /version) and uses the given fakes.
func detailEnv(t *testing.T, h http.HandlerFunc, cs kubernetes.Interface, dc dynamic.Interface) *Detail {
	t.Helper()
	home := isolate(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/version" {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"major":"1","minor":"31","gitVersion":"v1.31.0"}`)
			return
		}
		if h == nil {
			http.NotFound(w, r)
			return
		}
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	f := filepath.Join(home, "kc")
	writeKube(t, f, "c", map[string]string{"c": ""}, srv.URL)
	t.Setenv("KUBECONFIG", f)
	return &Detail{
		Conns:     NewConns(NewKubeconfig()),
		Dynamic:   func(*rest.Config) (dynamic.Interface, error) { return dc, nil },
		Clientset: func(*rest.Config) (kubernetes.Interface, error) { return cs, nil },
	}
}

func call(t *testing.T, h Handler, params any, out any) error {
	t.Helper()
	raw, _ := json.Marshal(params)
	res, err := h(context.Background(), raw)
	if err == nil && out != nil {
		b, _ := json.Marshal(res)
		if err := json.Unmarshal(b, out); err != nil {
			t.Fatal(err)
		}
	}
	return err
}

func TestYAMLStripsManagedFields(t *testing.T) {
	cm := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": "ConfigMap",
		"metadata": map[string]any{
			"name": "cm", "namespace": "default",
			"managedFields": []any{map[string]any{"manager": "kubectl"}},
		},
		"data": map[string]any{"k": "v"},
	}}
	gvr := schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}
	dc := dynfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{gvr: "ConfigMapList"}, cm)
	d := detailEnv(t, nil, nil, dc)
	var res TextResult
	err := call(t, d.yaml, ObjectParams{Context: "c", Resource: ResourceRef{Version: "v1", Plural: "configmaps"}, Namespace: "default", Name: "cm"}, &res)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(res.Text, "managedFields") || strings.Contains(res.Text, "kubectl") {
		t.Errorf("managedFields not stripped:\n%s", res.Text)
	}
	if !strings.Contains(res.Text, "name: cm") || !strings.Contains(res.Text, "k: v") {
		t.Errorf("yaml incomplete:\n%s", res.Text)
	}
	// Missing object: an error, not a panic.
	if err := call(t, d.yaml, ObjectParams{Context: "c", Resource: ResourceRef{Version: "v1", Plural: "configmaps"}, Namespace: "default", Name: "nope"}, nil); err == nil {
		t.Error("want not found")
	}
}

func TestInvalidParams(t *testing.T) {
	d := &Detail{Conns: NewConns(NewKubeconfig())}
	for name, h := range map[string]Handler{"yaml": d.yaml, "describe": d.describe, "events": d.events} {
		for _, params := range []string{`{}`, `not json`} {
			err := func() error { _, err := h(context.Background(), json.RawMessage(params)); return err }()
			var e *Error
			if !errors.As(err, &e) || e.Code != CodeInvalidParams {
				t.Errorf("%s %s: err = %v", name, params, err)
			}
		}
	}
	if _, err := d.logs(context.Background(), json.RawMessage(`{"context":"c"}`)); err == nil {
		t.Error("logs: want invalid_params")
	}
}

func TestEventsFilteredAndSorted(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	ev := func(name, uid, reason string, last time.Time) *corev1.Event {
		e := &corev1.Event{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "ns", CreationTimestamp: metav1.NewTime(now.Add(-time.Hour))},
			InvolvedObject: corev1.ObjectReference{UID: "u1"}, Type: "Normal", Reason: reason, Message: "m " + reason, Count: 3}
		if uid != "" {
			e.InvolvedObject.UID = "other"
		}
		if !last.IsZero() {
			e.LastTimestamp = metav1.NewTime(last)
		}
		return e
	}
	oldE := ev("a", "", "Old", now.Add(-10*time.Minute))
	newE := ev("b", "", "New", now)
	createdOnly := ev("c", "", "Created", time.Time{}) // falls back to creationTimestamp (-1h)
	foreign := ev("d", "x", "Foreign", now.Add(time.Minute))
	d := detailEnv(t, nil, kfake.NewSimpleClientset(oldE, createdOnly, foreign, newE), nil)
	var res ObjectEventsResult
	if err := call(t, d.events, ObjectEventsParams{Context: "c", Namespace: "ns", UID: "u1"}, &res); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range res.Events {
		got = append(got, e.Reason)
	}
	if strings.Join(got, ",") != "New,Old,Created" {
		t.Fatalf("reasons = %v", got)
	}
	if res.Events[0].LastSeen != "2026-01-01T12:00:00Z" || res.Events[0].Count != 3 || res.Events[0].Type != "Normal" {
		t.Errorf("event = %+v", res.Events[0])
	}
	// No events: an empty list, not null.
	var none ObjectEventsResult
	call(t, d.events, ObjectEventsParams{Context: "c", Namespace: "ns", UID: "zzz"}, &none)
	if b, _ := json.Marshal(none); string(b) != `{"events":[]}` {
		t.Errorf("empty = %s", b)
	}
}

// describeAPI serves discovery, a ConfigMap, a CRD kind "Widget" and no events.
func describeAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/api":
		fmt.Fprint(w, `{"kind":"APIVersions","versions":["v1"]}`)
	case "/apis":
		fmt.Fprint(w, `{"kind":"APIGroupList","apiVersion":"v1","groups":[{"name":"x.io","versions":[{"groupVersion":"x.io/v1","version":"v1"}],"preferredVersion":{"groupVersion":"x.io/v1","version":"v1"}}]}`)
	case "/api/v1":
		fmt.Fprint(w, `{"kind":"APIResourceList","groupVersion":"v1","resources":[{"name":"configmaps","namespaced":true,"kind":"ConfigMap","verbs":["get","list"]}]}`)
	case "/apis/x.io/v1":
		fmt.Fprint(w, `{"kind":"APIResourceList","groupVersion":"x.io/v1","resources":[{"name":"widgets","namespaced":true,"kind":"Widget","verbs":["get","list"]}]}`)
	case "/api/v1/namespaces/default/configmaps/cm":
		fmt.Fprint(w, `{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"cm","namespace":"default"},"data":{"k":"v"}}`)
	case "/apis/x.io/v1/namespaces/default/widgets/w":
		fmt.Fprint(w, `{"apiVersion":"x.io/v1","kind":"Widget","metadata":{"name":"w","namespace":"default"},"spec":{"size":3}}`)
	case "/api/v1/namespaces/default/events":
		fmt.Fprint(w, `{"apiVersion":"v1","kind":"EventList","items":[]}`)
	default:
		http.NotFound(w, r)
	}
}

func TestDescribe(t *testing.T) {
	d := detailEnv(t, describeAPI, nil, nil)
	for _, tt := range []struct {
		name string
		res  ResourceRef
		want string
	}{
		{"typed", ResourceRef{Version: "v1", Plural: "configmaps"}, "Name:         cm"},
		{"generic fallback for CRD", ResourceRef{Group: "x.io", Version: "v1", Plural: "widgets"}, "Name:"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			name := map[string]string{"typed": "cm"}[tt.name]
			if name == "" {
				name = "w"
			}
			var res TextResult
			if err := call(t, d.describe, ObjectParams{Context: "c", Resource: tt.res, Namespace: "default", Name: name}, &res); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(res.Text, tt.want) || !strings.Contains(res.Text, name) {
				t.Errorf("describe:\n%s", res.Text)
			}
		})
	}
	// Unknown resource: error, not a panic.
	if err := call(t, d.describe, ObjectParams{Context: "c", Resource: ResourceRef{Version: "v1", Plural: "nopes"}, Namespace: "default", Name: "n"}, nil); err == nil {
		t.Error("want error for unknown resource")
	}
}

// ---- logs ----

type emitted struct {
	mu   sync.Mutex
	evs  []string
	data []json.RawMessage
}

func (e *emitted) emit(event string, v any) {
	b, _ := json.Marshal(v)
	e.mu.Lock()
	e.evs = append(e.evs, event)
	e.data = append(e.data, b)
	e.mu.Unlock()
}

func (e *emitted) snapshot() ([]string, []json.RawMessage) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.evs...), append([]json.RawMessage(nil), e.data...)
}

func podWith(containers ...string) *corev1.Pod {
	p := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "ns"}}
	for _, c := range containers {
		p.Spec.Containers = append(p.Spec.Containers, corev1.Container{Name: c})
	}
	return p
}

func logsEnv(t *testing.T, pod *corev1.Pod, streams map[string]io.ReadCloser) *Detail {
	d := detailEnv(t, nil, kfake.NewSimpleClientset(pod), nil)
	d.Open = func(_ context.Context, _ kubernetes.Interface, _ LogsSubscribeParams, c string) (io.ReadCloser, error) {
		return streams[c], nil
	}
	return d
}

func startLogs(t *testing.T, d *Detail, ctx context.Context, p LogsSubscribeParams) (*emitted, <-chan struct{}) {
	t.Helper()
	raw, _ := json.Marshal(p)
	run, err := d.logs(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	em := &emitted{}
	done := make(chan struct{})
	go func() { run(em.emit); close(done) }()
	return em, done
}

func allLines(em *emitted) (lines []string, batches int, last string) {
	evs, data := em.snapshot()
	for i, e := range evs {
		last = e
		if e == EventLines {
			var l Lines
			json.Unmarshal(data[i], &l)
			lines = append(lines, l.Lines...)
			batches++
		}
	}
	return
}

func TestLogsMultiContainerNeedsContainer(t *testing.T) {
	d := logsEnv(t, podWith("a", "b"), nil)
	raw, _ := json.Marshal(LogsSubscribeParams{Context: "c", Namespace: "ns", Pod: "p", TailLines: 500, Follow: true})
	_, err := d.logs(context.Background(), raw)
	var e *Error
	if !errors.As(err, &e) || e.Code != CodeInvalidParams || !strings.Contains(e.Message, "a, b") {
		t.Fatalf("err = %v", err)
	}
}

func TestLogsEndOnEOFAndSingleContainer(t *testing.T) {
	d := logsEnv(t, podWith("only"), map[string]io.ReadCloser{"": io.NopCloser(strings.NewReader("one\ntwo\r\nlast-no-newline"))})
	em, done := startLogs(t, d, context.Background(), LogsSubscribeParams{Context: "c", Namespace: "ns", Pod: "p", TailLines: 500, Follow: true})
	<-done
	lines, _, last := allLines(em)
	if strings.Join(lines, "|") != "one|two|last-no-newline" || last != EventEnd {
		t.Errorf("lines=%v last=%s", lines, last)
	}
	_, data := em.snapshot()
	if string(data[len(data)-1]) != `{}` {
		t.Errorf("end = %s", data[len(data)-1])
	}
}

func TestLogsAllContainersPrefix(t *testing.T) {
	d := logsEnv(t, podWith("a", "b"), map[string]io.ReadCloser{
		"a": io.NopCloser(strings.NewReader("x\n")),
		"b": io.NopCloser(strings.NewReader("y\n")),
	})
	em, done := startLogs(t, d, context.Background(), LogsSubscribeParams{Context: "c", Namespace: "ns", Pod: "p", AllContainers: true, TailLines: 500})
	<-done
	lines, _, _ := allLines(em)
	if len(lines) != 2 {
		t.Fatalf("lines = %v", lines)
	}
	got := map[string]bool{lines[0]: true, lines[1]: true}
	if !got["[a] x"] || !got["[b] y"] {
		t.Errorf("lines = %v", lines)
	}
}

func TestLogsErrorEnd(t *testing.T) {
	d := logsEnv(t, podWith("a"), nil)
	d.Open = func(context.Context, kubernetes.Interface, LogsSubscribeParams, string) (io.ReadCloser, error) {
		return nil, errors.New("container is waiting to start")
	}
	em, done := startLogs(t, d, context.Background(), LogsSubscribeParams{Context: "c", Namespace: "ns", Pod: "p"})
	<-done
	evs, data := em.snapshot()
	if len(evs) != 1 || evs[0] != EventEnd || !strings.Contains(string(data[0]), "waiting to start") {
		t.Errorf("events = %v %s", evs, data)
	}
}

func TestLogsDefaultOpenUsesClientset(t *testing.T) {
	d := detailEnv(t, nil, kfake.NewSimpleClientset(podWith("a")), nil)
	em, done := startLogs(t, d, context.Background(), LogsSubscribeParams{Context: "c", Namespace: "ns", Pod: "p", TailLines: 5})
	<-done
	if lines, _, last := allLines(em); len(lines) != 1 || lines[0] != "fake logs" || last != EventEnd {
		t.Errorf("lines=%v last=%s", lines, last)
	}
}

// pipeStream is a log stream the test feeds and closes.
type pipeStream struct{ *io.PipeReader }

func newPipe() (*pipeStream, *io.PipeWriter) {
	r, w := io.Pipe()
	return &pipeStream{r}, w
}

func TestLogsBatchesOnTimeAndSize(t *testing.T) {
	oldI, oldS := logFlushInterval, logBatchSize
	defer func() { logFlushInterval, logBatchSize = oldI, oldS }()

	run := func(interval time.Duration, size int, input string, want func(lines, batches int) bool) {
		logFlushInterval, logBatchSize = interval, size
		ps, w := newPipe()
		d := logsEnv(t, podWith("a"), map[string]io.ReadCloser{"": ps})
		em, done := startLogs(t, d, context.Background(), LogsSubscribeParams{Context: "c", Namespace: "ns", Pod: "p", Follow: true})
		fmt.Fprint(w, input)
		// The stream stays open, so only a flush can deliver the lines.
		waitFor(t, func() bool { l, b, _ := allLines(em); return want(len(l), b) })
		w.Close()
		<-done
	}
	// Size: the timer never fires, three lines flush at once, the 4th waits for EOF.
	run(time.Hour, 3, "1\n2\n3\n", func(l, b int) bool { return l == 3 && b == 1 })
	// Time: far below the size limit, one line still flushes on the interval.
	run(20*time.Millisecond, 500, "1\n", func(l, b int) bool { return l == 1 && b == 1 })
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("timeout")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestLogsCancelStops(t *testing.T) {
	ps, w := newPipe()
	defer w.Close()
	d := logsEnv(t, podWith("a"), map[string]io.ReadCloser{"": ps})
	ctx, cancel := context.WithCancel(context.Background())
	em, done := startLogs(t, d, ctx, LogsSubscribeParams{Context: "c", Namespace: "ns", Pod: "p", Follow: true})
	go fmt.Fprint(w, "pending\n")
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("stream did not stop after cancel")
	}
	evs, _ := em.snapshot()
	for _, e := range evs {
		if e == EventEnd {
			t.Error("end emitted after cancel")
		}
	}
	// Nothing is emitted after run returned.
	n := len(evs)
	time.Sleep(50 * time.Millisecond)
	if evs2, _ := em.snapshot(); len(evs2) != n {
		t.Error("emit after cancel")
	}
	if _, err := w.Write([]byte("late\n")); err == nil {
		t.Error("stream not closed after cancel")
	}
}
