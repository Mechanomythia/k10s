package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/rest"
)

// Conn is the lazily created connection of one context: one REST config and
// one discovery cache, kept until the kubeconfig changes.
type Conn struct {
	Name      string
	Config    *rest.Config
	Discovery discovery.CachedDiscoveryInterface

	probe sync.Mutex // serialises probing
	live  bool
	// Version is the cluster gitVersion from the /version probe, set once live.
	Version string
}

// Conns holds the connections by context name. Nothing connects at start:
// Get does, on first use.
type Conns struct {
	kube *Kubeconfig

	mu    sync.Mutex
	conns map[string]*Conn

	lmu       sync.Mutex // guards listeners, nextL and last; held while calling listeners
	listeners map[int]func(ContextStateChange)
	nextL     int
	last      map[string]ContextStateChange
}

// NewConns returns Conns over the given kubeconfig.
func NewConns(kube *Kubeconfig) *Conns {
	return &Conns{kube: kube, conns: map[string]*Conn{}, listeners: map[int]func(ContextStateChange){}, last: map[string]ContextStateChange{}}
}

// Listen calls fn with the last state of every context seen so far, then
// with every state transition (connecting, then live, unreachable or
// unauthorized with the error) until stop returns. fn runs under a lock: it
// must not block or call back into Conns.
func (c *Conns) Listen(fn func(ContextStateChange)) (stop func()) {
	c.lmu.Lock()
	defer c.lmu.Unlock()
	for _, st := range c.last {
		fn(st)
	}
	id := c.nextL
	c.nextL++
	c.listeners[id] = fn
	return func() {
		c.lmu.Lock()
		delete(c.listeners, id)
		c.lmu.Unlock()
	}
}

// Reset drops every cached connection; the next Get reconnects. Call it when
// kubeconfig changes.
func (c *Conns) Reset() {
	c.mu.Lock()
	c.conns = map[string]*Conn{}
	c.mu.Unlock()
}

// Get returns the connection of a context, probing /version unless it is
// already live. A failed probe returns the classified error and the next Get
// probes again.
func (c *Conns) Get(ctx context.Context, name string) (*Conn, error) {
	c.mu.Lock()
	cn := c.conns[name]
	if cn == nil {
		cfg, err := c.kube.RestConfig(name)
		var dc *discovery.DiscoveryClient
		if err == nil {
			cfg.Timeout = 15 * time.Second
			dc, err = discovery.NewDiscoveryClientForConfig(cfg)
		}
		if err != nil {
			c.mu.Unlock()
			msg := fmt.Sprintf("context %q: %s", name, redact(err.Error()))
			c.state(name, StateUnreachable, msg, "")
			return nil, errors.New(msg)
		}
		cn = &Conn{Name: name, Config: cfg, Discovery: memory.NewMemCacheClient(dc)}
		c.conns[name] = cn
	}
	c.mu.Unlock()

	cn.probe.Lock()
	defer cn.probe.Unlock()
	if cn.live {
		return cn, nil
	}
	c.state(name, StateConnecting, "", "")
	raw, err := cn.Discovery.RESTClient().Get().AbsPath("/version").Do(ctx).Raw()
	if err != nil {
		st := classify(err)
		if apierrors.IsForbidden(err) { // the probe itself was refused
			st = StateUnauthorized
		}
		msg := redact(err.Error())
		c.state(name, st, msg, "")
		return nil, errors.New(msg)
	}
	var v struct {
		GitVersion string `json:"gitVersion"`
	}
	_ = json.Unmarshal(raw, &v)
	cn.live = true
	cn.Version = v.GitVersion
	c.state(name, StateLive, "", v.GitVersion)
	return cn, nil
}

// state broadcasts a transition to the listeners, unless it repeats the
// context's last state.
func (c *Conns) state(name string, st ContextState, msg, version string) {
	c.lmu.Lock()
	defer c.lmu.Unlock()
	if st == StateLive && version == "" {
		version = c.last[name].Version // table watches re-report live without it
	}
	ch := ContextStateChange{Context: name, State: st, Error: msg, Version: version}
	if c.last[name] == ch {
		return
	}
	c.last[name] = ch
	for _, fn := range c.listeners {
		fn(ch)
	}
}

// classify maps a probe error to a state: 401 and exec plugin failures are
// unauthorized, everything else (dial, timeout, TLS, ...) unreachable. A 403
// is per kind (ADR 0004), so classify never maps it; only Get's /version
// probe treats a refusal as unauthorized.
func classify(err error) ContextState {
	if apierrors.IsUnauthorized(err) {
		return StateUnauthorized
	}
	if m := err.Error(); strings.Contains(m, "getting credentials") || strings.Contains(m, "exec:") {
		return StateUnauthorized
	}
	return StateUnreachable
}

var (
	bearerRe = regexp.MustCompile(`(?i)(bearer\s+)\S+`)
	tokenRe  = regexp.MustCompile(`[A-Za-z0-9._~+/=-]{32,}`)
)

// redact hides anything in an error message that looks like a credential.
func redact(s string) string {
	return tokenRe.ReplaceAllString(bearerRe.ReplaceAllString(s, "${1}[redacted]"), "[redacted]")
}
