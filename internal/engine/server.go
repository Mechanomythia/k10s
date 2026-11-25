package engine

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"strconv"
	"sync"
)

// Handler answers one request. The returned value is marshalled into the
// result; an *Error is sent as is, any other error as CodeInternal.
type Handler func(ctx context.Context, params json.RawMessage) (any, error)

// Emit sends one event of a subscription. It is safe for concurrent use.
type Emit func(event string, data any)

// SubHandler starts a subscription. It validates params and returns quickly:
// an error becomes the error response. On success the server sends the
// {sub} response and then calls run, which emits events until ctx is done
// (unsubscribe or EOF) or it returns.
type SubHandler func(ctx context.Context, params json.RawMessage) (run func(emit Emit), err error)

// Server dispatches ndjson requests to handlers. Later tickets replace the
// stubs by assigning to Handlers and Subs before Serve.
type Server struct {
	Handlers map[string]Handler
	Subs     map[string]SubHandler

	mu      sync.Mutex // guards w, nextSub and cancels
	w       io.Writer
	nextSub int
	cancels map[string]context.CancelFunc
}

// New returns a Server with every protocol method stubbed as not_implemented.
func New() *Server {
	s := &Server{Handlers: map[string]Handler{}, Subs: map[string]SubHandler{}}
	for _, m := range []string{MethodContextsList, MethodResourcesList, MethodObjectYAML, MethodObjectDescribe, MethodObjectEvents} {
		s.Handlers[m] = func(context.Context, json.RawMessage) (any, error) { return nil, notImplemented() }
	}
	for _, m := range []string{MethodContextsWatch, MethodTableSubscribe, MethodLogsSubscribe} {
		s.Subs[m] = func(context.Context, json.RawMessage) (func(Emit), error) { return nil, notImplemented() }
	}
	return s
}

func notImplemented() *Error {
	return &Error{Code: CodeNotImplemented, Message: "not implemented"}
}

// Serve reads requests from r until EOF or ctx is done, writing responses and
// events to w, one JSON object per line. On return all subscriptions are
// cancelled and all handlers have finished.
func (s *Server) Serve(ctx context.Context, r io.Reader, w io.Writer) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	s.w = w
	s.cancels = map[string]context.CancelFunc{}
	var wg sync.WaitGroup
	defer wg.Wait()
	defer cancel() // runs before wg.Wait: EOF cancels every subscription

	br := bufio.NewReader(r)
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			var req Request
			if jerr := json.Unmarshal(line, &req); jerr != nil || req.ID == "" {
				// Never log the line itself: it could carry anything.
				log.Printf("engine: dropped malformed request line (%d bytes)", len(line))
			} else {
				wg.Add(1)
				go func() {
					defer wg.Done()
					s.dispatch(ctx, req, &wg)
				}()
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func (s *Server) dispatch(ctx context.Context, req Request, wg *sync.WaitGroup) {
	switch {
	case req.Method == MethodUnsubscribe:
		var p UnsubscribeParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			s.respond(req.ID, nil, &Error{Code: CodeInvalidParams, Message: "params: " + err.Error()})
			return
		}
		s.mu.Lock()
		cancel := s.cancels[p.Sub]
		delete(s.cancels, p.Sub)
		s.mu.Unlock()
		if cancel != nil {
			cancel()
		}
		s.respond(req.ID, Empty{}, nil)
	case s.Subs[req.Method] != nil:
		subCtx, cancel := context.WithCancel(ctx)
		run, err := s.Subs[req.Method](subCtx, req.Params)
		if err != nil {
			cancel()
			s.respond(req.ID, nil, err)
			return
		}
		s.mu.Lock()
		s.nextSub++
		sub := "s" + strconv.Itoa(s.nextSub)
		s.cancels[sub] = cancel
		s.mu.Unlock()
		s.respond(req.ID, SubscribeResult{Sub: sub}, nil)
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer cancel()
			if run != nil {
				run(func(event string, data any) { s.emit(sub, event, data) })
			}
			<-subCtx.Done()
		}()
	case s.Handlers[req.Method] != nil:
		res, err := s.Handlers[req.Method](ctx, req.Params)
		s.respond(req.ID, res, err)
	default:
		s.respond(req.ID, nil, &Error{Code: CodeUnknownMethod, Message: "unknown method " + req.Method})
	}
}

func (s *Server) respond(id string, result any, err error) {
	resp := Response{ID: id}
	if err != nil {
		var e *Error
		if !errors.As(err, &e) {
			e = &Error{Code: CodeInternal, Message: err.Error()}
		}
		resp.Error = e
	} else if raw, merr := json.Marshal(result); merr != nil {
		resp.Error = &Error{Code: CodeInternal, Message: "marshal result: " + merr.Error()}
	} else {
		resp.Result = raw
	}
	s.write(resp)
}

func (s *Server) emit(sub, event string, data any) {
	raw, err := json.Marshal(data)
	if err != nil {
		log.Printf("engine: marshal %s event: %v", event, err)
		return
	}
	s.write(Event{Sub: sub, Event: event, Data: raw})
}

// write sends v as one line, holding the lock so lines never interleave.
func (s *Server) write(v any) {
	line, err := json.Marshal(v)
	if err != nil {
		log.Printf("engine: marshal: %v", err)
		return
	}
	line = append(line, '\n')
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.w.Write(line); err != nil {
		log.Printf("engine: %s", fmt.Sprint(err))
	}
}
