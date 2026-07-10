package app

import (
	"testing"

	"changeme/internal/permissions"
)

func TestNewContainerWiresCoreServices(t *testing.T) {
	container := NewContainer()

	if container.Commands == nil {
		t.Fatal("Commands is nil")
	}
	if container.Modules == nil {
		t.Fatal("Modules is nil")
	}
	if container.Vault == nil {
		t.Fatal("Vault is nil")
	}
	if container.Capabilities == nil {
		t.Fatal("Capabilities is nil")
	}
}

func TestNewContainerDeniesScopeOutsideBridgeGrant(t *testing.T) {
	container := NewContainer()
	decision := container.Permissions.Check(permissions.Request{
		SubjectID:  "builtin.notes",
		Capability: "notes:read",
		Scope:      "profile:unrelated",
	})
	if decision.Allowed {
		t.Fatal("ungranted bridge scope was allowed")
	}
}

func TestRegisterBuiltinsRegistersToolsModule(t *testing.T) {
	container := NewContainer()

	if err := RegisterBuiltins(container); err != nil {
		t.Fatalf("RegisterBuiltins returned error: %v", err)
	}

	if len(container.Modules.List()) != 4 {
		t.Fatalf("module count = %d, want 4", len(container.Modules.List()))
	}
}
