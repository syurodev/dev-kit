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

func turnTask(id string) *AsyncTask { return &AsyncTask{ID: id, TaskType: "turn"} }

func TestBackgroundWorkSnapshotCountsTurnApartFromOtherTasks(t *testing.T) {
	cases := []struct {
		name  string
		tasks []*AsyncTask
		want  SessionActivity
	}{
		{"none", nil, SessionActivity{}},
		{"one shell", []*AsyncTask{shellTask("a")}, SessionActivity{Tasks: 1}},
		{"two shells", []*AsyncTask{shellTask("a"), shellTask("b")}, SessionActivity{Tasks: 2}},
		{"a turn alone", []*AsyncTask{turnTask("autonomous-turn-1")}, SessionActivity{AgentTurn: true}},
		{"a turn beside a shell", []*AsyncTask{turnTask("autonomous-turn-1"), shellTask("a")}, SessionActivity{AgentTurn: true, Tasks: 1}},
		{"an unnamed type is a task", []*AsyncTask{{ID: "x"}}, SessionActivity{Tasks: 1}},
		{"another type is a task", []*AsyncTask{{ID: "x", TaskType: "workflow"}}, SessionActivity{Tasks: 1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var b backgroundWork
			for _, task := range tc.tasks {
				b.apply("s1", UpdateAsyncTaskSpawned, task)
			}
			if got := b.snapshot("s1"); got != tc.want {
				t.Fatalf("snapshot = %+v, want %+v", got, tc.want)
			}
			if got := b.snapshot("other"); !got.Zero() {
				t.Fatalf("another session's snapshot = %+v", got)
			}
		})
	}
}

func TestSessionActivityZero(t *testing.T) {
	if !(SessionActivity{}).Zero() || (SessionActivity{AgentTurn: true}).Zero() || (SessionActivity{Tasks: 1}).Zero() {
		t.Fatal("Zero must be true only for no turn and no task")
	}
}

func TestBackgroundWorkApplyReportsWhetherTheSnapshotChanged(t *testing.T) {
	var b backgroundWork
	steps := []struct {
		name string
		do   func() activityChange
		want bool
	}{
		{"a shell spawn", func() activityChange { return b.apply("s1", UpdateAsyncTaskSpawned, shellTask("a")) }, true},
		{"the same spawn again", func() activityChange { return b.apply("s1", UpdateAsyncTaskSpawned, shellTask("a")) }, false},
		{"progress", func() activityChange { return b.apply("s1", UpdateAsyncTaskProgress, shellTask("a")) }, false},
		{"a still-running state", func() activityChange { return b.apply("s1", UpdateAsyncTaskState, taskState("a", "running")) }, false},
		{"a turn spawn", func() activityChange { return b.apply("s1", UpdateAsyncTaskSpawned, turnTask("autonomous-turn-1")) }, true},
		{"an end for an unknown task", func() activityChange { return b.apply("s1", UpdateAsyncTaskState, taskState("zzz", "completed")) }, false},
		{"the shell ends", func() activityChange { return b.apply("s1", UpdateAsyncTaskState, taskState("a", "completed")) }, true},
		{"the turn ends", func() activityChange {
			return b.apply("s1", UpdateAsyncTaskState, taskState("autonomous-turn-1", "completed"))
		}, true},
		{"the turn ends again", func() activityChange {
			return b.apply("s1", UpdateAsyncTaskState, taskState("autonomous-turn-1", "completed"))
		}, false},
		{"a nil task", func() activityChange { return b.apply("s1", UpdateAsyncTaskSpawned, nil) }, false},
	}
	for _, st := range steps {
		if got := st.do().Changed(); got != st.want {
			t.Fatalf("%s: changed = %v, want %v", st.name, got, st.want)
		}
	}
}

func TestBackgroundWorkApplyReportsBeforeAndAfter(t *testing.T) {
	var b backgroundWork
	b.apply("s1", UpdateAsyncTaskSpawned, shellTask("a"))
	ch := b.apply("s1", UpdateAsyncTaskSpawned, turnTask("autonomous-turn-1"))
	if ch.Before != (SessionActivity{Tasks: 1}) || ch.After != (SessionActivity{AgentTurn: true, Tasks: 1}) {
		t.Fatalf("change = %+v", ch)
	}
}

func TestBackgroundWorkForgetReportsWhetherItChangedAnything(t *testing.T) {
	var b backgroundWork
	if b.forget("s1").Changed() {
		t.Fatal("forgetting a session with no work reported a change")
	}
	b.apply("s1", UpdateAsyncTaskSpawned, turnTask("autonomous-turn-1"))
	ch := b.forget("s1")
	if !ch.Changed() || !ch.Before.AgentTurn || !ch.After.Zero() {
		t.Fatalf("forget change = %+v", ch)
	}
}

// A stale record clears the indicator on its own: the snapshot does not count it,
// and forgetting it is not a change a window would see.
func TestBackgroundWorkSnapshotDoesNotCountAStaleTask(t *testing.T) {
	clock := time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC)
	b := backgroundWork{now: func() time.Time { return clock }}
	b.apply("s1", UpdateAsyncTaskSpawned, shellTask("old"))
	b.apply("s1", UpdateAsyncTaskSpawned, turnTask("autonomous-turn-1"))
	clock = clock.Add(backgroundWorkMaxAge + time.Minute)
	if got := b.snapshot("s1"); !got.Zero() {
		t.Fatalf("a task older than the limit shows as activity: %+v", got)
	}
	if b.forget("s1").Changed() {
		t.Fatal("forgetting only stale tasks changed what the window shows")
	}
}

