package identity

import (
	"context"
	"testing"
)

func TestCallerRoundTrip(t *testing.T) {
	ctx := WithCaller(context.Background(), Agent("11111111-2222-3333-4444-555555555555"))
	c, ok := FromContext(ctx)
	if !ok || !c.IsAgent() || c.SessionID != "11111111-2222-3333-4444-555555555555" {
		t.Fatalf("got %+v ok=%v", c, ok)
	}
	if got := c.String(); got != "agent:11111111-2222-3333-4444-555555555555" {
		t.Fatalf("String() = %q", got)
	}
}

func TestLabels(t *testing.T) {
	if got := Label(context.Background()); got != Unknown {
		t.Fatalf("empty ctx label = %q, want %q", got, Unknown)
	}
	if got := UserTerminal().String(); got != "user-terminal" {
		t.Fatalf("user-terminal label = %q", got)
	}
	if got := UI().String(); got != "ui" {
		t.Fatalf("ui label = %q", got)
	}
	if UI().IsAgent() || UserTerminal().IsAgent() {
		t.Fatal("only Kind agent with a session id is an agent")
	}
	if Agent("").IsAgent() {
		t.Fatal("agent without session id must not count as an agent")
	}
}
