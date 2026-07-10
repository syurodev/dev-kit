package app

import "testing"

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

func TestRegisterBuiltinsRegistersToolsModule(t *testing.T) {
	container := NewContainer()

	if err := RegisterBuiltins(container); err != nil {
		t.Fatalf("RegisterBuiltins returned error: %v", err)
	}

	if len(container.Modules.List()) != 2 {
		t.Fatalf("module count = %d, want 2", len(container.Modules.List()))
	}
}
