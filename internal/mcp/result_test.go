package mcp

import (
	"encoding/json"
	"testing"

	"devkit/internal/environments"
)

func TestStructuredWrapsNonObjects(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want string
	}{
		{"slice of structs", []environments.Tag{{Name: "dev"}}, `{"result":[`},
		{"slice of strings", []string{"a"}, `{"result":["a"]}`},
		{"empty slice", []string{}, `{"result":[]}`},
		{"string", "hello", `{"result":"hello"}`},
		{"int", 7, `{"result":7}`},
	}
	for _, tc := range cases {
		raw, err := json.Marshal(structured(tc.in))
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got := string(raw); len(got) < len(tc.want) || got[:len(tc.want)] != tc.want {
			t.Fatalf("%s: structured() = %s, want prefix %s", tc.name, got, tc.want)
		}
	}
}

func TestStructuredPassesObjectsThrough(t *testing.T) {
	obj := VaultStatusResult{State: "unlocked"}
	raw, _ := json.Marshal(structured(obj))
	if got := string(raw); got != `{"state":"unlocked"}` {
		t.Fatalf("objects must not be wrapped, got %s", got)
	}
	m := map[string]any{"a": 1}
	if raw, _ := json.Marshal(structured(m)); string(raw) != `{"a":1}` {
		t.Fatalf("maps must not be wrapped, got %s", raw)
	}
	if structured(nil) != nil {
		t.Fatal("nil must stay nil so the sdk omits structuredContent")
	}
	var typedNil *VaultStatusResult
	if structured(typedNil) != nil {
		t.Fatal("typed nil must stay nil")
	}
}

func TestEveryToolResultIsAnObject(t *testing.T) {
	// Guards the whole surface: a handler that answers with a bare array is
	// rejected by the client before the agent ever sees it.
	for _, v := range []any{
		[]environments.Tag{{Name: "dev"}},
		[]string(nil),
		"file contents",
		42,
	} {
		raw, err := json.Marshal(structured(v))
		if err != nil {
			t.Fatal(err)
		}
		if len(raw) == 0 || raw[0] != '{' {
			t.Fatalf("structuredContent must be a JSON object, got %s", raw)
		}
	}
}

// unwrap returns the value structured() wrapped, so assertions can keep
// naming the domain type instead of the transport envelope.
func unwrap(v any) any {
	if m, ok := v.(map[string]any); ok {
		if inner, ok := m[structuredResultKey]; ok && len(m) == 1 {
			return inner
		}
	}
	return v
}
