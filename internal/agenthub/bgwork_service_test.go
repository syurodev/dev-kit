package agenthub_test

import (
	"context"
	"sync"
	"testing"
	"time"
)

const (
	spawnShellJSON = `{"sessionUpdate":"async_task_spawned","asyncTaskId":"t1","taskType":"shell","name":"build"}`
	doneShellJSON  = `{"sessionUpdate":"async_task_state_update","asyncTaskId":"t1","state":"completed"}`
	spawnTurnJSON  = `{"sessionUpdate":"async_task_spawned","asyncTaskId":"autonomous-turn-c1514d72","taskType":"turn"}`
)

func TestHandleUpdateTracksAShellTask(t *testing.T) {
	f := newIdleFixture(t)
	f.svc.HandleUpdateForTest(context.Background(), f.kid.ID, []byte(spawnShellJSON))
	if !f.svc.HasBackgroundWorkForTest(f.kid.ID) {
		t.Fatal("a spawned shell task is not tracked")
	}
	f.svc.HandleUpdateForTest(context.Background(), f.kid.ID, []byte(doneShellJSON))
	if f.svc.HasBackgroundWorkForTest(f.kid.ID) {
		t.Fatal("a completed shell task is still tracked")
	}
}

func TestHandleUpdateTracksAnAutonomousTurn(t *testing.T) {
	f := newIdleFixture(t)
	f.svc.HandleUpdateForTest(context.Background(), f.parent.ID, []byte(spawnTurnJSON))
	if !f.svc.HasBackgroundWorkForTest(f.parent.ID) {
		t.Fatal("an agent-started turn is not tracked")
	}
	if f.svc.HasBackgroundWorkForTest(f.kid.ID) {
		t.Fatal("another session's work leaked")
	}
}

func TestClosingASessionForgetsItsBackgroundWork(t *testing.T) {
	f := newIdleFixture(t)
	f.svc.HandleUpdateForTest(context.Background(), f.kid.ID, []byte(spawnShellJSON))
	if err := f.svc.CloseSession(context.Background(), f.kid.ID); err != nil {
		t.Fatal(err)
	}
	if f.svc.HasBackgroundWorkForTest(f.kid.ID) {
		t.Fatal("a closed session still has tracked work")
	}
}

// A task whose end update was lost is ignored after the limit, and releasing the
// process then drops its record along with the process.
func TestReleasingARuntimeForgetsItsStaleBackgroundWork(t *testing.T) {
	f := newIdleFixture(t)
	ctx := context.Background()
	// The activity notifier reads the clock from its own goroutine.
	var clockMu sync.Mutex
	clock := time.Now()
	f.svc.SetBackgroundWorkClockForTest(func() time.Time { clockMu.Lock(); defer clockMu.Unlock(); return clock })
	f.svc.HandleUpdateForTest(ctx, f.kid.ID, []byte(spawnShellJSON))
	clockMu.Lock()
	clock = clock.Add(13 * time.Hour)
	clockMu.Unlock()
	if !f.svc.ReleaseRuntimeForTest(ctx, f.kid.ID, time.Now().Add(11*time.Minute), 10*time.Minute) {
		t.Fatal("a stale task blocked the release")
	}
	if n := f.svc.BackgroundWorkRecordedForTest(f.kid.ID); n != 0 {
		t.Fatalf("%d task records survive the process that ran them", n)
	}
}

func TestClosingASessionDropsItsReopenMark(t *testing.T) {
	f := newIdleFixture(t)
	f.svc.MarkReopenForGrantForTest(f.kid.ID)
	if err := f.svc.CloseSession(context.Background(), f.kid.ID); err != nil {
		t.Fatal(err)
	}
	if f.svc.ReopenMarkedForTest(f.kid.ID) {
		t.Fatal("a closed session is still marked for a reopen")
	}
}
