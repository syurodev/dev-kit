package modules

import "testing"

func TestRegistryRegistersModule(t *testing.T) {
	registry := NewRegistry()
	err := registry.Register(Module{
		ID:    "tools",
		Title: "Tools",
		Commands: []string{
			"tools.hash",
		},
	})
	if err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	modules := registry.List()
	if len(modules) != 1 {
		t.Fatalf("len(modules) = %d, want 1", len(modules))
	}
	if modules[0].ID != "tools" {
		t.Fatalf("module id = %q", modules[0].ID)
	}
}

func TestRegistryRejectsDuplicateModule(t *testing.T) {
	registry := NewRegistry()
	module := Module{ID: "tools", Title: "Tools"}
	if err := registry.Register(module); err != nil {
		t.Fatalf("first Register returned error: %v", err)
	}
	if err := registry.Register(module); err == nil {
		t.Fatal("duplicate module must error")
	}
}
