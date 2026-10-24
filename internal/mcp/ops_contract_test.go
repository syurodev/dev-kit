package mcp

import (
	"devkit/internal/ops"
	"encoding/json"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"os"
	"reflect"
	"testing"
)

func TestMCPSurfaceMatchesRegistry(t *testing.T) {
	raw, err := os.ReadFile("testdata/legacy_ops_tools.json")
	if err != nil {
		t.Fatal(err)
	}
	var legacy []string
	if err := json.Unmarshal(raw, &legacy); err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{}
	for _, name := range legacy {
		allowed[name] = true
	}
	actual := map[string]bool{}
	for _, tool := range listAllTools(t) {
		actual[tool.Name] = true
	}
	for _, op := range ops.All() {
		d := op.Descriptor()
		if d.Exposure == ops.UI {
			if actual[d.Name] {
				t.Errorf("UI tool %s exposed", d.Name)
			}
			continue
		}
		if allowed[d.Name] {
			t.Errorf("remove migrated %s from legacy", d.Name)
		}
		allowed[d.Name] = true
		if !actual[d.Name] {
			t.Errorf("missing %s", d.Name)
		}
	}
	for name := range actual {
		if !allowed[name] {
			t.Errorf("%s neither op nor legacy", name)
		}
	}
	for _, name := range legacy {
		if !actual[name] {
			t.Errorf("remove missing legacy %s", name)
		}
	}
}

// Frozen from tools_notes.go before migration, not from registry declarations.
func TestNotesWireDescriptionsAndAnnotationsStayIdentical(t *testing.T) {
	raw, err := os.ReadFile("testdata/notes_surface.json")
	if err != nil {
		t.Fatal(err)
	}
	var baseline []struct {
		Name, Description string
		ReadOnly          bool
		Destructive       *bool
	}
	if err := json.Unmarshal(raw, &baseline); err != nil {
		t.Fatal(err)
	}
	actual := map[string]*sdk.Tool{}
	for _, tool := range listAllTools(t) {
		actual[tool.Name] = tool
	}
	for _, want := range baseline {
		got := actual[want.Name]
		if got == nil {
			t.Fatal(want.Name)
		}
		if got.Description != want.Description || got.Annotations.ReadOnlyHint != want.ReadOnly || !reflect.DeepEqual(got.Annotations.DestructiveHint, want.Destructive) || got.Annotations.OpenWorldHint != nil {
			t.Errorf("%s wire metadata changed: %+v", want.Name, got)
		}
	}
}
