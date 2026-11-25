package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/kubectl/pkg/describe"
	"sigs.k8s.io/yaml"
)

// Detail serves object.yaml, object.describe and object.events, and logs.subscribe.
type Detail struct {
	Conns *Conns
	// Dynamic and Clientset build clients from a context's config; tests
	// replace them with fakes.
	Dynamic   func(*rest.Config) (dynamic.Interface, error)
	Clientset func(*rest.Config) (kubernetes.Interface, error)

	// Open opens one container's log stream; tests replace it.
	Open func(ctx context.Context, cs kubernetes.Interface, p LogsSubscribeParams, container string) (io.ReadCloser, error)
}

// Register wires the four methods into s.
func (d *Detail) Register(s *Server) {
	s.Handlers[MethodObjectYAML] = d.yaml
	s.Handlers[MethodObjectDescribe] = d.describe
	s.Handlers[MethodObjectEvents] = d.events
	s.Subs[MethodLogsSubscribe] = d.logs
}

func invalid(format string, a ...any) error {
	return &Error{Code: CodeInvalidParams, Message: fmt.Sprintf(format, a...)}
}

func decode(raw json.RawMessage, v any) error {
	if err := json.Unmarshal(raw, v); err != nil {
		return invalid("params: %v", err)
	}
	return nil
}

func (d *Detail) dynamic(cfg *rest.Config) (dynamic.Interface, error) {
	if d.Dynamic != nil {
		return d.Dynamic(cfg)
	}
	return dynamic.NewForConfig(cfg)
}

func (d *Detail) clientset(cfg *rest.Config) (kubernetes.Interface, error) {
	if d.Clientset != nil {
		return d.Clientset(cfg)
	}
	return kubernetes.NewForConfig(cfg)
}

func (p ObjectParams) check() error {
	if p.Context == "" || p.Name == "" || p.Resource.Version == "" || p.Resource.Plural == "" {
		return invalid("context, resource.version, resource.plural and name are required")
	}
	return nil
}

func (d *Detail) yaml(ctx context.Context, raw json.RawMessage) (any, error) {
	var p ObjectParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	if err := p.check(); err != nil {
		return nil, err
	}
	cn, err := d.Conns.Get(ctx, p.Context)
	if err != nil {
		return nil, err
	}
	dc, err := d.dynamic(cn.Config)
	if err != nil {
		return nil, err
	}
	gvr := schema.GroupVersionResource{Group: p.Resource.Group, Version: p.Resource.Version, Resource: p.Resource.Plural}
	var ri dynamic.ResourceInterface = dc.Resource(gvr)
	if p.Namespace != "" {
		ri = dc.Resource(gvr).Namespace(p.Namespace)
	}
	obj, err := ri.Get(ctx, p.Name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	unstructured.RemoveNestedField(obj.Object, "metadata", "managedFields")
	out, err := yaml.Marshal(obj.Object)
	if err != nil {
		return nil, err
	}
	return TextResult{Text: string(out)}, nil
}

func (d *Detail) describe(ctx context.Context, raw json.RawMessage) (any, error) {
	var p ObjectParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	if err := p.check(); err != nil {
		return nil, err
	}
	cn, err := d.Conns.Get(ctx, p.Context)
	if err != nil {
		return nil, err
	}
	mapper := restmapper.NewDeferredDiscoveryRESTMapper(cn.Discovery)
	gvr := schema.GroupVersionResource{Group: p.Resource.Group, Version: p.Resource.Version, Resource: p.Resource.Plural}
	gvk, err := mapper.KindFor(gvr)
	if err != nil {
		return nil, err
	}
	mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return nil, err
	}
	desc, ok := describe.DescriberFor(gvk.GroupKind(), cn.Config)
	if !ok {
		if desc, ok = describe.GenericDescriberFor(mapping, cn.Config); !ok {
			return nil, fmt.Errorf("no describer for %s", gvk)
		}
	}
	text, err := desc.Describe(p.Namespace, p.Name, describe.DescriberSettings{ShowEvents: true})
	if err != nil {
		return nil, err
	}
	return TextResult{Text: text}, nil
}

func (d *Detail) events(ctx context.Context, raw json.RawMessage) (any, error) {
	var p ObjectEventsParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	if p.Context == "" || p.UID == "" {
		return nil, invalid("context and uid are required")
	}
	cn, err := d.Conns.Get(ctx, p.Context)
	if err != nil {
		return nil, err
	}
	cs, err := d.clientset(cn.Config)
	if err != nil {
		return nil, err
	}
	// An empty namespace lists every namespace, for cluster-scoped objects.
	list, err := cs.CoreV1().Events(p.Namespace).List(ctx, metav1.ListOptions{FieldSelector: "involvedObject.uid=" + p.UID})
	if err != nil {
		return nil, err
	}
	type item struct {
		at time.Time
		ev ObjectEvent
	}
	var items []item
	for _, e := range list.Items {
		if string(e.InvolvedObject.UID) != p.UID { // fakes ignore field selectors
			continue
		}
		at := e.LastTimestamp.Time
		if at.IsZero() {
			at = e.EventTime.Time
		}
		if at.IsZero() {
			at = e.CreationTimestamp.Time
		}
		count := int(e.Count)
		if count == 0 && e.Series != nil {
			count = int(e.Series.Count)
		}
		items = append(items, item{at, ObjectEvent{Type: e.Type, Reason: e.Reason, Message: e.Message, Count: count, LastSeen: at.UTC().Format(time.RFC3339)}})
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].at.After(items[j].at) })
	res := ObjectEventsResult{Events: []ObjectEvent{}}
	for _, it := range items {
		res.Events = append(res.Events, it.ev)
	}
	return res, nil
}
