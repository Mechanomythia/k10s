package engine

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// golden maps each testdata/protocol file to the type it must decode into.
var golden = map[string]func() any{
	"envelope.request":               func() any { return new(Request) },
	"envelope.response.result":       func() any { return new(Response) },
	"envelope.response.error":        func() any { return new(Response) },
	"envelope.event":                 func() any { return new(Event) },
	"subscribe.result":               func() any { return new(SubscribeResult) },
	"unsubscribe.params":             func() any { return new(UnsubscribeParams) },
	"contexts.list.result":           func() any { return new(ContextsListResult) },
	"contexts.changed.event":         func() any { return new(ContextsChanged) },
	"resources.list.params":          func() any { return new(ResourcesListParams) },
	"resources.list.result":          func() any { return new(ResourcesListResult) },
	"table.subscribe.params":         func() any { return new(TableSubscribeParams) },
	"table.subscribe.all.params":     func() any { return new(TableSubscribeParams) },
	"table.snapshot.event":           func() any { return new(Snapshot) },
	"table.upsert.event":             func() any { return new(Upsert) },
	"table.delete.event":             func() any { return new(Delete) },
	"table.context.state.event":      func() any { return new(ContextStateChange) },
	"table.context.state.live.event": func() any { return new(ContextStateChange) },
	"object.yaml.params":             func() any { return new(ObjectParams) },
	"object.yaml.result":             func() any { return new(TextResult) },
	"pod.containers.params":          func() any { return new(PodContainersParams) },
	"pod.containers.result":          func() any { return new(PodContainersResult) },
	"object.describe.params":         func() any { return new(ObjectParams) },
	"object.describe.result":         func() any { return new(TextResult) },
	"object.events.params":           func() any { return new(ObjectEventsParams) },
	"object.events.result":           func() any { return new(ObjectEventsResult) },
	"logs.subscribe.params":          func() any { return new(LogsSubscribeParams) },
	"logs.lines.event":               func() any { return new(Lines) },
	"logs.end.event":                 func() any { return new(End) },
	"logs.end.error.event":           func() any { return new(End) },
	"object.delete.params":           func() any { return new(ObjectParams) },
	"workload.restart.params":        func() any { return new(ObjectParams) },
	"workload.scale.params":          func() any { return new(ScaleParams) },
	"object.replace.params":          func() any { return new(ReplaceParams) },
	"object.replace.result":          func() any { return new(ReplaceResult) },
	"table.forbidden.event":          func() any { return new(Forbidden) },
	"overview.subscribe.params":      func() any { return new(OverviewSubscribeParams) },
	"overview.summary.event":         func() any { return new(Summary) },
	"overview.summary.failing.event": func() any { return new(Summary) },
}

func TestGolden(t *testing.T) {
	files, _ := filepath.Glob("testdata/protocol/*.json")
	if len(files) != len(golden) {
		t.Errorf("%d golden files, %d registered in the test", len(files), len(golden))
	}
	for name, mk := range golden {
		t.Run(name, func(t *testing.T) {
			in, err := os.ReadFile("testdata/protocol/" + name + ".json")
			if err != nil {
				t.Fatal(err)
			}
			v := mk()
			dec := json.NewDecoder(bytes.NewReader(in))
			dec.DisallowUnknownFields() // a golden key the type lacks is a bug
			if err := dec.Decode(v); err != nil {
				t.Fatal(err)
			}
			out, err := json.Marshal(v)
			if err != nil {
				t.Fatal(err)
			}
			var a, b any
			if err := json.Unmarshal(in, &a); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(out, &b); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(a, b) {
				t.Errorf("round trip differs:\n in: %s\nout: %s", in, out)
			}
		})
	}
}
