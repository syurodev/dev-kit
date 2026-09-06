package agenthub_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"devkit/internal/agenthub"
	"devkit/internal/interaction"
)

func TestPermissionTimeoutRejects(t *testing.T) {
	svc := newTestService(t)
	svc.PermissionTimeout = 30 * time.Millisecond
	// no ResolvePermission / nil → fail closed
	sess, err := svc.CreateSession(context.Background(), agenthub.CreateInput{
		AgentType: agenthub.AgentTypeMock,
		Cwd:       t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Invoke(context.Background(), agenthub.InvokeInput{
		SessionID: sess.ID,
		Prompt:    "NEED_PERMISSION",
	})
	if !errors.Is(err, agenthub.ErrPermissionDenied) {
		t.Fatalf("got %v want ErrPermissionDenied", err)
	}
}

func TestPermissionAllowContinues(t *testing.T) {
	svc := newTestService(t)
	svc.ResolvePermission = func(ctx context.Context, sessionID string, params json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`{"outcome":{"outcome":"selected","optionId":"allow"}}`), nil
	}
	sess, err := svc.CreateSession(context.Background(), agenthub.CreateInput{
		AgentType: agenthub.AgentTypeMock,
		Cwd:       t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := svc.Invoke(context.Background(), agenthub.InvokeInput{
		SessionID: sess.ID,
		Prompt:    "NEED_PERMISSION",
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != "completed" {
		t.Fatalf("%+v", out)
	}
}

func TestPermissionWaiterTimeoutRejects(t *testing.T) {
	svc := newTestService(t)
	svc.PermissionTimeout = 30 * time.Millisecond
	waiter := agenthub.NewPermissionWaiter(nil)
	svc.ResolvePermission = waiter.Resolve

	sess, err := svc.CreateSession(context.Background(), agenthub.CreateInput{
		AgentType: agenthub.AgentTypeMock,
		Cwd:       t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Invoke(context.Background(), agenthub.InvokeInput{
		SessionID: sess.ID,
		Prompt:    "NEED_PERMISSION",
	})
	if !errors.Is(err, agenthub.ErrPermissionDenied) {
		t.Fatalf("got %v want ErrPermissionDenied", err)
	}
}

func TestPermissionWaiterAllowContinues(t *testing.T) {
	svc := newTestService(t)
	svc.PermissionTimeout = 2 * time.Second

	var notifyCh = make(chan agenthub.PendingPermission, 1)
	waiter := agenthub.NewPermissionWaiter(func(p agenthub.PendingPermission) {
		notifyCh <- p
	})
	svc.ResolvePermission = waiter.Resolve

	sess, err := svc.CreateSession(context.Background(), agenthub.CreateInput{
		AgentType: agenthub.AgentTypeMock,
		Cwd:       t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}

	errCh := make(chan error, 1)
	var out agenthub.InvokeResult
	go func() {
		var invokeErr error
		out, invokeErr = svc.Invoke(context.Background(), agenthub.InvokeInput{
			SessionID: sess.ID,
			Prompt:    "NEED_PERMISSION",
		})
		errCh <- invokeErr
	}()

	var pending agenthub.PendingPermission
	select {
	case pending = <-notifyCh:
	case <-time.After(2 * time.Second):
		t.Fatal("waiter did not notify")
	}

	allow := json.RawMessage(`{"outcome":{"outcome":"selected","optionId":"allow"}}`)
	if err := waiter.Complete(pending.ID, allow); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("invoke: %v", err)
		}
		if out.Status != "completed" {
			t.Fatalf("%+v", out)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for invoke")
	}
}

func TestPermissionWaiterDenyFailsTurn(t *testing.T) {
	svc := newTestService(t)
	svc.PermissionTimeout = 2 * time.Second

	notifyCh := make(chan agenthub.PendingPermission, 1)
	waiter := agenthub.NewPermissionWaiter(func(p agenthub.PendingPermission) {
		notifyCh <- p
	})
	svc.ResolvePermission = waiter.Resolve

	sess, err := svc.CreateSession(context.Background(), agenthub.CreateInput{
		AgentType: agenthub.AgentTypeMock,
		Cwd:       t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}

	errCh := make(chan error, 1)
	var out agenthub.InvokeResult
	go func() {
		var invokeErr error
		out, invokeErr = svc.Invoke(context.Background(), agenthub.InvokeInput{
			SessionID: sess.ID,
			Prompt:    "NEED_PERMISSION",
		})
		errCh <- invokeErr
	}()

	var pending agenthub.PendingPermission
	select {
	case pending = <-notifyCh:
	case <-time.After(2 * time.Second):
		t.Fatal("waiter did not notify")
	}

	// UI Deny sends cancelled outcome with nil resolver error — must still fail closed.
	deny := json.RawMessage(`{"outcome":{"outcome":"cancelled"}}`)
	if err := waiter.Complete(pending.ID, deny); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-errCh:
		if !errors.Is(err, agenthub.ErrPermissionDenied) {
			t.Fatalf("invoke err = %v want ErrPermissionDenied (out=%+v)", err, out)
		}
		if out.Status != "error" {
			t.Fatalf("status=%q want error", out.Status)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for invoke")
	}
}

func TestPermissionCancelledOutcomeFailsTurn(t *testing.T) {
	svc := newTestService(t)
	svc.ResolvePermission = func(ctx context.Context, sessionID string, params json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`{"outcome":{"outcome":"cancelled"}}`), nil
	}
	sess, err := svc.CreateSession(context.Background(), agenthub.CreateInput{
		AgentType: agenthub.AgentTypeMock,
		Cwd:       t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := svc.Invoke(context.Background(), agenthub.InvokeInput{
		SessionID: sess.ID,
		Prompt:    "NEED_PERMISSION",
	})
	if !errors.Is(err, agenthub.ErrPermissionDenied) {
		t.Fatalf("got %v want ErrPermissionDenied", err)
	}
	if out.Status != "error" {
		t.Fatalf("status=%q want error", out.Status)
	}
}

// The UI renders the deadline; it cannot infer one, because a toast may mount
// long after the request arrived. So the request has to carry when it closes.
func TestPendingPermissionCarriesDeadline(t *testing.T) {
	var seen agenthub.PendingPermission
	w := agenthub.NewPermissionWaiter(func(p agenthub.PendingPermission) { seen = p })
	w.Timeout = 30 * time.Second

	done := make(chan struct{})
	go func() {
		defer close(done)
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		_, _ = w.Wait(ctx, "sess-1", json.RawMessage(`{"options":[]}`))
	}()
	<-done

	if seen.CreatedAt.IsZero() {
		t.Fatal("CreatedAt not stamped")
	}
	if seen.ExpiresAt.IsZero() {
		t.Fatal("ExpiresAt not stamped; the UI would have to invent a deadline")
	}
	if got := seen.ExpiresAt.Sub(seen.CreatedAt); got != 30*time.Second {
		t.Fatalf("window = %v, want the waiter's configured timeout", got)
	}
}

func TestPendingPermissionDeadlineDefaultsToInteractionTimeout(t *testing.T) {
	var seen agenthub.PendingPermission
	w := agenthub.NewPermissionWaiter(func(p agenthub.PendingPermission) { seen = p })

	done := make(chan struct{})
	go func() {
		defer close(done)
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		_, _ = w.Wait(ctx, "sess-2", json.RawMessage(`{"options":[]}`))
	}()
	<-done

	if got := seen.ExpiresAt.Sub(seen.CreatedAt); got != interaction.DefaultTimeout {
		t.Fatalf("window = %v, want interaction.DefaultTimeout", got)
	}
}
