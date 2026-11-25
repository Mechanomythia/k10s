package engine

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/rest"
)

// tableAccept asks the API server to print objects as a Table.
const tableAccept = "application/json;as=Table;v=v1;g=meta.k8s.io"

// Backoff between attempts of a failing or dropped watch.
const (
	minBackoff = time.Second
	maxBackoff = 30 * time.Second
)

var (
	errGone     = errors.New("resource version expired")
	errReported = errors.New("state already reported") // Conns.Get broadcast it
)

// Tables serves table.subscribe: server-printed tables across contexts, on
// watches shared between subscriptions.
type Tables struct {
	conns        *Conns
	Linger       time.Duration // how long a watch outlives its last subscriber
	SnapshotWait time.Duration // how long the first snapshot waits for slow contexts

	mu      sync.Mutex
	watches map[watchKey]*sharedWatch
}

// NewTables returns Tables with the ADR's 30 s linger and a 2 s snapshot wait.
func NewTables(conns *Conns) *Tables {
	return &Tables{conns: conns, Linger: 30 * time.Second, SnapshotWait: 2 * time.Second, watches: map[watchKey]*sharedWatch{}}
}

// Register wires table.subscribe into s.
func (t *Tables) Register(s *Server) { s.Subs[MethodTableSubscribe] = t.subscribe }

type watchKey struct {
	context   string
	resource  ResourceRef
	namespace string // "" for every namespace or a cluster-scoped kind
	selector  string
}

func (t *Tables) subscribe(ctx context.Context, params json.RawMessage) (func(Emit), error) {
	var p TableSubscribeParams
	if err := json.Unmarshal(params, &p); err != nil || len(p.Contexts) == 0 || p.Resource.Version == "" || p.Resource.Plural == "" {
		return nil, &Error{Code: CodeInvalidParams, Message: "table.subscribe needs contexts and a resource"}
	}
	ns := p.Namespace
	if p.AllNamespaces {
		ns = ""
	}
	return func(emit Emit) {
		s := &tableSub{ctx: ctx, emit: emit, contexts: p.Contexts, answered: map[string]bool{}, rows: map[string]map[string]Row{}}
		stop := t.conns.Listen(s.state)
		var ws []*sharedWatch
		for _, name := range p.Contexts {
			w := t.acquire(watchKey{name, p.Resource, ns, p.LabelSelector})
			w.join(s)
			ws = append(ws, w)
		}
		timer := time.AfterFunc(t.SnapshotWait, s.flush)
		<-ctx.Done()
		timer.Stop()
		s.mu.Lock()
		s.closed = true
		s.mu.Unlock()
		stop()
		for _, w := range ws {
			w.leave(s)
			t.release(w)
		}
	}, nil
}

// acquire returns the shared watch of k, starting it if needed.
func (t *Tables) acquire(k watchKey) *sharedWatch {
	t.mu.Lock()
	defer t.mu.Unlock()
	w := t.watches[k]
	if w == nil {
		ctx, cancel := context.WithCancel(context.Background())
		w = &sharedWatch{key: k, conns: t.conns, cancel: cancel, subs: map[*tableSub]bool{}}
		t.watches[k] = w
		go w.run(ctx)
	}
	w.refs++
	w.gen++ // voids a pending linger
	return w
}

// release drops one reference; the last one stops the watch after Linger
// unless it is acquired again meanwhile.
func (t *Tables) release(w *sharedWatch) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if w.refs--; w.refs > 0 {
		return
	}
	w.gen++
	gen := w.gen
	time.AfterFunc(t.Linger, func() {
		t.mu.Lock()
		defer t.mu.Unlock()
		if w.gen == gen {
			delete(t.watches, w.key)
			w.cancel()
		}
	})
}

// sharedWatch lists and watches one (context, resource, namespace, selector)
// and fans its rows out to the subscriptions that joined it.
type sharedWatch struct {
	key       watchKey
	conns     *Conns
	cancel    context.CancelFunc
	refs, gen int // guarded by Tables.mu

	// Only the run goroutine touches these.
	hc   *http.Client
	base string
	rv   string

	mu      sync.Mutex // guards the rest; held while calling subscribers, so they see events in order
	subs    map[*tableSub]bool
	listed  bool
	denied  string // the last 403 message, until a list succeeds
	columns []Column
	rows    map[string]Row // by uid, cells in columns order
}

func (w *sharedWatch) join(s *tableSub) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.subs[s] = true
	if w.listed {
		s.reset(w.key.context, w.columns, w.rows)
	}
	if w.denied != "" {
		s.forbidden(w.key.context, w.denied)
	}
}

func (w *sharedWatch) leave(s *tableSub) {
	w.mu.Lock()
	delete(w.subs, s)
	w.mu.Unlock()
}

