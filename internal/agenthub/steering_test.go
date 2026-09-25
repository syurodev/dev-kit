package agenthub_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"devkit/internal/agenthub"
)

// A steered message goes into the turn that is running — the thing that
// separates steering from the prompt queue, which waits for the turn to end.
func TestSteerReachesTheRunningTurn(t *testing.T) {
	svc := newTestService(t)
	steered := make(chan struct{})
	svc.ResolvePermission = func(ctx context.Context, sessionID string, params json.RawMessage) (json.RawMessage, error) {
		// The turn parks here, which is when a user would type.
		<-steered
		return json.RawMessage(`{"outcome":{"outcome":"selected","optionId":"allow"}}`), nil
	}
	sess, err := svc.CreateSession(context.Background(), agenthub.CreateInput{
		AgentType: agenthub.AgentTypeMock,
		Cwd:       t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		_, invokeErr := svc.Invoke(context.Background(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "NEED_PERMISSION"})
		done <- invokeErr
	}()

	// Wait for the turn to be in flight before steering.
	deadline := time.After(2 * time.Second)
	for {
		status, err := svc.GetStatus(context.Background(), sess.ID)
		if err != nil {
			t.Fatal(err)
		}
		if status.Status == agenthub.SessionStatusAwaitingPermission {
			break
		}
		select {
		case <-deadline:
			t.Fatal("turn never started")
		default:
			time.Sleep(time.Millisecond)
		}
	}

	outcome, err := svc.Steer(context.Background(), sess.ID, "actually, skip the tests")
	if err != nil {
		t.Fatal(err)
	}
	if outcome != agenthub.SteerInjected {
		t.Fatalf("outcome = %q, want injected", outcome)
	}
	close(steered)
	if err := <-done; err != nil {
		t.Fatal(err)
	}

	// The agent never echoes a steered message, so the transcript would lose
	// it unless the hub writes it down.
	page, err := svc.TranscriptPage(context.Background(), sess.ID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range page.Entries {
		if strings.Contains(string(entry.Payload), "actually, skip the tests") {
			found = true
		}
	}
	if !found {
		t.Fatal("the steered message is not in the transcript")
	}
}

// An idle session takes a prompt, not a steer: a turn started behind the hub's
// back has no record, no checkpoint and no Stop button.
func TestSteerOnAnIdleSessionAsksForAPrompt(t *testing.T) {
	svc := newTestService(t)
	sess, err := svc.CreateSession(context.Background(), agenthub.CreateInput{
		AgentType: agenthub.AgentTypeMock,
		Cwd:       t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := svc.Steer(context.Background(), sess.ID, "hello")
	if err != nil {
		t.Fatal(err)
	}
	if outcome != agenthub.SteerPromptRequired {
		t.Fatalf("outcome = %q, want promptRequired", outcome)
	}
	page, err := svc.TranscriptPage(context.Background(), sess.ID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range page.Entries {
		if strings.Contains(string(entry.Payload), "hello") {
			t.Fatal("nothing was sent, so nothing should be recorded")
		}
	}
}

func TestSteerRefusesAnEmptyMessage(t *testing.T) {
	svc := newTestService(t)
	sess, err := svc.CreateSession(context.Background(), agenthub.CreateInput{
		AgentType: agenthub.AgentTypeMock,
		Cwd:       t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Steer(context.Background(), sess.ID, "   "); !errors.Is(err, agenthub.ErrInvalidPrompt) {
		t.Fatalf("err = %v, want ErrInvalidPrompt", err)
	}
}

func TestSetGoalAndClearItReachTheAgent(t *testing.T) {
	adapter := agenthub.NewMockAdapter()
	svc := newTestServiceWith(t, adapter)
	sess, err := svc.CreateSession(context.Background(), agenthub.CreateInput{
		AgentType: agenthub.AgentTypeMock,
		Cwd:       t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.SetGoal(context.Background(), sess.ID, "  a green build  "); err != nil {
		t.Fatal(err)
	}
	rt := adapter.LastRuntime()
	if got := rt.Goal(); got != "a green build" {
		t.Fatalf("goal = %q, want it trimmed", got)
	}
	if err := svc.SetGoal(context.Background(), sess.ID, ""); err != nil {
		t.Fatal(err)
	}
	if got := rt.Goal(); got != "" {
		t.Fatalf("goal = %q, want cleared", got)
	}
}

// Answering a permission lets the turn go on, whichever way it was answered:
// the agent is told yes or no and keeps working. The session has to say
// running again for that stretch — a denial used to leave it
// awaiting_permission until the turn ended, which took the running
// indicator off an agent that was still writing.
func TestAnsweredPermissionPutsTheTurnBackToRunning(t *testing.T) {
	answers := map[string]string{
		"allow": `{"outcome":{"outcome":"selected","optionId":"allow"}}`,
		"deny":  `{"outcome":{"outcome":"selected","optionId":"deny"}}`,
		// The card dismissed rather than answered: the gate fails closed and
		// the agent is refused, but the turn still goes on.
		"cancelled": `{"outcome":{"outcome":"cancelled"}}`,
	}
	for name, answer := range answers {
		t.Run(name, func(t *testing.T) {
			svc := newTestService(t)
			answered := make(chan struct{})
			svc.ResolvePermission = func(ctx context.Context, sessionID string, params json.RawMessage) (json.RawMessage, error) {
				defer close(answered)
				return json.RawMessage(answer), nil
			}
			sess, err := svc.CreateSession(context.Background(), agenthub.CreateInput{
				AgentType: agenthub.AgentTypeMock,
				Cwd:       t.TempDir(),
			})
			if err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() {
				_, invokeErr := svc.Invoke(context.Background(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "NEED_PERMISSION HOLD_AFTER"})
				done <- invokeErr
			}()
			<-answered

			deadline := time.After(2 * time.Second)
			for {
				status, err := svc.GetStatus(context.Background(), sess.ID)
				if err != nil {
					t.Fatal(err)
				}
				if status.Status == agenthub.SessionStatusRunning {
					break
				}
				select {
				case <-deadline:
					t.Fatalf("status = %q while the turn is still going, want running", status.Status)
				default:
					time.Sleep(time.Millisecond)
				}
			}

			if err := svc.Cancel(context.Background(), sess.ID); err != nil {
				t.Fatal(err)
			}
			if err := <-done; err != nil && !errors.Is(err, agenthub.ErrCancelled) {
				t.Fatal(err)
			}
		})
	}
}
