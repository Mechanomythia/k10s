package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
)

type seen struct{ method, path, body string }

// writeAPI serves just enough of an API server for the writes and records
// every request after discovery.
type writeAPI struct {
	mu   sync.Mutex
	reqs []seen
	put  int // status for PUT of the object (0 = echo it back)
}

func (a *writeAPI) handle(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/api":
		fmt.Fprint(w, `{"kind":"APIVersions","versions":["v1"]}`)
		return
	case "/api/v1":
		fmt.Fprint(w, `{"kind":"APIResourceList","groupVersion":"v1","resources":[]}`)
		return
	case "/apis":
		fmt.Fprint(w, `{"kind":"APIGroupList","groups":[{"name":"apps","versions":[{"groupVersion":"apps/v1","version":"v1"}],"preferredVersion":{"groupVersion":"apps/v1","version":"v1"}}]}`)
		return
	case "/apis/apps/v1":
		fmt.Fprint(w, `{"kind":"APIResourceList","groupVersion":"apps/v1","resources":[{"name":"deployments","namespaced":true,"kind":"Deployment","verbs":["get"]}]}`)
		return
	}
	a.mu.Lock()
	a.reqs = append(a.reqs, seen{r.Method, r.URL.Path, string(b)})
	a.mu.Unlock()
	const dep = `{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"web","namespace":"default","resourceVersion":"9"}}`
	switch {
	case r.Method == "PUT" && strings.HasSuffix(r.URL.Path, "/scale"):
		fmt.Fprint(w, string(b))
	case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/scale"):
		fmt.Fprint(w, `{"apiVersion":"autoscaling/v1","kind":"Scale","metadata":{"name":"web","namespace":"default"},"spec":{"replicas":2}}`)
	case r.Method == "PUT" && a.put != 0:
		w.WriteHeader(a.put)
		fmt.Fprintf(w, `{"kind":"Status","apiVersion":"v1","status":"Failure","message":"the object has been modified","code":%d}`, a.put)
	case r.Method == "PUT":
		fmt.Fprint(w, strings.Replace(dep, `"9"`, `"10"`, 1))
	default:
		fmt.Fprint(w, dep)
	}
}

func (a *writeAPI) only(t *testing.T) seen {
	t.Helper()
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.reqs) != 1 {
		t.Fatalf("requests = %v", a.reqs)
	}
	return a.reqs[0]
}

func writeEnv(t *testing.T) (*Detail, *writeAPI) {
	a := &writeAPI{}
	d := detailEnv(t, a.handle, nil, nil)
	d.Dynamic = nil // the real dynamic client, against the httptest server
	return d, a
}

var deploys = ResourceRef{Group: "apps", Version: "v1", Plural: "deployments"}

func TestDelete(t *testing.T) {
	d, a := writeEnv(t)
	if err := call(t, d.delete, ObjectParams{Context: "c", Resource: deploys, Namespace: "default", Name: "web"}, nil); err != nil {
		t.Fatal(err)
	}
	r := a.only(t)
	var body struct{ PropagationPolicy string }
	_ = json.Unmarshal([]byte(r.body), &body)
	if r.method != "DELETE" || r.path != "/apis/apps/v1/namespaces/default/deployments/web" || body.PropagationPolicy != "Background" {
		t.Errorf("got %+v", r)
	}
	if strings.Contains(r.body, "gracePeriodSeconds") {
		t.Errorf("grace period set: %s", r.body)
	}
}

