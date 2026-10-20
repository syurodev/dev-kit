package interaction

import (
	"context"
	"errors"
	"testing"
	"time"
)

func waitForPending(t *testing.T, h *Hub) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if h.Pending() != nil {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("timed out waiting for pending interaction")
}

func TestSelectBlocksUntilComplete(t *testing.T) {
	h := NewHub(NoopPrompter{})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	var got string
	done := make(chan error, 1)
	go func() {
		id, err := h.SelectProfile(ctx, SelectRequest{Kind: KindSSH})
		got = id
		done <- err
	}()

	waitForPending(t, h)
	pend := h.Pending()
	if pend == nil || pend.Type != TypeSelect {
		t.Fatalf("pending = %+v", pend)
	}
	if err := h.Complete(pend.ID, "abc123"); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if got != "abc123" {
		t.Fatalf("got %q", got)
	}
}

func TestSelectTimeout(t *testing.T) {
	h := NewHub(NoopPrompter{})
	h.Timeout = 20 * time.Millisecond
	_, err := h.SelectProfile(context.Background(), SelectRequest{Kind: KindKafka})
	if !errors.Is(err, ErrInteractionTimeout) {
		t.Fatalf("err = %v", err)
	}
}

// A popup has no deadline of its own: it outlives any default timer and can
// still be answered, and the pending item carries no ExpiresAt for the UI to
// draw as "expired".
func TestConfirmHasNoDeadlineByDefault(t *testing.T) {
	h := NewHub(NoopPrompter{})
	if h.Timeout != 0 {
		t.Fatalf("default Timeout = %v, want none", h.Timeout)
	}
	done := make(chan error, 1)
	go func() {
		done <- h.Confirm(context.Background(), ConfirmRequest{Kind: KindDatabase, Tool: "db.query", EnvBlocks: true})
	}()
	waitForPending(t, h)
	pend := h.Pending()
	if !pend.ExpiresAt.IsZero() {
		t.Fatalf("ExpiresAt = %v, want zero", pend.ExpiresAt)
	}
	select {
	case err := <-done:
		t.Fatalf("returned before an answer: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if err := h.Complete(pend.ID, "allow"); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestCompleteAfterTimeoutReturnsNoPending(t *testing.T) {
	h := NewHub(NoopPrompter{})
	h.Timeout = 50 * time.Millisecond

	var selectErr error
	done := make(chan struct{})
	go func() {
		_, selectErr = h.SelectProfile(context.Background(), SelectRequest{Kind: KindSSH})
		done <- struct{}{}
	}()

	waitForPending(t, h)
	pend := h.Pending()
	<-done
	if !errors.Is(selectErr, ErrInteractionTimeout) {
		t.Fatalf("select err = %v, want ErrInteractionTimeout", selectErr)
	}
	if err := h.Complete(pend.ID, "late"); !errors.Is(err, ErrNoPendingInteraction) {
		t.Fatalf("complete err = %v, want ErrNoPendingInteraction", err)
	}
}

func TestSelectHeadlessNoPrompterImmediate(t *testing.T) {
	h := NewHub(nil)
	h.Headless = true
	_, err := h.SelectProfile(context.Background(), SelectRequest{Kind: KindSSH})
	if !errors.Is(err, ErrSelectionRequired) {
		t.Fatalf("err = %v", err)
	}
}

func TestConfirmAllowAndDeny(t *testing.T) {
	h := NewHub(NoopPrompter{})
	ctx := context.Background()
	go func() {
		waitForPending(t, h)
		_ = h.Complete(h.Pending().ID, "allow")
	}()
	if err := h.Confirm(ctx, ConfirmRequest{Env: "prod", Tool: "ssh.runCommand", Summary: "ls", EnvBlocks: true}); err != nil {
		t.Fatal(err)
	}

	go func() {
		waitForPending(t, h)
		_ = h.Complete(h.Pending().ID, "deny")
	}()
	if err := h.Confirm(ctx, ConfirmRequest{Env: "prod", Tool: "ssh.runCommand", Summary: "rm", EnvBlocks: true}); !errors.Is(err, ErrConfirmationDenied) {
		t.Fatalf("err = %v", err)
	}
}

func TestAllowAlwaysSkippedWhenEnvDoesNotBlock(t *testing.T) {
	h := NewHub(NoopPrompter{})
	ctx := context.Background()
	go func() {
		waitForPending(t, h)
		p := h.Pending()
		if p.AllowAlwaysOffered != true {
			t.Errorf("expected allow_always offered when env does not block")
		}
		_ = h.Complete(p.ID, "allow_always")
	}()
	if err := h.Confirm(ctx, ConfirmRequest{Env: "dev", Tool: "db.query", Summary: "select 1", EnvBlocks: false}); err != nil {
		t.Fatal(err)
	}
	if err := h.Confirm(ctx, ConfirmRequest{Env: "dev", Tool: "db.query", Summary: "select 2", EnvBlocks: false}); err != nil {
		t.Fatal(err)
	}
	if h.Pending() != nil {
		t.Fatal("should not leave pending after allow_always short-circuit")
	}
}

func TestEnvBlockIgnoresAllowAlways(t *testing.T) {
	h := NewHub(NoopPrompter{})
	ctx := context.Background()
	go func() {
		waitForPending(t, h)
		_ = h.Complete(h.Pending().ID, "allow_always")
	}()
	if err := h.Confirm(ctx, ConfirmRequest{Env: "dev", Tool: "kafka.publishMessage", Summary: "x", EnvBlocks: false}); err != nil {
		t.Fatal(err)
	}
	prompted := make(chan struct{}, 1)
	go func() {
		waitForPending(t, h)
		p := h.Pending()
		if p.AllowAlwaysOffered {
			t.Errorf("env block must not offer allow_always")
		}
		prompted <- struct{}{}
		_ = h.Complete(p.ID, "allow")
	}()
	if err := h.Confirm(ctx, ConfirmRequest{Env: "prod", Tool: "kafka.publishMessage", Summary: "x", EnvBlocks: true}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-prompted:
	default:
		t.Fatal("env block must prompt even if allow_always was set")
	}
}
