package tasks

import (
	"context"
	"testing"
	"time"

	"devkit/internal/storage"
)

// changedCall is one OnChanged invocation, in the order it fired.
type changedCall struct {
	boardID string
	taskID  string
}

// recorder is the test double for Service.OnChanged: every write under test
// should notify through it, ids only, in the order the write happened.
type recorder struct {
	calls []changedCall
}

func (r *recorder) record(boardID, taskID string) {
	r.calls = append(r.calls, changedCall{boardID, taskID})
}

// wired attaches a recorder to a fresh service and returns both.
func wired(t *testing.T) (*Service, *recorder) {
	t.Helper()
	svc, _ := newService(t)
	rec := &recorder{}
	svc.OnChanged = rec.record
	return svc, rec
}

func TestNilOnChangedNeverPanics(t *testing.T) {
	svc, _ := newService(t)
	// OnChanged is left nil, which is the default for every other test in
	// this package — this just says so explicitly, across the choke points a
	// future write path is meant to go through.
	board := newBoard(t, svc)
	task, err := svc.Create(user, CreateInput{BoardID: board.ID, Title: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Transition(context.Background(), user, TransitionInput{TaskID: task.ID, To: "in_progress"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddComment(user, task.ID, "hi"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateBoard(board.ID, func(b *Board) { b.Color = "red" }); err != nil {
		t.Fatal(err)
	}
}

func TestCreateNotifiesItsBoard(t *testing.T) {
	svc, rec := wired(t)
	board := newBoard(t, svc)
	rec.calls = nil // newBoard already notified CreateBoard; only Create matters here.

	task, err := svc.Create(user, CreateInput{BoardID: board.ID, Title: "x"})
	if err != nil {
		t.Fatal(err)
	}
	want := []changedCall{{board.ID, task.ID}}
	if !equalCalls(rec.calls, want) {
		t.Fatalf("calls = %+v, want %+v", rec.calls, want)
	}
}

// A user's own transition goes straight through applyTransition, with no
// gate involved — the plain case mutateAs has to notify without help.
func TestUserTransitionNotifies(t *testing.T) {
	svc, rec := wired(t)
	board := newBoard(t, svc)
	task, err := svc.Create(user, CreateInput{BoardID: board.ID, Title: "x"})
	if err != nil {
		t.Fatal(err)
	}
	rec.calls = nil // Create already notified; only the transition matters here.

	if _, err := svc.Transition(context.Background(), user, TransitionInput{TaskID: task.ID, To: "in_progress"}); err != nil {
		t.Fatal(err)
	}
	want := []changedCall{{board.ID, task.ID}}
	if !equalCalls(rec.calls, want) {
		t.Fatalf("calls = %+v, want %+v", rec.calls, want)
	}
}

// Claim is one write — the session id, the move to the active status and the
// pick in the history together — so one notification, not one per part.
func TestClaimNotifiesOncePerWrite(t *testing.T) {
	svc, rec := wired(t)
	board := newBoard(t, svc)
	task, err := svc.Create(user, CreateInput{BoardID: board.ID, Title: "x"})
	if err != nil {
		t.Fatal(err)
	}
	rec.calls = nil

	if _, err := svc.Claim(agent, task.ID); err != nil {
		t.Fatal(err)
	}
	want := []changedCall{{board.ID, task.ID}}
	if !equalCalls(rec.calls, want) {
		t.Fatalf("calls = %+v, want %+v", rec.calls, want)
	}
}

func TestAddCommentNotifiesTheTasksBoard(t *testing.T) {
	svc, rec := wired(t)
	board := newBoard(t, svc)
	task, err := svc.Create(user, CreateInput{BoardID: board.ID, Title: "x"})
	if err != nil {
		t.Fatal(err)
	}
	rec.calls = nil

	if _, err := svc.AddComment(user, task.ID, "hello"); err != nil {
		t.Fatal(err)
	}
	want := []changedCall{{board.ID, task.ID}}
	if !equalCalls(rec.calls, want) {
		t.Fatalf("calls = %+v, want %+v", rec.calls, want)
	}
}

// Moving a task to another board has to tell both trees: the one it left and
// the one it landed on. Neither refreshing alone is correct.
func TestSetFieldsMovingBoardsNotifiesBoth(t *testing.T) {
	svc, rec := wired(t)
	from := newBoard(t, svc)
	to, err := svc.CreateBoard("Other", "OTH", "green", Workflow{})
	if err != nil {
		t.Fatal(err)
	}
	task, err := svc.Create(user, CreateInput{BoardID: from.ID, Title: "x"})
	if err != nil {
		t.Fatal(err)
	}
	rec.calls = nil

	newBoardID := to.ID
	if _, err := svc.SetFields(user, task.ID, Fields{BoardID: &newBoardID}); err != nil {
		t.Fatal(err)
	}
	want := []changedCall{{to.ID, task.ID}, {from.ID, task.ID}}
	if !equalCalls(rec.calls, want) {
		t.Fatalf("calls = %+v, want %+v", rec.calls, want)
	}
}

func TestBindBoardNotifiesWithNoTaskID(t *testing.T) {
	svc, db := newService(t)
	rec := &recorder{}
	svc.OnChanged = rec.record
	scope := &fakeScope{db: db, sessions: map[string][]string{}, gone: map[string]bool{}, services: map[string]map[string]string{}}
	svc.SetScope(scope, db, storage.TableTaskBoardBindings)
	addWorkspace(t, db, "ws-a")
	board := newBoard(t, svc)
	rec.calls = nil

	if _, err := svc.BindBoard(board.ID, "ws-a"); err != nil {
		t.Fatal(err)
	}
	want := []changedCall{{board.ID, ""}}
	if !equalCalls(rec.calls, want) {
		t.Fatalf("calls = %+v, want %+v", rec.calls, want)
	}
}

// The verify path notifies twice on a pass: the gate's own applyTransition
// closing the task, and the system comment recording the result — both
// through the choke points every other write uses, not a call of their own.
func TestVerifyPassNotifiesTransitionAndComment(t *testing.T) {
	svc, db := newService(t)
	rec := &recorder{}
	svc.OnChanged = rec.record
	notify := &stubNotify{mu: make(chan struct{}, 4)}
	svc.SetGate(NewGate(db, storage.TableTaskVerifyRuns, nil, notify))
	board := newBoard(t, svc)
	project := t.TempDir()
	board, err := svc.UpdateBoard(board.ID, func(b *Board) {
		b.Verify = &Verify{Command: "true", TimeoutSec: 30}
		b.ProjectPath = project
	})
	if err != nil {
		t.Fatal(err)
	}
	task, err := svc.Create(user, CreateInput{BoardID: board.ID, Title: "x"})
	if err != nil {
		t.Fatal(err)
	}
	rec.calls = nil

	if _, err := svc.Transition(context.Background(), agent, TransitionInput{TaskID: task.ID, To: "done"}); err != nil {
		t.Fatal(err)
	}
	waitForNotification(t, notify)

	// Two writes land once the run finishes: the transition to "done" and the
	// system comment recording it. Order between them is fixed by execute().
	want := []changedCall{{board.ID, task.ID}, {board.ID, task.ID}}
	if !equalCalls(rec.calls, want) {
		t.Fatalf("calls = %+v, want %+v", rec.calls, want)
	}
}

func equalCalls(got, want []changedCall) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// OnChanged never runs under the write lock: a subscriber that writes a task from
// inside the notification (a refresh that touches the board, a future hook) would
// otherwise wait for the lock it is standing in.
func TestOnChangedRunsAfterTheWriteLockIsReleased(t *testing.T) {
	svc, _ := newService(t)
	board := newBoard(t, svc)
	task, err := svc.Create(user, CreateInput{BoardID: board.ID, Title: "x"})
	if err != nil {
		t.Fatal(err)
	}
	reentered := make(chan error, 1)
	first := true
	svc.OnChanged = func(_, taskID string) {
		if !first {
			return
		}
		first = false
		// Archive is a metadata write: it takes the same lock as the write that fired this.
		_, err := svc.Archive(user, taskID)
		reentered <- err
	}
	done := make(chan error, 1)
	go func() {
		_, err := svc.Claim(agent, task.ID)
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a write notified while holding the write lock: the subscriber's own write deadlocked")
	}
	if err := <-reentered; err != nil {
		t.Fatalf("the write made from the notification failed: %v", err)
	}
}
