package capabilities

import (
	"context"
	"strings"
	"testing"

	"changeme/internal/audit"
	"changeme/internal/permissions"
)

func TestNewGatewayRejectsNilDependencies(t *testing.T) {
	validPolicy := permissions.NewPolicy(nil)
	validSink := audit.NewMemorySink()
	var typedNilSink *audit.MemorySink

	tests := []struct {
		name   string
		policy *permissions.Policy
		sink   audit.Sink
		want   string
	}{
		{name: "nil policy", policy: nil, sink: validSink, want: "capability policy is required"},
		{name: "nil audit sink", policy: validPolicy, sink: nil, want: "audit sink is required"},
		{name: "typed nil audit sink", policy: validPolicy, sink: typedNilSink, want: "audit sink is required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				recovered := recover()
				if recovered == nil {
					t.Fatal("NewGateway did not panic")
				}
				if !strings.Contains(recovered.(string), tt.want) {
					t.Fatalf("panic = %q, want message containing %q", recovered, tt.want)
				}
			}()

			NewGateway(tt.policy, tt.sink)
		})
	}
}

func TestGatewayAuditsDeniedCallWithoutRunningHandler(t *testing.T) {
	policy := permissions.NewPolicy(nil)
	sink := audit.NewMemorySink()
	gateway := NewGateway(policy, sink)
	handlerCalls := 0

	_, err := gateway.Call(context.Background(), CallRequest{
		SubjectID:  "plugin.example",
		Capability: "vault:read",
		Scope:      "secret:main",
		Handler: func(ctx context.Context) (any, error) {
			handlerCalls++
			return "secret", nil
		},
	})

	if err == nil {
		t.Fatal("Call returned nil error, want permission error")
	}
	if handlerCalls != 0 {
		t.Fatalf("handler calls = %d, want 0", handlerCalls)
	}

	events := sink.Events()
	if len(events) != 1 {
		t.Fatalf("event count = %d, want 1", len(events))
	}
	got := events[0]
	if got.SubjectID != "plugin.example" {
		t.Fatalf("event subject ID = %q, want %q", got.SubjectID, "plugin.example")
	}
	if got.Capability != "vault:read" {
		t.Fatalf("event capability = %q, want %q", got.Capability, "vault:read")
	}
	if got.Scope != "secret:main" {
		t.Fatalf("event scope = %q, want %q", got.Scope, "secret:main")
	}
	if got.Outcome != audit.OutcomeDenied {
		t.Fatalf("event outcome = %q, want %q", got.Outcome, audit.OutcomeDenied)
	}
}

func TestGatewayDeniesWithoutPermission(t *testing.T) {
	policy := permissions.NewPolicy(nil)
	gateway := NewGateway(policy, audit.NewMemorySink())

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
	sink := audit.NewMemorySink()
	gateway := NewGateway(policy, sink)

	result, err := gateway.Call(context.Background(), CallRequest{
		SubjectID:  "builtin.tools",
		Capability: "tools:hash",
		Scope:      "sha256",
		Handler: func(ctx context.Context) (any, error) {
			events := sink.Events()
			if len(events) != 1 {
				t.Fatalf("event count before handler = %d, want 1", len(events))
			}
			if events[0].Outcome != audit.OutcomeAllowed {
				t.Fatalf("event outcome before handler = %q, want %q", events[0].Outcome, audit.OutcomeAllowed)
			}
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
