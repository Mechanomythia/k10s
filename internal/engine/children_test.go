package engine

import (
	"reflect"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynfake "k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"
)

func TestChildren(t *testing.T) {
	podsRef := ResourceRef{Version: "v1", Plural: "pods"}
	sel := map[string]any{
		"matchLabels":      map[string]any{"app": "web"},
		"matchExpressions": []any{map[string]any{"key": "tier", "operator": "In", "values": []any{"a", "b"}}},
	}
	const selStr = "app=web,tier in (a,b)"
	cases := []struct {
		name string
		ref  ResourceRef
		ns   string
		spec map[string]any
		want ChildrenResult
	}{
		{"deployment", ResourceRef{Group: "apps", Version: "v1", Plural: "deployments"}, "default", map[string]any{"selector": sel},
			ChildrenResult{Resource: podsRef, Namespace: "default", LabelSelector: selStr}},
		{"statefulset", ResourceRef{Group: "apps", Version: "v1", Plural: "statefulsets"}, "default", map[string]any{"selector": sel},
			ChildrenResult{Resource: podsRef, Namespace: "default", LabelSelector: selStr}},
		{"daemonset", ResourceRef{Group: "apps", Version: "v1", Plural: "daemonsets"}, "default", map[string]any{"selector": sel},
			ChildrenResult{Resource: podsRef, Namespace: "default", LabelSelector: selStr}},
		{"replicaset", ResourceRef{Group: "apps", Version: "v1", Plural: "replicasets"}, "default", map[string]any{"selector": sel},
			ChildrenResult{Resource: podsRef, Namespace: "default", LabelSelector: selStr}},
		{"job", ResourceRef{Group: "batch", Version: "v1", Plural: "jobs"}, "default", map[string]any{"selector": map[string]any{"matchLabels": map[string]any{"job-name": "j"}}},
			ChildrenResult{Resource: podsRef, Namespace: "default", LabelSelector: "job-name=j"}},
		{"service", ResourceRef{Version: "v1", Plural: "services"}, "default", map[string]any{"selector": map[string]any{"app": "web", "tier": "a"}},
			ChildrenResult{Resource: podsRef, Namespace: "default", LabelSelector: "app=web,tier=a"}},
		{"service without selector", ResourceRef{Version: "v1", Plural: "services"}, "default", map[string]any{"type": "ExternalName"},
			ChildrenResult{Resource: podsRef, Namespace: "default", None: true}},
		{"deployment with empty selector", ResourceRef{Group: "apps", Version: "v1", Plural: "deployments"}, "default", map[string]any{"selector": map[string]any{}},
			ChildrenResult{Resource: podsRef, Namespace: "default", None: true}},
		{"cronjob", ResourceRef{Group: "batch", Version: "v1", Plural: "cronjobs"}, "default", map[string]any{},
			ChildrenResult{Resource: ResourceRef{Group: "batch", Version: "v1", Plural: "jobs"}, Namespace: "default", OwnerUID: "uid-1"}},
		{"node", ResourceRef{Version: "v1", Plural: "nodes"}, "", map[string]any{},
			ChildrenResult{Resource: podsRef, FieldSelector: "spec.nodeName=obj"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gvr := schema.GroupVersionResource{Group: c.ref.Group, Version: c.ref.Version, Resource: c.ref.Plural}
			meta := map[string]any{"name": "obj", "uid": "uid-1"}
			if c.ns != "" {
				meta["namespace"] = c.ns
			}
			o := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "Parent", "metadata": meta, "spec": c.spec}}
			dc := dynfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{gvr: "ParentList"})
			if err := dc.Tracker().Create(gvr, o, c.ns); err != nil {
				t.Fatal(err)
			}
			d := detailEnv(t, nil, nil, dc)
			var res ChildrenResult
			err := call(t, d.children, ObjectParams{Context: "c", Resource: c.ref, Namespace: c.ns, Name: "obj"}, &res)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(res, c.want) {
				t.Errorf("result = %+v, want %+v", res, c.want)
			}
		})
	}
}

func TestChildrenUnsupportedAndForbidden(t *testing.T) {
	dc := dynfake.NewSimpleDynamicClient(runtime.NewScheme())
	calls := 0
	dc.PrependReactor("get", "*", func(clienttesting.Action) (bool, runtime.Object, error) {
		calls++
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Group: "apps", Resource: "deployments"}, "web", nil)
	})
	d := detailEnv(t, nil, nil, dc)
	children := mapAPIErrors(d.children)
	for _, plural := range []string{"pods", "configmaps"} {
		err := call(t, children, ObjectParams{Context: "c", Resource: ResourceRef{Version: "v1", Plural: plural}, Namespace: "default", Name: "x"}, nil)
		if errCode(err) != CodeInvalidParams {
			t.Errorf("%s: %v", plural, err)
		}
	}
	if calls != 0 {
		t.Errorf("%d API calls for unsupported kinds", calls)
	}
	err := call(t, children, ObjectParams{Context: "c", Resource: ResourceRef{Group: "apps", Version: "v1", Plural: "deployments"}, Namespace: "default", Name: "web"}, nil)
	if errCode(err) != CodeForbidden {
		t.Errorf("403: %v", err)
	}
}
