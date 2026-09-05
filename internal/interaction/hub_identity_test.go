package interaction

import (
	"context"
	"errors"
	"testing"
	"time"

	"devkit/internal/identity"
)

func agentCtx(id string) context.Context {
	return identity.WithCaller(context.Background(), identity.Agent(id))
}

func waitForPendingCount(t *testing.T, h *Hub, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(h.PendingList()) == want {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("pending = %d, want %d", len(h.PendingList()), want)
}

func TestTwoCallersPendAtOnce(t *testing.T) {
	h := NewHub(NoopPrompter{})
	done := make(chan error, 2)
	for _, id := range []string{"aaaaaaaa-0000-0000-0000-000000000001", "aaaaaaaa-0000-0000-0000-000000000002"} {
		go func(id string) {
			done <- h.Confirm(agentCtx(id), ConfirmRequest{Env: "dev", Tool: "db.query", ProfileID: "p1", Kind: KindDatabase})
		}(id)
	}
	waitForPendingCount(t, h, 2)
	list := h.PendingList()
	if list[0].Caller == "" || list[0].Caller == list[1].Caller {
		t.Fatalf("pending must carry distinct callers: %+v", list)
	}
	for _, p := range list {
		if err := h.Complete(p.ID, "allow"); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		if err := <-done; err != nil {
			t.Fatalf("confirm %d: %v", i, err)
		}
	}
}

func TestSameCallerCannotPendTwice(t *testing.T) {
	h := NewHub(NoopPrompter{})
	ctx := agentCtx("aaaaaaaa-0000-0000-0000-000000000001")
	go func() { _ = h.Confirm(ctx, ConfirmRequest{Env: "dev", Tool: "db.query", ProfileID: "p1"}) }()
	waitForPendingCount(t, h, 1)
	err := h.Confirm(ctx, ConfirmRequest{Env: "dev", Tool: "ssh.runCommand", ProfileID: "p2"})
	if !errors.Is(err, ErrCallerBusy) {
		t.Fatalf("got %v, want ErrCallerBusy", err)
	}
	h.Complete(h.PendingList()[0].ID, "deny")
}

func TestAllowAlwaysIsScopedToCallerToolProfile(t *testing.T) {
	h := NewHub(NoopPrompter{})
	a := agentCtx("aaaaaaaa-0000-0000-0000-000000000001")
	b := agentCtx("aaaaaaaa-0000-0000-0000-000000000002")
	req := ConfirmRequest{Env: "dev", Tool: "db.query", ProfileID: "p1", Kind: KindDatabase}

	done := make(chan error, 1)
	go func() { done <- h.Confirm(a, req) }()
	waitForPendingCount(t, h, 1)
	if err := h.Complete(h.PendingList()[0].ID, "allow_always"); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}

	// Same caller, tool, profile: short-circuits.
	if err := h.Confirm(a, req); err != nil {
		t.Fatalf("allow_always for A should short-circuit: %v", err)
	}
	// Same caller, different profile: prompts again.
	go func() { done <- h.Confirm(a, ConfirmRequest{Env: "dev", Tool: "db.query", ProfileID: "p2"}) }()
	waitForPendingCount(t, h, 1)
	h.Complete(h.PendingList()[0].ID, "deny")
	<-done
	// Other caller, same tool and profile: prompts again — never inherits A's grant.
	go func() { done <- h.Confirm(b, req) }()
	waitForPendingCount(t, h, 1)
	h.Complete(h.PendingList()[0].ID, "deny")
	if err := <-done; !errors.Is(err, ErrConfirmationDenied) {
		t.Fatalf("B must have been prompted and denied, got %v", err)
	}

	// Forgetting A removes its grant.
	h.ForgetCaller(identity.Agent("aaaaaaaa-0000-0000-0000-000000000001").String())
	go func() { done <- h.Confirm(a, req) }()
	waitForPendingCount(t, h, 1)
	h.Complete(h.PendingList()[0].ID, "deny")
	if err := <-done; !errors.Is(err, ErrConfirmationDenied) {
		t.Fatalf("after ForgetCaller A must be prompted again, got %v", err)
	}
}
