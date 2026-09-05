package capabilities

import (
	"context"
	"testing"
	"time"

	"devkit/internal/audit"
	"devkit/internal/identity"
	"devkit/internal/permissions"
)

func identityTestGateway() (*Gateway, *audit.MemorySink) {
	sink := audit.NewMemorySink()
	policy := permissions.NewPolicy([]permissions.Grant{
		{SubjectID: "builtin.test", Capability: "test:read", Scope: "thing"},
	})
	return NewGateway(policy, sink), sink
}

func TestGatewayStampsCallerAndTime(t *testing.T) {
	gw, sink := identityTestGateway()
	ctx := identity.WithCaller(context.Background(), identity.Agent("11111111-2222-3333-4444-555555555555"))
	before := time.Now().UTC().Add(-time.Second)
	if _, err := gw.Call(ctx, CallRequest{
		SubjectID: "builtin.test", Capability: "test:read", Scope: "thing",
		Handler: func(context.Context) (any, error) { return nil, nil },
	}); err != nil {
		t.Fatal(err)
	}
	events := sink.Events()
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	if events[0].Caller != "agent:11111111-2222-3333-4444-555555555555" {
		t.Fatalf("Caller = %q", events[0].Caller)
	}
	if events[0].At.Before(before) || events[0].At.IsZero() {
		t.Fatalf("At = %v, want a recent UTC time", events[0].At)
	}
}

func TestGatewayUnattributedCallIsAllowedAndLabelledUnknown(t *testing.T) {
	gw, sink := identityTestGateway()
	if _, err := gw.Call(context.Background(), CallRequest{
		SubjectID: "builtin.test", Capability: "test:read", Scope: "thing",
		Handler: func(context.Context) (any, error) { return nil, nil },
	}); err != nil {
		t.Fatalf("unattributed internal calls must still pass policy: %v", err)
	}
	if got := sink.Events()[0].Caller; got != identity.Unknown {
		t.Fatalf("Caller = %q, want %q", got, identity.Unknown)
	}
}
