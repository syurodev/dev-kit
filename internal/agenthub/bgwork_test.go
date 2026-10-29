package agenthub

import "testing"

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