func TestBackgroundWorkRevCountsEveryChangeAndOnlyChanges(t *testing.T) {
	var b backgroundWork
	rev := func() uint64 { return b.snapshotRev("s1").Rev }
	if rev() != 0 {
		t.Fatal("a session that never had work has a revision")
	}
	steps := []struct {
		name string
		do   func()
		want uint64
	}{
		{"a shell spawn", func() { b.apply("s1", UpdateAsyncTaskSpawned, shellTask("a")) }, 1},
		{"the same spawn again", func() { b.apply("s1", UpdateAsyncTaskSpawned, shellTask("a")) }, 1},
		{"progress", func() { b.apply("s1", UpdateAsyncTaskProgress, shellTask("a")) }, 1},
		{"a turn spawn", func() { b.apply("s1", UpdateAsyncTaskSpawned, turnTask("autonomous-turn-1")) }, 2},
		{"the turn ends", func() { b.apply("s1", UpdateAsyncTaskState, taskState("autonomous-turn-1", "completed")) }, 3},
		{"the shell ends", func() { b.apply("s1", UpdateAsyncTaskState, taskState("a", "completed")) }, 4},
		{"an end for nothing", func() { b.apply("s1", UpdateAsyncTaskState, taskState("a", "completed")) }, 4},
		{"forget with nothing", func() { b.forget("s1") }, 4},
	}
	for _, st := range steps {
		st.do()
		if got := rev(); got != st.want {
			t.Fatalf("%s: rev = %d, want %d", st.name, got, st.want)
		}
	}
}

// The revision outlives the process: what a window read before the process went away is
// older than the event that cleared it, and has to be seen to be.
func TestBackgroundWorkRevSurvivesForgetAndKeepsCounting(t *testing.T) {
	var b backgroundWork
	b.apply("s1", UpdateAsyncTaskSpawned, shellTask("a")) // 1
	b.forget("s1")                                        // 2
	got := b.snapshotRev("s1")
	if got.Rev != 2 || !got.Zero() {
		t.Fatalf("after forget = %+v", got)
	}
	b.apply("s1", UpdateAsyncTaskSpawned, shellTask("b"))
	if got := b.snapshotRev("s1"); got.Rev != 3 || got.Tasks != 1 {
		t.Fatalf("a new process's task = %+v", got)
	}
	if got := b.snapshotRev("other"); got.Rev != 0 {
		t.Fatalf("another session's revision = %d", got.Rev)
	}
}

func TestBackgroundWorkRevCountsAnExpiryWhenItIsObserved(t *testing.T) {
	clock := time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC)
	b := backgroundWork{now: func() time.Time { return clock }}
	b.apply("s1", UpdateAsyncTaskSpawned, shellTask("a"))
	before := b.snapshotRev("s1").Rev
	clock = clock.Add(backgroundWorkMaxAge + time.Minute)
	after := b.snapshotRev("s1")
	if after.Rev != before+1 || !after.Zero() {
		t.Fatalf("expiry: before %d, after %+v", before, after)
	}
	if again := b.snapshotRev("s1"); again.Rev != after.Rev {
		t.Fatal("an expiry was counted twice")
	}
}

func TestBackgroundWorkEndedSaysWhyTheAgentTurnWentOff(t *testing.T) {
	ended := func(b *backgroundWork) bool { return b.snapshotRev("s1").Ended }
	var b backgroundWork
	b.apply("s1", UpdateAsyncTaskSpawned, turnTask("autonomous-turn-1"))
	if ended(&b) {
		t.Fatal("a turn that is running has ended")
	}
	b.apply("s1", UpdateAsyncTaskState, taskState("autonomous-turn-1", "completed"))
	if !ended(&b) {
		t.Fatal("a turn the vendor ended is not ended")
	}
	b.apply("s1", UpdateAsyncTaskSpawned, turnTask("autonomous-turn-2"))
	if ended(&b) {
		t.Fatal("a new turn is already ended")
	}
	b.forget("s1")
	if ended(&b) {
		t.Fatal("a turn cut off by the process going away is ended")
	}
	// A shell task alone never makes a turn that ended.
	var shell backgroundWork
	shell.apply("s1", UpdateAsyncTaskSpawned, shellTask("a"))
	shell.apply("s1", UpdateAsyncTaskState, taskState("a", "completed"))
	if ended(&shell) {
		t.Fatal("a shell task ending is an agent turn ending")
	}
	// The valve is not the vendor's own end.
	clock := time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC)
	expired := backgroundWork{now: func() time.Time { return clock }}
	expired.apply("s1", UpdateAsyncTaskSpawned, turnTask("autonomous-turn-3"))
	clock = clock.Add(backgroundWorkMaxAge + time.Minute)
	if ended(&expired) {
		t.Fatal("an expired turn is ended")
	}
}
