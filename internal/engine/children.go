package engine

import (
	"context"
	"encoding/json"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// children answers object.children: the table.subscribe filters that list
// the parent's children. The kind is decided from the resource ref alone.
func (d *Detail) children(ctx context.Context, raw json.RawMessage) (any, error) {
	var p ObjectParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	if err := p.check(); err != nil {
		return nil, err
	}
	var kind string // the parent kind, by group and plural
	switch k := p.Resource.Group + "/" + p.Resource.Plural; k {
	case "apps/deployments", "apps/statefulsets", "apps/daemonsets", "apps/replicasets", "batch/jobs",
		"/services", "batch/cronjobs", "/nodes":
		kind = k
	default:
		return nil, invalid("%s has no children to list", p.Resource.Plural)
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
	obj, err := dc.Resource(gvr).Namespace(p.Namespace).Get(ctx, p.Name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	res := ChildrenResult{Resource: ResourceRef{Version: "v1", Plural: "pods"}, Namespace: obj.GetNamespace()}
	switch kind {
	case "/nodes":
		res.FieldSelector = fields.OneTermEqualSelector("spec.nodeName", obj.GetName()).String()
	case "batch/cronjobs":
		res.Resource = ResourceRef{Group: "batch", Version: "v1", Plural: "jobs"}
		res.OwnerUID = string(obj.GetUID())
	case "/services":
		m, _, _ := unstructured.NestedStringMap(obj.Object, "spec", "selector")
		if len(m) == 0 {
			res.None = true
		} else {
			res.LabelSelector = labels.SelectorFromSet(m).String()
		}
	default:
		m, ok, _ := unstructured.NestedMap(obj.Object, "spec", "selector")
		var ls metav1.LabelSelector
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(m, &ls); err != nil {
			return nil, err
		}
		sel, err := metav1.LabelSelectorAsSelector(&ls)
		if err != nil {
			return nil, err
		}
		if !ok || sel.Empty() {
			res.None = true
		} else {
			res.LabelSelector = sel.String()
		}
	}
	return res, nil
}