// run lists, then watches from the list's resource version until ctx is
// done: it re-lists on 410 Gone and retries failures with backoff, reporting
// them as context states.
func (w *sharedWatch) run(ctx context.Context) {
	backoff, relist := minBackoff, true
	for {
		var err error
		if relist {
			if err = w.list(ctx); err == nil {
				relist, backoff = false, minBackoff
			}
		}
		if err == nil {
			if err = w.watch(ctx, &backoff); errors.Is(err, errGone) {
				relist = true
				continue
			}
		}
		if ctx.Err() != nil {
			return
		}
		if apierrors.IsForbidden(err) {
			w.deny(err)
			backoff = maxBackoff // never retry a refused kind in a tight loop
		} else if err != nil && !errors.Is(err, errReported) {
			w.conns.state(w.key.context, classify(err), redact(err.Error()), "")
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, maxBackoff)
	}
}

// deny tells every subscriber that the API server refused this kind (403).
// The context's state is left alone.
func (w *sharedWatch) deny(err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	msg := redact(err.Error())
	if msg == w.denied {
		return // already told
	}
	w.denied = msg
	for s := range w.subs {
		s.forbidden(w.key.context, w.denied)
	}
}

func (w *sharedWatch) list(ctx context.Context) error {
	cn, err := w.conns.Get(ctx, w.key.context)
	if err != nil {
		return errReported
	}
	cfg := rest.CopyConfig(cn.Config)
	cfg.Timeout = 0 // a watch streams for as long as it lasts
	if w.hc, err = rest.HTTPClientFor(cfg); err != nil {
		return err
	}
	w.base = strings.TrimSuffix(cfg.Host, "/") + w.path()
	lctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	body, err := w.open(lctx, url.Values{})
	if err != nil {
		return err
	}
	defer body.Close()
	var tbl metav1.Table
	if err := json.NewDecoder(body).Decode(&tbl); err != nil {
		return err
	}
	cols := make([]Column, len(tbl.ColumnDefinitions))
	for i, c := range tbl.ColumnDefinitions {
		cols[i] = Column{Name: c.Name, Priority: int(c.Priority)}
	}
	rows := map[string]Row{}
	for _, r := range tbl.Rows {
		row, _ := w.row(r)
		rows[row.UID] = row
	}
	w.rv = tbl.ResourceVersion
	w.conns.state(w.key.context, StateLive, "", "")
	w.mu.Lock()
	defer w.mu.Unlock()
	w.listed, w.columns, w.rows, w.denied = true, cols, rows, ""
	for s := range w.subs {
		s.reset(w.key.context, cols, rows)
	}
	return nil
}

// watch streams events from w.rv until the stream ends (nil), the resource
// version expires (errGone) or it fails.
func (w *sharedWatch) watch(ctx context.Context, backoff *time.Duration) error {
	body, err := w.open(ctx, url.Values{"watch": {"1"}, "resourceVersion": {w.rv}})
	if err != nil {
		return err
	}
	defer body.Close()
	w.conns.state(w.key.context, StateLive, "", "")
	dec := json.NewDecoder(body)
	for {
		var ev struct {
			Type   string          `json:"type"`
			Object json.RawMessage `json:"object"`
		}
		if err := dec.Decode(&ev); err == io.EOF {
			return nil
		} else if err != nil {
			return err
		}
		if ev.Type == "ERROR" {
			var st metav1.Status
			_ = json.Unmarshal(ev.Object, &st)
			return statusErr(st)
		}
		var tbl metav1.Table
		if err := json.Unmarshal(ev.Object, &tbl); err != nil {
			return err
		}
		*backoff = minBackoff
		w.mu.Lock()
		for _, r := range tbl.Rows {
			row, rv := w.row(r)
			w.rv = rv
			switch ev.Type {
			case "ADDED", "MODIFIED":
				w.rows[row.UID] = row
				for s := range w.subs {
					s.upsert(w.key.context, w.columns, row)
				}
			case "DELETED":
				delete(w.rows, row.UID)
				for s := range w.subs {
					s.remove(row)
				}
			}
		}
		w.mu.Unlock()
	}
}

// open sends a Table GET to the resource's collection.
func (w *sharedWatch) open(ctx context.Context, q url.Values) (io.ReadCloser, error) {
	q.Set("includeObject", "Object")
	if w.key.selector != "" {
		q.Set("labelSelector", w.key.selector)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, w.base+"?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", tableAccept)
	resp, err := w.hc.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		var st metav1.Status
		_ = json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&st)
		if st.Code == 0 {
			st.Code = int32(resp.StatusCode)
		}
		if st.Message == "" {
			st.Message = resp.Status
		}
		return nil, statusErr(st)
	}
	return resp.Body, nil
}

func statusErr(st metav1.Status) error {
	if st.Code == http.StatusGone {
		return errGone
	}
	return &apierrors.StatusError{ErrStatus: st}
}

func (w *sharedWatch) path() string {
	r := w.key.resource
	p := "/api/" + r.Version
	if r.Group != "" {
		p = "/apis/" + r.Group + "/" + r.Version
	}
	if w.key.namespace != "" {
		p += "/namespaces/" + url.PathEscape(w.key.namespace)
	}
	return p + "/" + r.Plural
}

