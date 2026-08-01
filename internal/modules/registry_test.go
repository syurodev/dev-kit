package modules

import (
	"reflect"
	"strings"
	"testing"
)

func TestRegistryRegistersManifestAndProtectsSnapshot(t *testing.T) {
	registry := NewRegistry()
	manifest := ModuleManifest{
		ID:           "tools",
		Title:        "Tools",
		Version:      "1.0.0",
		APIVersion:   CurrentAPIVersion,
		Commands:     []string{"tools.hash"},
		Views:        []string{"tools.hash"},
		Capabilities: []string{"tools:hash"},
	}
	if err := registry.Register(manifest); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	manifest.Commands[0] = "tools.changed"
	manifest.Capabilities[0] = "tools:changed"

	got := registry.List()
	want := []ModuleManifest{
		{
			ID:           "tools",
			Title:        "Tools",
			Version:      "1.0.0",
			APIVersion:   CurrentAPIVersion,
			Commands:     []string{"tools.hash"},
			Views:        []string{"tools.hash"},
			Capabilities: []string{"tools:hash"},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("List() = %#v, want %#v", got, want)
	}

	got[0].Views[0] = "tools.changed"
	if registry.List()[0].Views[0] != "tools.hash" {
		t.Fatal("List must return a snapshot, not registry-owned slices")
	}
}

func TestRegistryListsManifestsDeterministically(t *testing.T) {
	registry := NewRegistry()
	for _, manifest := range []ModuleManifest{
		{ID: "ssh", Title: "SSH", Version: "1.0.0", APIVersion: CurrentAPIVersion},
		{ID: "database", Title: "Database", Version: "1.0.0", APIVersion: CurrentAPIVersion},
		{ID: "notes", Title: "Notes", Version: "1.0.0", APIVersion: CurrentAPIVersion},
	} {
		if err := registry.Register(manifest); err != nil {
			t.Fatalf("Register(%q) returned error: %v", manifest.ID, err)
		}
	}

	got := registry.List()
	ids := []string{got[0].ID, got[1].ID, got[2].ID}
	if !reflect.DeepEqual(ids, []string{"database", "notes", "ssh"}) {
		t.Fatalf("module order = %v, want [database notes ssh]", ids)
	}
}

func TestValidateManifestRejectsInvalidMetadata(t *testing.T) {
	base := ModuleManifest{
		ID:         "tools",
		Title:      "Tools",
		Version:    "1.0.0",
		APIVersion: CurrentAPIVersion,
	}
	tests := []struct {
		name    string
		mutate  func(*ModuleManifest)
		message string
	}{
		{name: "missing id", mutate: func(m *ModuleManifest) { m.ID = "" }, message: "module id is required"},
		{name: "invalid id", mutate: func(m *ModuleManifest) { m.ID = "Tools" }, message: "must contain only lowercase"},
		{name: "missing title", mutate: func(m *ModuleManifest) { m.Title = "" }, message: "module title is required"},
		{name: "invalid version", mutate: func(m *ModuleManifest) { m.Version = "1.0" }, message: "semantic version"},
		{name: "invalid api version", mutate: func(m *ModuleManifest) { m.APIVersion = "v1" }, message: "positive integer"},
		{name: "invalid command id", mutate: func(m *ModuleManifest) { m.Commands = []string{"tools.Hash"} }, message: "command id"},
		{name: "invalid view id", mutate: func(m *ModuleManifest) { m.Views = []string{"tools view"} }, message: "view id"},
		{name: "invalid capability", mutate: func(m *ModuleManifest) { m.Capabilities = []string{"tools"} }, message: "namespaced"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manifest := base
			tt.mutate(&manifest)
			err := ValidateManifest(manifest)
			if err == nil || !strings.Contains(err.Error(), tt.message) {
				t.Fatalf("ValidateManifest error = %v, want message containing %q", err, tt.message)
			}
		})
	}
}

func TestRegistryRejectsDuplicateContributionsAtomically(t *testing.T) {
	registry := NewRegistry()
	first := ModuleManifest{
		ID:         "tools",
		Title:      "Tools",
		Version:    "1.0.0",
		APIVersion: CurrentAPIVersion,
		Commands:   []string{"tools.hash"},
		Views:      []string{"tools.hash"},
	}
	if err := registry.Register(first); err != nil {
		t.Fatalf("first Register returned error: %v", err)
	}

	tests := []ModuleManifest{
		{ID: "notes", Title: "Notes", Version: "1.0.0", APIVersion: CurrentAPIVersion, Commands: []string{"notes.create", "notes.create"}},
		{ID: "database", Title: "Database", Version: "1.0.0", APIVersion: CurrentAPIVersion, Views: []string{"database.connections", "database.connections"}},
		{ID: "other-tools", Title: "Other Tools", Version: "1.0.0", APIVersion: CurrentAPIVersion, Commands: []string{"tools.hash", "other.run"}},
		{ID: "other-views", Title: "Other Views", Version: "1.0.0", APIVersion: CurrentAPIVersion, Views: []string{"tools.hash", "other.view"}},
	}
	for _, manifest := range tests {
		if err := registry.Register(manifest); err == nil {
			t.Fatalf("Register(%q) must reject duplicate contribution", manifest.ID)
		}
	}

	got := registry.List()
	if len(got) != 1 || got[0].ID != "tools" {
		t.Fatalf("failed registrations changed registry: %#v", got)
	}
}

func TestRegistryRejectsDuplicateModule(t *testing.T) {
	registry := NewRegistry()
	manifest := ModuleManifest{ID: "tools", Title: "Tools", Version: "1.0.0", APIVersion: CurrentAPIVersion}
	if err := registry.Register(manifest); err != nil {
		t.Fatalf("first Register returned error: %v", err)
	}
	if err := registry.Register(manifest); err == nil {
		t.Fatal("duplicate module must error")
	}
}
