package agenthub_test

import (
	"context"
	"errors"
	"testing"

	"devkit/internal/agenthub"
)

// closedFixture is a session that was closed: no runtime, status closed.
func closedFixture(t *testing.T) (*idleFixture, string) {
	t.Helper()
	f := newIdleFixture(t)
	if err := f.svc.CloseSession(context.Background(), f.kid.ID); err != nil {
		t.Fatal(err)
	}
	if f.hasRuntime(f.kid.ID) || f.status(t, f.kid.ID) != agenthub.SessionStatusClosed {
		t.Fatal("fixture: the closed session still has a runtime or is not closed")
	}
	return f, f.kid.ID
}

// A window whose copy of the row is stale (a parent closed the child through MCP)
// asks to prepare it. Waking a process that no prompt is about to use left a
// sandbox alive for a session the row calls closed: nothing releases a closed
// session's runtime, so it lived until DevKit quit.
func TestPrepareSessionRefusesAClosedSessionWithoutOpeningAProcess(t *testing.T) {
	f, id := closedFixture(t)
	if err := f.svc.PrepareSession(context.Background(), id); !errors.Is(err, agenthub.ErrSessionClosed) {
		t.Fatalf("PrepareSession err = %v, want ErrSessionClosed", err)
	}
	if f.hasRuntime(id) {
		t.Fatal("PrepareSession opened a process for a closed session")
	}
	if got := f.status(t, id); got != agenthub.SessionStatusClosed {
		t.Fatalf("status = %q, want closed", got)
	}
}

// SetGoal is a settings call like SetMode: it must not wake a closed session.
func TestSetGoalRefusesAClosedSessionWithoutOpeningAProcess(t *testing.T) {
	f, id := closedFixture(t)
	if err := f.svc.SetGoal(context.Background(), id, "ship it"); !errors.Is(err, agenthub.ErrSessionClosed) {
		t.Fatalf("SetGoal err = %v, want ErrSessionClosed", err)
	}
	if f.hasRuntime(id) {
		t.Fatal("SetGoal opened a process for a closed session")
	}
}

// A closed session has no turn to steer: the answer is the one an idle session
// gets, send it as a prompt (which reopens it), and no process is opened to say so.
func TestSteerOnAClosedSessionAsksForAPromptWithoutOpeningAProcess(t *testing.T) {
	f, id := closedFixture(t)
	outcome, err := f.svc.Steer(context.Background(), id, "hello")
	if err != nil {
		t.Fatal(err)
	}
	if outcome != agenthub.SteerPromptRequired {
		t.Fatalf("outcome = %q, want promptRequired", outcome)
	}
	if f.hasRuntime(id) {
		t.Fatal("Steer opened a process for a closed session")
	}
}

// By design: the next prompt reopens a closed session and it runs.
func TestAPromptStillReopensAClosedSession(t *testing.T) {
	f, id := closedFixture(t)
	out, err := f.svc.Invoke(context.Background(), agenthub.InvokeInput{SessionID: id, Prompt: "back again"})
	if err != nil {
		t.Fatalf("invoke on a closed session: %v", err)
	}
	if out.Status != "completed" || !f.hasRuntime(id) {
		t.Fatalf("status = %q, runtime = %v: a prompt must reopen a closed session", out.Status, f.hasRuntime(id))
	}
	if got := f.status(t, id); got == agenthub.SessionStatusClosed {
		t.Fatal("the session stayed closed after a prompt")
	}
}
