package engine

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/fsnotify/fsnotify"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// Kubeconfig is the merged kubeconfig the engine reads, from the KUBECONFIG
// list (or ~/.kube/config) minus iris's own files.
type Kubeconfig struct {
	paths []string
}

// NewKubeconfig resolves the kubeconfig paths from the current environment.
func NewKubeconfig() *Kubeconfig {
	home, _ := os.UserHomeDir()
	var paths []string
	if env := os.Getenv("KUBECONFIG"); env != "" {
		paths = filepath.SplitList(env)
	} else {
		paths = []string{filepath.Join(home, ".kube", "config")}
	}
	irisDir := filepath.Join(home, ".local", "share", "iris", "claude") + string(filepath.Separator)
	k := &Kubeconfig{}
	for _, p := range paths {
		if p = filepath.Clean(p); p != "." && !strings.HasPrefix(p, irisDir) {
			k.paths = append(k.paths, p)
		}
	}
	return k
}

func (k *Kubeconfig) rules() *clientcmd.ClientConfigLoadingRules {
	return &clientcmd.ClientConfigLoadingRules{Precedence: k.paths}
}

// Contexts lists the merged contexts sorted by name.
func (k *Kubeconfig) Contexts() ([]ContextInfo, error) {
	cfg, err := k.rules().Load()
	if err != nil {
		return nil, err
	}
	list := []ContextInfo{}
	for name, c := range cfg.Contexts {
		list = append(list, ContextInfo{Name: name, Cluster: c.Cluster, User: c.AuthInfo, Namespace: c.Namespace, Current: name == cfg.CurrentContext})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	return list, nil
}

// RestConfig builds the REST config of one context. It never prompts.
func (k *Kubeconfig) RestConfig(name string) (*rest.Config, error) {
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(k.rules(), &clientcmd.ConfigOverrides{CurrentContext: name}).ClientConfig()
}

// Watch returns a channel that receives (coalesced) whenever a kubeconfig file
// changes, and is closed when ctx is done. It watches the parent directories
// so an editor's rename-replace is caught.
func (k *Kubeconfig) Watch(ctx context.Context) (<-chan struct{}, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	files := map[string]bool{}
	for _, p := range k.paths {
		files[p] = true
		_ = w.Add(filepath.Dir(p)) // a missing directory has nothing to watch yet
	}
	ch := make(chan struct{}, 1)
	go func() {
		defer close(ch)
		defer w.Close()
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-w.Events:
				if !ok {
					return
				}
				if files[filepath.Clean(ev.Name)] {
					select {
					case ch <- struct{}{}:
					default:
					}
				}
			case <-w.Errors:
			}
		}
	}()
	return ch, nil
}

// Register wires contexts.list and contexts.watch into s.
func (k *Kubeconfig) Register(s *Server) {
	s.Handlers[MethodContextsList] = func(context.Context, json.RawMessage) (any, error) {
		list, err := k.Contexts()
		return ContextsListResult{Contexts: list}, err
	}
	s.Subs[MethodContextsWatch] = func(ctx context.Context, _ json.RawMessage) (func(Emit), error) {
		ch, err := k.Watch(ctx)
		if err != nil {
			return nil, err
		}
		return func(emit Emit) {
			for range ch {
				if list, err := k.Contexts(); err == nil {
					emit(EventContextsChanged, ContextsChanged{Contexts: list})
				}
			}
		}, nil
	}
}
