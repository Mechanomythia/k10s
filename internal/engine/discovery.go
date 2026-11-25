package engine

import (
	"context"
	"encoding/json"
	"slices"
	"strings"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
)

// Resources lists every kind of a context that can be listed and watched,
// CRDs included, from the context's cached discovery. Subresources are left
// out; groups that fail discovery are skipped.
func (c *Conns) Resources(ctx context.Context, name string) ([]ResourceKind, error) {
	cn, err := c.Get(ctx, name)
	if err != nil {
		return nil, err
	}
	lists, err := cn.Discovery.ServerPreferredResources()
	if err != nil && !discovery.IsGroupDiscoveryFailedError(err) {
		return nil, err
	}
	kinds := []ResourceKind{}
	for _, l := range lists {
		gv, err := schema.ParseGroupVersion(l.GroupVersion)
		if err != nil {
			continue
		}
		for _, r := range l.APIResources {
			if strings.Contains(r.Name, "/") || !slices.Contains(r.Verbs, "list") || !slices.Contains(r.Verbs, "watch") {
				continue
			}
			kinds = append(kinds, ResourceKind{Group: gv.Group, Version: gv.Version, Kind: r.Kind, Plural: r.Name, ShortNames: r.ShortNames, Namespaced: r.Namespaced})
		}
	}
	return kinds, nil
}

// Register wires resources.list into s.
func (c *Conns) Register(s *Server) {
	s.Handlers[MethodResourcesList] = func(ctx context.Context, params json.RawMessage) (any, error) {
		var p ResourcesListParams
		if err := json.Unmarshal(params, &p); err != nil || p.Context == "" {
			return nil, &Error{Code: CodeInvalidParams, Message: "resources.list needs a context"}
		}
		kinds, err := c.Resources(ctx, p.Context)
		if err != nil {
			return nil, err
		}
		return ResourcesListResult{Resources: kinds}, nil
	}
}
