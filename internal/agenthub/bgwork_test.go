package agenthub

import (
	"testing"
	"time"
)

func shellTask(id string) *AsyncTask    { return &AsyncTask{ID: id, TaskType: "shell"} }
func taskState(id, s string) *AsyncTask { return &AsyncTask{ID: id, State: s} }

func TestBackgroundWorkEndedStatesRemoveTheTask(t *testing.T) {
	for _, state := range []string{"completed", "failed", "stopped", "cancelled"} {
		t.Run(state, func(t *testing.T) {
			var b backgroundWork
			b.apply("s1", UpdateAsyncTaskSpawned, shellTask("t1"))
			if !b.running("s1") {
				t.Fatal("a spawned task must count as running")
			}
			b.apply("s1", UpdateAsyncTaskState, taskState("t1", state))
			if b.running("s1") {
				t.Fatalf("a %s task still counts as running", state)
			}
		})
	}
}

func TestBackgroundWorkRunningAndPendingStatesKeepTheTask(t *testing.T) {
	for _, state := range []string{"", "running", "pending"} {
		var b backgroundWork
		b.apply("s1", UpdateAsyncTaskSpawned, shellTask("t1"))
		b.apply("s1", UpdateAsyncTaskState, taskState("t1", state))
		if !b.running("s1") {
			t.Fatalf("state %q ended the task", state)
		}
	}
}

func TestBackgroundWorkProgressNeitherAddsNorRemoves(t *testing.T) {
	var b backgroundWork
	b.apply("s1", UpdateAsyncTaskProgress, shellTask("t1"))
	if b.running("s1") {
		t.Fatal("a progress update alone must not start tracking a task")
	}
	b.apply("s1", UpdateAsyncTaskSpawned, shellTask("t1"))
	b.apply("s1", UpdateAsyncTaskProgress, shellTask("t1"))
	if !b.running("s1") {
		t.Fatal("a progress update ended the task")
	}
}

func TestBackgroundWorkCountsEveryTaskOfASession(t *testing.T) {
	var b backgroundWork
	b.apply("s1", UpdateAsyncTaskSpawned, shellTask("t1"))
	b.apply("s1", UpdateAsyncTaskSpawned, &AsyncTask{ID: "autonomous-turn-1", TaskType: "turn"})
	b.apply("s1", UpdateAsyncTaskState, taskState("t1", "completed"))
	if !b.running("s1") {
		t.Fatal("the autonomous turn is still running")
	}
	b.apply("s1", UpdateAsyncTaskState, taskState("autonomous-turn-1", "completed"))
	if b.running("s1") {
		t.Fatal("both tasks ended")
	}
}

func TestBackgroundWorkIsPerSessionAndForgetClears(t *testing.T) {
	var b backgroundWork
	b.apply("s1", UpdateAsyncTaskSpawned, shellTask("t1"))
	if b.running("s2") {
		t.Fatal("another session's task counted")
	}
	b.forget("s1")
	if b.running("s1") {
		t.Fatal("forget left the task")
	}
}

func TestBackgroundWorkIgnoresATaskWithoutAnID(t *testing.T) {
	var b backgroundWork
	b.apply("s1", UpdateAsyncTaskSpawned, &AsyncTask{})
	b.apply("s1", UpdateAsyncTaskSpawned, nil)
	if b.running("s1") {
		t.Fatal("an anonymous task started tracking")
	}
}

func TestBackgroundWorkIgnoresATaskOlderThanTheLimit(t *testing.T) {
	clock := time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC)
	b := backgroundWork{now: func() time.Time { return clock }}
	b.apply("s1", UpdateAsyncTaskSpawned, shellTask("t1"))

	clock = clock.Add(backgroundWorkMaxAge - time.Minute)
	if !b.running("s1") {
		t.Fatal("a task younger than the limit stopped counting")
	}
	clock = clock.Add(2 * time.Minute)
	if b.running("s1") {
		t.Fatal("a task older than the limit still blocks a release: a lost end update would pin the process for good")
	}
}

func TestBackgroundWorkAYoungTaskKeepsCountingBesideAnOldOne(t *testing.T) {
	clock := time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC)
	b := backgroundWork{now: func() time.Time { return clock }}
	b.apply("s1", UpdateAsyncTaskSpawned, shellTask("old"))
	clock = clock.Add(backgroundWorkMaxAge + time.Hour)
	b.apply("s1", UpdateAsyncTaskSpawned, shellTask("young"))
	if !b.running("s1") {
		t.Fatal("the young task does not count")
	}
	b.apply("s1", UpdateAsyncTaskState, taskState("young", "completed"))
	if b.running("s1") {
		t.Fatal("only the stale task is left and it must not count")
	}
}
