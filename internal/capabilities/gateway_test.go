package capabilities

import (
	"context"
	"testing"

	"changeme/internal/permissions"
)

func TestGatewayDeniesWithoutPermission(t *testing.T) {
	policy := permissions.NewPolicy(nil)
	gateway := NewGateway(policy)

	_, err := gateway.Call(context.Background(), CallRequest{
		SubjectID:  "plugin.example",
		Capability: "vault:read",
		Scope:      "secret:main",
		Handler: func(ctx context.Context) (any, error) {
			return "secret", nil
		},
	})

	if err == nil {
		t.Fatal("Call returned nil error, want permission error")
	}
}

func TestGatewayCallsHandlerWhenAllowed(t *testing.T) {
	policy := permissions.NewPolicy([]permissions.Grant{
		{SubjectID: "builtin.tools", Capability: "tools:hash", Scope: "*"},
	})
	gateway := NewGateway(policy)

	result, err := gateway.Call(context.Background(), CallRequest{
		SubjectID:  "builtin.tools",
		Capability: "tools:hash",
		Scope:      "sha256",
		Handler: func(ctx context.Context) (any, error) {
			return "ok", nil
		},
	})

	if err != nil {
		t.Fatalf("Call returned error: %v", err)
	}
	if result != "ok" {
		t.Fatalf("result = %v, want ok", result)
	}
}
