package bridge

import (
	"context"
	"testing"

	"devkit/internal/identity"
)

func TestCallerContextDefaultsToUI(t *testing.T) {
	c, ok := identity.FromContext(callerContext(context.Background()))
	if !ok || c.Kind != identity.KindUI {
		t.Fatalf("got %+v ok=%v, want ui", c, ok)
	}
}

func TestCallerContextKeepsExistingCaller(t *testing.T) {
	in := identity.WithCaller(context.Background(), identity.Agent("11111111-2222-3333-4444-555555555555"))
	c, _ := identity.FromContext(callerContext(in))
	if !c.IsAgent() {
		t.Fatalf("MCP-stamped agent caller must survive the bridge: %+v", c)
	}
}