// row turns a Table row, whose object is the full object, into a Row and
// the object's resource version.
func (w *sharedWatch) row(r metav1.TableRow) (Row, string) {
	obj := &unstructured.Unstructured{}
	_ = json.Unmarshal(r.Object.Raw, &obj.Object)
	cells := make([]string, len(r.Cells))
	for i, c := range r.Cells {
		if c != nil {
			cells[i] = fmt.Sprint(c)
		}
	}
	return Row{
		Context:   w.key.context,
		Namespace: obj.GetNamespace(),
		Name:      obj.GetName(),
		UID:       string(obj.GetUID()),
		Cells:     cells,
		Health:    HealthOf(obj, time.Now()),
	}, obj.GetResourceVersion()
}

// tableSub is one table.subscribe: it merges the rows of its contexts'
// watches under the columns of the first context that answered.
type tableSub struct {
	ctx      context.Context
	emit     Emit
	contexts []string

	mu       sync.Mutex
	closed   bool
	columns  []Column                  // nil until a context answers
	answered map[string]bool           // contexts that answered or failed
	rows     map[string]map[string]Row // by context, then uid; cells in columns order
	sent     bool                      // snapshot sent
}

// send emits under s.mu unless the subscription is over.
func (s *tableSub) send(event string, data any) {
	if !s.closed && s.ctx.Err() == nil {
		s.emit(event, data)
	}
}

func (s *tableSub) state(ch ContextStateChange) {
	if !slices.Contains(s.contexts, ch.Context) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.send(EventContextState, ch)
	if ch.State == StateUnreachable || ch.State == StateUnauthorized {
		s.answered[ch.Context] = true
		s.maybeSnapshot()
	}
}

// forbidden reports a 403 on this kind for one context: the table has no rows
// from it, and its state is untouched.
func (s *tableSub) forbidden(name, msg string) {
	if !slices.Contains(s.contexts, name) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.send(EventForbidden, Forbidden{Context: name, Message: msg})
	s.answered[name] = true
	s.maybeSnapshot()
}

// reset replaces a context's rows after a (re-)list.
func (s *tableSub) reset(name string, cols []Column, rows map[string]Row) {
	s.mu.Lock()
	defer s.mu.Unlock()
	resend := s.columns == nil && s.sent
	if s.columns == nil {
		s.columns = cols
	}
	old := s.rows[name]
	cur := make(map[string]Row, len(rows))
	for uid, r := range rows {
		cur[uid] = s.fit(cols, r)
	}
	s.rows[name] = cur
	s.answered[name] = true
	switch {
	case !s.sent:
		s.maybeSnapshot()
	case resend: // the snapshot went out without columns: send it again with them
		s.snapshot()
	default:
		for _, r := range sortedRows(cur) {
			s.send(EventUpsert, Upsert{Row: r})
		}
		for uid, r := range old {
			if _, ok := cur[uid]; !ok {
				s.send(EventDelete, Delete{Context: r.Context, Namespace: r.Namespace, Name: r.Name, UID: r.UID})
			}
		}
	}
}

func (s *tableSub) upsert(name string, cols []Column, r Row) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r = s.fit(cols, r)
	if s.rows[name] == nil {
		s.rows[name] = map[string]Row{}
	}
	s.rows[name][r.UID] = r
	if s.sent {
		s.send(EventUpsert, Upsert{Row: r})
	}
}

func (s *tableSub) remove(r Row) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.rows[r.Context], r.UID)
	if s.sent {
		s.send(EventDelete, Delete{Context: r.Context, Namespace: r.Namespace, Name: r.Name, UID: r.UID})
	}
}

// fit reorders a row's cells from cols to the subscription's columns by
// name; a column cols lacks is "".
func (s *tableSub) fit(cols []Column, r Row) Row {
	idx := make(map[string]int, len(cols))
	for i, c := range cols {
		idx[c.Name] = i
	}
	cells := make([]string, len(s.columns))
	for j, c := range s.columns {
		if i, ok := idx[c.Name]; ok && i < len(r.Cells) {
			cells[j] = r.Cells[i]
		}
	}
	r.Cells = cells
	return r
}

func (s *tableSub) maybeSnapshot() {
	for _, c := range s.contexts {
		if !s.answered[c] {
			return
		}
	}
	if !s.sent {
		s.snapshot()
	}
}

// flush sends the snapshot with what has answered when SnapshotWait is up.
func (s *tableSub) flush() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.sent {
		s.snapshot()
	}
}

func (s *tableSub) snapshot() {
	rows := []Row{}
	for _, m := range s.rows {
		for _, r := range m {
			rows = append(rows, r)
		}
	}
	cols := s.columns
	if cols == nil {
		cols = []Column{}
	}
	s.send(EventSnapshot, Snapshot{Columns: cols, Rows: sortRows(rows)})
	s.sent = true
}

// sortedRows returns a map's rows ordered by context, namespace and name.
func sortedRows(m map[string]Row) []Row {
	rows := make([]Row, 0, len(m))
	for _, r := range m {
		rows = append(rows, r)
	}
	return sortRows(rows)
}

func sortRows(rows []Row) []Row {
	slices.SortFunc(rows, func(a, b Row) int {
		return cmp.Or(strings.Compare(a.Context, b.Context), strings.Compare(a.Namespace, b.Namespace), strings.Compare(a.Name, b.Name))
	})
	return rows
}