func TestScale(t *testing.T) {
	d, a := writeEnv(t)
	if err := call(t, d.scale, ScaleParams{Context: "c", Resource: deploys, Namespace: "default", Name: "web", Replicas: 5}, nil); err != nil {
		t.Fatal(err)
	}
	if len(a.reqs) != 2 || a.reqs[0].method != "GET" || a.reqs[1].method != "PUT" {
		t.Fatalf("requests = %v", a.reqs)
	}
	if p := "/apis/apps/v1/namespaces/default/deployments/web/scale"; a.reqs[0].path != p || a.reqs[1].path != p {
		t.Errorf("paths = %v", a.reqs)
	}
	var sc struct{ Spec struct{ Replicas int } }
	_ = json.Unmarshal([]byte(a.reqs[1].body), &sc)
	if sc.Spec.Replicas != 5 {
		t.Errorf("put body %s", a.reqs[1].body)
	}
	// Not scalable, negative.
	for name, p := range map[string]ScaleParams{
		"daemonset": {Context: "c", Resource: ResourceRef{Group: "apps", Version: "v1", Plural: "daemonsets"}, Name: "x", Replicas: 1},
		"pods":      {Context: "c", Resource: ResourceRef{Version: "v1", Plural: "pods"}, Name: "x", Replicas: 1},
		"negative":  {Context: "c", Resource: deploys, Name: "x", Replicas: -1},
	} {
		if err := call(t, d.scale, p, nil); errCode(err) != CodeInvalidParams {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func errCode(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

func TestRestart(t *testing.T) {
	d, a := writeEnv(t)
	if err := call(t, d.restart, ObjectParams{Context: "c", Resource: deploys, Namespace: "default", Name: "web"}, nil); err != nil {
		t.Fatal(err)
	}
	r := a.only(t)
	var body struct {
		Spec struct {
			Template struct {
				Metadata struct{ Annotations map[string]string }
			}
		}
	}
	_ = json.Unmarshal([]byte(r.body), &body)
	if r.method != "PATCH" || r.path != "/apis/apps/v1/namespaces/default/deployments/web" || body.Spec.Template.Metadata.Annotations["kubectl.kubernetes.io/restartedAt"] == "" {
		t.Errorf("got %+v", r)
	}
	if err := call(t, d.restart, ObjectParams{Context: "c", Resource: ResourceRef{Version: "v1", Plural: "pods"}, Name: "x"}, nil); errCode(err) != CodeInvalidParams {
		t.Errorf("pods: %v", err)
	}
}

const depYAML = "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: web\n  namespace: default\n  resourceVersion: \"9\"\nspec:\n  replicas: 3\n"

func TestReplace(t *testing.T) {
	d, a := writeEnv(t)
	replace := mapAPIErrors(d.replace)
	p := ReplaceParams{Context: "c", Resource: deploys, Namespace: "default", Name: "web", YAML: depYAML}
	var res ReplaceResult
	if err := call(t, replace, p, &res); err != nil {
		t.Fatal(err)
	}
	r := a.only(t)
	if r.method != "PUT" || !strings.Contains(r.body, `"resourceVersion":"9"`) || res.ResourceVersion != "10" {
		t.Errorf("got %+v result %+v", r, res)
	}

	a.put = http.StatusConflict
	if err := call(t, replace, p, nil); errCode(err) != CodeConflict {
		t.Errorf("409: %v", err)
	}
	a.put = http.StatusForbidden
	if err := call(t, replace, p, nil); errCode(err) != CodeForbidden {
		t.Errorf("403: %v", err)
	}
	n := len(a.reqs)
	for name, y := range map[string]string{
		"name":      strings.Replace(depYAML, "name: web", "name: other", 1),
		"namespace": strings.Replace(depYAML, "namespace: default", "namespace: kube-system", 1),
		"version":   strings.Replace(depYAML, "apps/v1", "apps/v2", 1),
		"kind":      strings.Replace(depYAML, "Deployment", "StatefulSet", 1),
	} {
		q := p
		q.YAML = y
		err := call(t, replace, q, nil)
		if errCode(err) != CodeInvalidParams || !strings.Contains(err.Error(), "the YAML names a different object") {
			t.Errorf("%s: %v", name, err)
		}
	}
	if len(a.reqs) != n {
		t.Errorf("a mismatched replace reached the server: %v", a.reqs[n:])
	}
}

func TestForbiddenTable(t *testing.T) {
	f := newFake(t, []string{"Name", "Status"}, fakePod{"default", "a", "u1", "Running"})
	f.status = http.StatusForbidden
	conns := connsFor(t, map[string]string{"dev": f.srv.URL})
	var states []ContextState
	defer conns.Listen(func(s ContextStateChange) { states = append(states, s.State) })()
	s := startSub(t, newTables(conns), TableSubscribeParams{Contexts: []string{"dev"}, Resource: ResourceRef{Version: "v1", Plural: "pods"}, AllNamespaces: true})
	fb := s.next(t, EventForbidden).(Forbidden)
	if fb.Context != "dev" || !strings.Contains(fb.Message, "forbidden") {
		t.Errorf("forbidden = %+v", fb)
	}
	s.next(t, EventSnapshot) // the table still answers, with no rows
	for _, st := range states {
		if st == StateUnauthorized || st == StateUnreachable {
			t.Errorf("states = %v", states)
		}
	}
	if states[len(states)-1] != StateLive {
		t.Errorf("states = %v, want live last", states)
	}
}

func TestUnauthorizedTable(t *testing.T) {
	f := newFake(t, []string{"Name"})
	f.status = http.StatusUnauthorized
	conns := connsFor(t, map[string]string{"dev": f.srv.URL})
	s := startSub(t, newTables(conns), TableSubscribeParams{Contexts: []string{"dev"}, Resource: ResourceRef{Version: "v1", Plural: "pods"}, AllNamespaces: true})
	s.saw(t, func(g got) bool {
		c, ok := g.data.(ContextStateChange)
		return g.event == EventContextState && ok && c.State == StateUnauthorized
	})
}
