package interaction

import (
	"context"
	"devkit/internal/identity"
	"errors"
	"testing"
)

func TestAgentConfirmUsesBrokerAndPreservesGatePolicy(t *testing.T) {
	h := NewHub(NoopPrompter{})
	ctx := identity.WithCaller(context.Background(), identity.Agent("session-a"))
	calls := 0
	h.ConfirmAgent = func(ctx context.Context, req ConfirmRequest) (string, error) {
		calls++
		if len(h.PendingList()) != 0 {
			t.Fatal("broker request also opened popup")
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("gate deadline lost")
		}
		return "allow_always", nil
	}
	req := ConfirmRequest{Tool: "db.query", ProfileID: "p1", Env: "dev"}
	if err := h.Confirm(ctx, req); err != nil {
		t.Fatal(err)
	}
	if err := h.Confirm(ctx, req); err != nil || calls != 1 {
		t.Fatal("scoped grant not reused")
	}
	req.Env, req.EnvBlocks = "prod", true
	for range 2 {
		if err := h.Confirm(ctx, req); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 3 {
		t.Fatal("production reused a grant")
	}
	req.ProfileID = "p2"
	req.EnvBlocks = false
	if err := h.Confirm(ctx, req); err != nil || calls != 4 {
		t.Fatal("grant crossed profiles")
	}
	h.ConfirmAgent = func(context.Context, ConfirmRequest) (string, error) { return "unknown", nil }
	if err := h.Confirm(ctx, ConfirmRequest{Tool: "db.query", ProfileID: "p3"}); !errors.Is(err, ErrConfirmationDenied) {
		t.Fatalf("unknown answer = %v", err)
	}
	h.Headless = true
	h.ConfirmAgent = func(context.Context, ConfirmRequest) (string, error) {
		t.Fatal("headless called broker")
		return "allow", nil
	}
	if err := h.Confirm(ctx, req); !errors.Is(err, ErrConfirmationRequired) {
		t.Fatalf("headless = %v", err)
	}
}

func TestTerminalConfirmKeepsPopup(t *testing.T) {
	h := NewHub(NoopPrompter{})
	h.ConfirmAgent = func(context.Context, ConfirmRequest) (string, error) {
		t.Fatal("terminal routed into an agent")
		return "allow", nil
	}
	done := make(chan error, 1)
	go func() {
		done <- h.Confirm(identity.WithCaller(context.Background(), identity.UserTerminal()), ConfirmRequest{Tool: "db.query"})
	}()
	waitForPending(t, h)
	if err := h.Complete(h.Pending().ID, "deny"); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, ErrConfirmationDenied) {
		t.Fatalf("terminal denial = %v", err)
	}
}
