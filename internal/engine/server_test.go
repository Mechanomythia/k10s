package engine

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// syncBuf is a goroutine-safe buffer for the server's output.
type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) lines() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.Split(strings.TrimSpace(s.b.String()), "\n")
}

// serveAll runs the server over the given input lines until EOF.
func serveAll(t *testing.T, s *Server, input ...string) []string {
	t.Helper()
	var out syncBuf
	if err := s.Serve(context.Background(), strings.NewReader(strings.Join(input, "\n")+"\n"), &out); err != nil {
		t.Fatal(err)
	}
	return out.lines()
}

func TestRequestResponse(t *testing.T) {
	s := New()
	s.Handlers[MethodContextsList] = func(context.Context, json.RawMessage) (any, error) {
		return ContextsListResult{Contexts: []ContextInfo{{Name: "prod"}}}, nil
	}
	got := serveAll(t, s, `{"id":"1","method":"contexts.list"}`)
	want := `{"id":"1","result":{"contexts":[{"name":"prod","cluster":"","user":"","namespace":"","current":false}]}}`
	if len(got) != 1 || got[0] != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestErrors(t *testing.T) {
	tests := []struct{ name, in, code string }{
		{"unknown method", `{"id":"1","method":"nope"}`, CodeUnknownMethod},
		{"stub", `{"id":"1","method":"contexts.list"}`, CodeNotImplemented},
		{"stub subscription", `{"id":"1","method":"table.subscribe","params":{}}`, CodeNotImplemented},
		{"bad unsubscribe params", `{"id":"1","method":"unsubscribe","params":"x"}`, CodeInvalidParams},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// A malformed line before the request must not break the stream.
			got := serveAll(t, New(), `not json`, tt.in)
			var r Response
			if err := json.Unmarshal([]byte(got[0]), &r); err != nil {
				t.Fatal(err)
			}
			if len(got) != 1 || r.ID != "1" || r.Error == nil || r.Error.Code != tt.code {
				t.Errorf("got %q, want error %s", got, tt.code)
			}
		})
	}
}

// pipeServer runs Serve over pipes so a test can talk to it interactively.
type pipeServer struct {
	in   io.WriteCloser
	out  *bufio.Scanner
	done chan error
}

func startPipe(t *testing.T, s *Server) *pipeServer {
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	p := &pipeServer{in: inW, out: bufio.NewScanner(outR), done: make(chan error, 1)}
	go func() { p.done <- s.Serve(context.Background(), inR, outW); outW.Close() }()
	t.Cleanup(func() { inW.Close() })
	return p
}

func (p *pipeServer) send(line string) { fmt.Fprintln(p.in, line) }

func (p *pipeServer) next(t *testing.T) string {
	t.Helper()
	if !p.out.Scan() {
		t.Fatal("output closed")
	}
	return p.out.Text()
}

func TestSubscribeUnsubscribe(t *testing.T) {
	s := New()
	cancelled := make(chan struct{})
	s.Subs[MethodLogsSubscribe] = func(ctx context.Context, _ json.RawMessage) (func(Emit), error) {
		return func(emit Emit) {
			emit(EventLines, Lines{Lines: []string{"a"}})
			<-ctx.Done()
			close(cancelled)
		}, nil
	}
	p := startPipe(t, s)
	p.send(`{"id":"1","method":"logs.subscribe","params":{}}`)
	if got := p.next(t); got != `{"id":"1","result":{"sub":"s1"}}` {
		t.Fatalf("response: %s", got)
	}
	if got := p.next(t); got != `{"sub":"s1","event":"lines","data":{"lines":["a"]}}` {
		t.Fatalf("event: %s", got)
	}
	p.send(`{"id":"2","method":"unsubscribe","params":{"sub":"s1"}}`)
	if got := p.next(t); got != `{"id":"2","result":{}}` {
		t.Fatalf("unsubscribe: %s", got)
	}
	waitClosed(t, cancelled)
	// Unsubscribing again is not an error.
	p.send(`{"id":"3","method":"unsubscribe","params":{"sub":"s1"}}`)
	if got := p.next(t); got != `{"id":"3","result":{}}` {
		t.Fatalf("second unsubscribe: %s", got)
	}
}

func waitClosed(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("subscription context was not cancelled")
	}
}

func TestEOFCancelsSubscriptions(t *testing.T) {
	s := New()
	var cancelled [2]chan struct{}
	for i := range cancelled {
		cancelled[i] = make(chan struct{})
	}
	started := make(chan struct{}, 2)
	n := 0
	s.Subs[MethodTableSubscribe] = func(ctx context.Context, _ json.RawMessage) (func(Emit), error) {
		done := cancelled[n]
		n++
		return func(Emit) { started <- struct{}{}; <-ctx.Done(); close(done) }, nil
	}
	p := startPipe(t, s)
	p.send(`{"id":"1","method":"table.subscribe","params":{}}`)
	p.next(t)
	p.send(`{"id":"2","method":"table.subscribe","params":{}}`)
	p.next(t)
	<-started
	<-started
	p.in.Close() // EOF
	for _, c := range cancelled {
		waitClosed(t, c)
	}
	select {
	case err := <-p.done:
		if err != nil {
			t.Error(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return after EOF")
	}
}

func TestConcurrentWritesDoNotInterleave(t *testing.T) {
	const subs, events = 8, 200
	s := New()
	big := strings.Repeat("x", 4096)
	s.Subs[MethodLogsSubscribe] = func(ctx context.Context, _ json.RawMessage) (func(Emit), error) {
		return func(emit Emit) {
			for i := 0; i < events; i++ {
				emit(EventLines, Lines{Lines: []string{big}})
			}
		}, nil
	}
	var in []string
	for i := 0; i < subs; i++ {
		in = append(in, fmt.Sprintf(`{"id":"%d","method":"logs.subscribe","params":{}}`, i))
	}
	// EOF cancels subscriptions, but their runs emit without checking ctx, so
	// every event is still written before Serve returns.
	lines := serveAll(t, s, in...)
	if len(lines) != subs+subs*events {
		t.Fatalf("got %d lines, want %d", len(lines), subs+subs*events)
	}
	for _, l := range lines {
		if !json.Valid([]byte(l)) {
			t.Fatalf("invalid line (interleaved write?): %.80s", l)
		}
	}
}
