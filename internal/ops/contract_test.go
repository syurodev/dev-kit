package ops

import (
	"context"
	"devkit/internal/capabilities"
	"devkit/internal/identity"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

func canary(v reflect.Value) {
	switch v.Kind() {
	case reflect.String:
		v.SetString("CANARY-private-text")
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			canary(v.Field(i))
		}
	case reflect.Slice:
		v.Set(reflect.MakeSlice(v.Type(), 1, 1))
		canary(v.Index(0))
	}
}
func TestRegistryContracts(t *testing.T) {
	names := map[string]Descriptor{}
	for _, op := range All() {
		d := op.Descriptor()
		if _, ok := names[d.Name]; ok {
			t.Errorf("duplicate %s", d.Name)
		}
		names[d.Name] = d
		if d.Name == "" || d.Subject == "" || d.Capability == "" || d.Grant == "" {
			t.Errorf("incomplete %s", d.Name)
		}
		if d.Exposure != Both && d.Reason == "" {
			t.Errorf("%s: exposure reason missing", d.Name)
		}
		if d.Exposure != UI && strings.TrimSpace(d.Agent.Description) == "" {
			t.Errorf("%s: agent-exposed operation has no MCP description", d.Name)
		}
		if d.Confirm.Policy == AskOnce && d.Confirm.Reason == "" {
			t.Errorf("%s: AskOnce reason missing", d.Name)
		}
		if (d.Risk == Destructive || d.Risk == External) && d.Confirm.Policy == Never && d.Safe == "" {
			t.Errorf("%s: safety reason missing", d.Name)
		}
		for _, fill := range []bool{false, true} {
			in := reflect.New(op.InputType()).Elem()
			if fill {
				canary(in)
			}
			scope, err := op.Resolve(context.Background(), in.Interface())
			if err != nil {
				t.Fatal(err)
			}
			b, err := json.Marshal(scope)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(b), "CANARY") {
				t.Errorf("%s leaks private input: %s", d.Name, b)
			}
		}
	}
	for _, op := range All() {
		d := op.Descriptor()
		for _, twin := range d.Twins {
			other, ok := names[twin]
			if !ok || other.Subject != d.Subject || other.Capability != d.Capability {
				t.Errorf("%s: invalid twin %s", d.Name, twin)
			}
		}
	}
}

func TestNotesAuditContractMatchesLegacyBridge(t *testing.T) {
	raw, err := os.ReadFile("testdata/notes_audit.json")
	if err != nil {
		t.Fatal(err)
	}
	var baseline []struct {
		Bridge, Capability, Scope string
		Metadata                  map[string]any
	}
	if err := json.Unmarshal(raw, &baseline); err != nil {
		t.Fatal(err)
	}
	actual := map[string]Operation{}
	for _, op := range Notes() {
		actual[op.Descriptor().Bridge] = op
	}
	for _, want := range baseline {
		op := actual[want.Bridge]
		if op == nil {
			t.Fatal(want.Bridge)
		}
		in := reflect.New(op.InputType()).Elem()
		canary(in)
		for _, field := range []string{"ID", "VersionID"} {
			v := in.FieldByName(field)
			if v.IsValid() {
				v.SetString("abcdef0123456789abcdef01")
			}
		}
		scope, err := op.Resolve(context.Background(), in.Interface())
		if err != nil {
			t.Fatal(err)
		}
		d := op.Descriptor()
		if scope.Meta == nil {
			scope.Meta = map[string]any{}
		}
		scope.Meta["action"] = dAction(op)
		got, _ := json.Marshal(scope.Meta)
		expected, _ := json.Marshal(want.Metadata)
		if d.Subject != "builtin.notes" || d.Capability != want.Capability || scope.Value != want.Scope || string(got) != string(expected) {
			t.Errorf("%s contract changed: %s %s %s", want.Bridge, d.Capability, scope.Value, got)
		}
	}
}

// Invoke supplies the action; capture the real request without running domain code.
func dAction(op Operation) string {
	c := &captureCaller{}
	_, _ = op.InvokeJSON(identity.WithCaller(context.Background(), identity.UI()), Runtime{Caller: c}, []byte(`{}`))
	action, _ := c.request.Metadata["action"].(string)
	return action
}

type captureCaller struct{ request capabilities.CallRequest }

func (c *captureCaller) CallOperation(_ context.Context, r capabilities.CallRequest) (any, error) {
	c.request = r
	return nil, nil
}

func TestNotesInputJSONNamesAndSchemaHintsStayIdentical(t *testing.T) {
	raw, err := os.ReadFile("testdata/notes_inputs.json")
	if err != nil {
		t.Fatal(err)
	}
	var baseline map[string][]struct{ Name, Type, Tag string }
	if err := json.Unmarshal(raw, &baseline); err != nil {
		t.Fatal(err)
	}
	for _, op := range Notes() {
		if op.Descriptor().Exposure == UI {
			continue
		}
		typ := op.InputType()
		want, ok := baseline[typ.Name()]
		if !ok {
			if typ.NumField() != 0 {
				t.Fatal(typ)
			}
			continue
		}
		if typ.NumField() != len(want) {
			t.Fatal(typ)
		}
		for i, field := range want {
			actual := typ.Field(i)
			if actual.Name != field.Name || actual.Type.String() != field.Type || string(actual.Tag) != field.Tag {
				t.Errorf("%s.%s wire schema changed", typ.Name(), field.Name)
			}
		}
	}
}

func (c *captureCaller) PublishError(_ context.Context, _ string, err error) error { return err }
