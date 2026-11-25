package engine

import (
	"context"
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
}

// Conns holds the connections by context name. Nothing connects at start:
// Get does, on first use.
type Conns struct {
	kube *Kubeconfig
	// OnState, if set before first use, is called on every state transition
	// (connecting, then live, unreachable or unauthorized with the error).
	OnState func(ContextStateChange)

	mu    sync.Mutex
	conns map[string]*Conn
}

// NewConns returns Conns over the given kubeconfig.
func NewConns(kube *Kubeconfig) *Conns {
	return &Conns{kube: kube, conns: map[string]*Conn{}}
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
		if err != nil {
			c.mu.Unlock()
			return nil, fmt.Errorf("context %q: %s", name, redact(err.Error()))
		}
		cfg.Timeout = 15 * time.Second
		dc, err := discovery.NewDiscoveryClientForConfig(cfg)
		if err != nil {
			c.mu.Unlock()
			return nil, fmt.Errorf("context %q: %s", name, redact(err.Error()))
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
	c.state(name, StateConnecting, "")
	err := cn.Discovery.RESTClient().Get().AbsPath("/version").Do(ctx).Error()
	if err != nil {
		st := classify(err)
		msg := redact(err.Error())
		c.state(name, st, msg)
		return nil, errors.New(msg)
	}
	cn.live = true
	c.state(name, StateLive, "")
	return cn, nil
}

func (c *Conns) state(name string, st ContextState, msg string) {
	if c.OnState != nil {
		c.OnState(ContextStateChange{Context: name, State: st, Error: msg})
	}
}

// classify maps a probe error to a state: 401/403 and exec plugin failures
// are unauthorized, everything else (dial, timeout, TLS, ...) unreachable.
func classify(err error) ContextState {
	if apierrors.IsUnauthorized(err) || apierrors.IsForbidden(err) {
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
