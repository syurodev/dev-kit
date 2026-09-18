package agenthub_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"devkit/internal/agenthub"
)

// sessionStatus reads a session's row, for the tests that care that a refusal
// leaves it usable.
func sessionStatus(t *testing.T, svc *agenthub.Service, id string) agenthub.SessionStatus {
	t.Helper()
	session, err := svc.GetStatus(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return session.Status
}

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
	out, err := svc.Invoke(context.Background(), agenthub.InvokeInput{
		SessionID: sess.ID,
		Prompt:    "NEED_PERMISSION",
	})
	// Nobody to ask means the tool is refused — but refusing a tool is not a
	// broken turn, so the turn ends denied rather than failing.
	if err != nil {
		t.Fatalf("invoke err = %v, want none", err)
	}
	if out.Status != "denied" {
		t.Fatalf("status = %q, want denied", out.Status)
	}
	if got := sessionStatus(t, svc, sess.ID); got != agenthub.SessionStatusIdle {
		t.Fatalf("session status = %q, want idle — the user's own answer did not break it", got)
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
	out, err := svc.Invoke(context.Background(), agenthub.InvokeInput{
		SessionID: sess.ID,
		Prompt:    "NEED_PERMISSION",
	})
	if err != nil {
		t.Fatalf("invoke err = %v, want none", err)
	}
	if out.Status != "denied" {
		t.Fatalf("status = %q, want denied", out.Status)
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

func TestPermissionWaiterDenyDeniesTurn(t *testing.T) {
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
		// Deny is an answer, not a failure: the tool is refused, the turn runs
		// on and ends, and the session stays usable.
		if err != nil {
			t.Fatalf("invoke err = %v, want none (out=%+v)", err, out)
		}
		if out.Status != "denied" {
			t.Fatalf("status = %q, want denied", out.Status)
		}
		if got := sessionStatus(t, svc, sess.ID); got != agenthub.SessionStatusIdle {
			t.Fatalf("session status = %q, want idle", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for invoke")
	}
}

func TestPermissionCancelledOutcomeDeniesTurn(t *testing.T) {
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
	if err != nil {
		t.Fatalf("invoke err = %v, want none", err)
	}
	if out.Status != "denied" {
		t.Fatalf("status = %q, want denied", out.Status)
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

func TestPendingPermissionHasNoDeadlineByDefault(t *testing.T) {
	// A permission is a question put to a person, and the card offers a whole
	// diff to read before answering. Nothing expires unless a timeout is
	// configured; the waiter says so by leaving ExpiresAt zero, which is how
	// the card knows to draw no countdown.
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

	if !seen.ExpiresAt.IsZero() {
		t.Fatalf("ExpiresAt = %v, want zero — nothing closes this gate but the user", seen.ExpiresAt)
	}
	if seen.CreatedAt.IsZero() {
		t.Fatal("CreatedAt not stamped")
	}
}

// TestCancelSessionAnswersWhatTheSessionWasWaitingOn covers the state a stop
// used to leave behind: the agent killed, its question still on screen with a
// live countdown, and the only way out an answer nobody was listening for.
func TestCancelSessionAnswersWhatTheSessionWasWaitingOn(t *testing.T) {
	waiter := agenthub.NewPermissionWaiter(nil)

	type answer struct {
		outcome json.RawMessage
		err     error
	}
	answers := make(chan answer, 2)
	for _, sessionID := range []string{"sess-stopped", "sess-other"} {
		go func(id string) {
			outcome, err := waiter.Wait(context.Background(), id, json.RawMessage(`{"toolCall":{"title":"rm -rf /"}}`))
			if id == "sess-stopped" {
				answers <- answer{outcome, err}
			}
		}(sessionID)
	}

	// Both registrations have to land before the stop, or the test proves
	// nothing about the one it leaves alone.
	deadline := time.After(2 * time.Second)
	for len(waiter.PendingList()) < 2 {
		select {
		case <-deadline:
			t.Fatal("permissions never registered")
		default:
			time.Sleep(time.Millisecond)
		}
	}

	if got := waiter.CancelSession("sess-stopped"); got != 1 {
		t.Fatalf("cancelled %d permissions, want 1", got)
	}

	select {
	case got := <-answers:
		if !errors.Is(got.err, agenthub.ErrPermissionDenied) {
			t.Fatalf("err = %v, want ErrPermissionDenied — a stopped turn must not approve a tool", got.err)
		}
		if !json.Valid(got.outcome) || string(got.outcome) == "" {
			t.Fatalf("outcome = %q, want the denied outcome", got.outcome)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the agent was left blocked on a request nobody will answer")
	}

	// The other session's turn is still going and still needs its answer.
	rest := waiter.PendingList()
	if len(rest) != 1 || rest[0].SessionID != "sess-other" {
		t.Fatalf("pending after cancel = %+v, want only sess-other", rest)
	}
}

// Stopping when nothing is running is not a failure. It used to answer
// ErrUnavailable, which the bridge showed as "Agent Hub is not available" — and
// that is what the second press of Stop always got, since the first press kills
// the process the third would have cancelled.
func TestCancelWithNothingRunningIsNotAnError(t *testing.T) {
	svc := newTestService(t)
	sess, err := svc.CreateSession(context.Background(), agenthub.CreateInput{
		AgentType: agenthub.AgentTypeMock,
		Cwd:       t.TempDir(),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := svc.CloseSession(context.Background(), sess.ID); err != nil {
		t.Fatalf("close: %v", err)
	}

	if err := svc.Cancel(context.Background(), sess.ID); err != nil {
		t.Fatalf("cancel with no runtime = %v, want nil", err)
	}
}
