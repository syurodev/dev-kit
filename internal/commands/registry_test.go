package commands

import (
	"context"
	"testing"
)

func TestRegistryExecutesRegisteredCommand(t *testing.T) {
	registry := NewRegistry()
	err := registry.Register(Command{
		ID:    "tools.hash.sha256",
		Title: "SHA-256",
		Handler: func(ctx context.Context, input any) (any, error) {
			return "ok", nil
		},
	})
	if err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	result, err := registry.Execute(context.Background(), "tools.hash.sha256", nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result != "ok" {
		t.Fatalf("result = %v, want ok", result)
	}
}

func TestRegistryRejectsDuplicateCommand(t *testing.T) {
	registry := NewRegistry()
	command := Command{ID: "x", Title: "X", Handler: func(ctx context.Context, input any) (any, error) { return nil, nil }}

	if err := registry.Register(command); err != nil {
		t.Fatalf("first Register returned error: %v", err)
	}
	if err := registry.Register(command); err == nil {
		t.Fatal("second Register returned nil, want error")
	}
}

func TestRegistryExecuteUnknownCommand(t *testing.T) {
	registry := NewRegistry()
	if _, err := registry.Execute(context.Background(), "missing", nil); err == nil {
		t.Fatal("executing unknown command must error")
	}
}
