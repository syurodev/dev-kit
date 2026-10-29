package agenthub_test

import (
	"context"
	"testing"
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
