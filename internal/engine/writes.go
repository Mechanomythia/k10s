package engine

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/yaml"
)

// mapAPIErrors turns API server refusals into protocol errors carrying the
// server's message; anything else passes through.
func mapAPIErrors(h Handler) Handler {
	return func(ctx context.Context, raw json.RawMessage) (any, error) {
		res, err := h(ctx, raw)
		var e *Error
		if err == nil || errors.As(err, &e) {
			return res, err
		}
		switch {
		case apierrors.IsForbidden(err):
			err = &Error{Code: CodeForbidden, Message: redact(err.Error())}
		case apierrors.IsConflict(err):
			err = &Error{Code: CodeConflict, Message: redact(err.Error())}
		}
		return res, err
	}
}

// target resolves the one object a write names; with kinds, only those apps
// resources are allowed. The resource must be served as named (so group,
// version and plural cannot steer the request path elsewhere) and the
// namespace must be given exactly when the kind is namespaced, so a write can
// never become a request on a collection.
func (d *Detail) target(ctx context.Context, p ObjectParams, kinds ...string) (dynamic.ResourceInterface, metav1.APIResource, error) {
	var none metav1.APIResource
	if err := p.check(); err != nil {
		return nil, none, err
	}
	if len(kinds) > 0 && (p.Resource.Group != "apps" || !slices.Contains(kinds, p.Resource.Plural)) {
		return nil, none, invalid("%s does not support this action", p.Resource.Plural)
	}
	cn, err := d.Conns.Get(ctx, p.Context)
	if err != nil {
		return nil, none, err
	}
	gv := p.Resource.Version
	if p.Resource.Group != "" {
		gv = p.Resource.Group + "/" + p.Resource.Version
	}
	list, err := cn.Discovery.ServerResourcesForGroupVersion(gv)
	if err != nil {
		return nil, none, err
	}
	i := slices.IndexFunc(list.APIResources, func(r metav1.APIResource) bool { return r.Name == p.Resource.Plural })
	if i < 0 {
		return nil, none, invalid("%s is not served as %s", p.Resource.Plural, gv)
	}
	res := list.APIResources[i]
	if res.Namespaced && p.Namespace == "" {
		return nil, none, invalid("namespace is required for %s", p.Resource.Plural)
	}
	if !res.Namespaced && p.Namespace != "" {
		return nil, none, invalid("%s is not namespaced", p.Resource.Plural)
	}
	dc, err := d.dynamic(cn.Config)
	if err != nil {
		return nil, none, err
	}
	gvr := schema.GroupVersionResource{Group: p.Resource.Group, Version: p.Resource.Version, Resource: p.Resource.Plural}
	if res.Namespaced {
		return dc.Resource(gvr).Namespace(p.Namespace), res, nil
	}
	return dc.Resource(gvr), res, nil
}

func (d *Detail) delete(ctx context.Context, raw json.RawMessage) (any, error) {
	var p ObjectParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	ri, _, err := d.target(ctx, p)
	if err != nil {
		return nil, err
	}
	bg := metav1.DeletePropagationBackground
	if err := ri.Delete(ctx, p.Name, metav1.DeleteOptions{PropagationPolicy: &bg}); err != nil {
		return nil, err
	}
	return Empty{}, nil
}

func (d *Detail) scale(ctx context.Context, raw json.RawMessage) (any, error) {
	var p ScaleParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	if p.Replicas < 0 {
		return nil, invalid("replicas must be 0 or more")
	}
	ri, _, err := d.target(ctx, ObjectParams{p.Context, p.Resource, p.Namespace, p.Name}, "deployments", "statefulsets", "replicasets")
	if err != nil {
		return nil, err
	}
	sc, err := ri.Get(ctx, p.Name, metav1.GetOptions{}, "scale")
	if err != nil {
		return nil, err
	}
	if err := unstructured.SetNestedField(sc.Object, int64(p.Replicas), "spec", "replicas"); err != nil {
		return nil, err
	}
	if _, err := ri.Update(ctx, sc, metav1.UpdateOptions{}, "scale"); err != nil {
		return nil, err
	}
	return Empty{}, nil
}

func (d *Detail) restart(ctx context.Context, raw json.RawMessage) (any, error) {
	var p ObjectParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	ri, _, err := d.target(ctx, p, "deployments", "statefulsets", "daemonsets")
	if err != nil {
		return nil, err
	}
	patch, err := json.Marshal(map[string]any{"spec": map[string]any{"template": map[string]any{"metadata": map[string]any{
		"annotations": map[string]string{"kubectl.kubernetes.io/restartedAt": time.Now().UTC().Format(time.RFC3339)}}}}})
	if err != nil {
		return nil, err
	}
	if _, err := ri.Patch(ctx, p.Name, types.MergePatchType, patch, metav1.PatchOptions{}); err != nil {
		return nil, err
	}
	return Empty{}, nil
}

func (d *Detail) replace(ctx context.Context, raw json.RawMessage) (any, error) {
	var p ReplaceParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	obj := map[string]any{}
	if err := yaml.Unmarshal([]byte(p.YAML), &obj); err != nil {
		return nil, invalid("yaml: %v", err)
	}
	u := &unstructured.Unstructured{Object: obj}
	apiVersion := p.Resource.Version
	if p.Resource.Group != "" {
		apiVersion = p.Resource.Group + "/" + p.Resource.Version
	}
	if u.GetAPIVersion() != apiVersion || u.GetName() != p.Name || u.GetNamespace() != p.Namespace || u.GetKind() == "" {
		return nil, invalid("the YAML names a different object")
	}
	if u.GetResourceVersion() == "" {
		return nil, invalid("the YAML has no metadata.resourceVersion")
	}
	ri, res, err := d.target(ctx, ObjectParams{p.Context, p.Resource, p.Namespace, p.Name})
	if err != nil {
		return nil, err
	}
	if res.Kind != u.GetKind() {
		return nil, invalid("the YAML names a different object")
	}
	out, err := ri.Update(ctx, u, metav1.UpdateOptions{})
	if err != nil {
		return nil, err
	}
	return ReplaceResult{ResourceVersion: out.GetResourceVersion()}, nil
}
