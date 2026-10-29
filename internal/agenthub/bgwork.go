package agenthub

import (
	"sync"
	"time"
)

// backgroundWork is what each session's vendor process is running that dies
// with the process: a shell task the agent started, or a turn the agent began
// by itself after one finished (taskType "turn", id "autonomous-turn-…").
// Releasing the process kills them, so idle release and the reopen after a
// folder grant look here first.
//
// It is in memory on purpose. A task cannot outlive its process, so a record
// that survived a restart or a release would only be wrong. It is built from
// the vendor's async_task_* updates, so a vendor that sends none (Codex,
// opencode) is never protected by it.
//
// A vendor can lose the update that ends a task, and a record that never ends
// would pin the process (no idle release) for good. So a task counts as running
// for backgroundWorkMaxAge after it was spawned and not after: a shell task or an
// agent-started turn outliving half a day is far less likely than a lost update.
type backgroundWork struct {
	mu    sync.Mutex
	tasks map[string]map[string]bgTask // session id -> task id -> what it is and when it spawned
	now   func() time.Time             // nil: time.Now
}

// bgTask is one tracked task: when it spawned (the age limit counts from it) and
// whether it is a turn the agent began (taskType "turn") or other work.
type bgTask struct {
	spawned time.Time
	turn    bool
}

// taskTypeTurn is the taskType of a turn the agent started by itself.
const taskTypeTurn = "turn"

// activityChange is what one apply or forget did to a session's snapshot.
type activityChange struct{ Before, After SessionActivity }

// Changed reports whether the window would see something different.
func (c activityChange) Changed() bool { return c.Before != c.After }

// backgroundWorkMaxAge is how long after its spawn a task still blocks a release.
const backgroundWorkMaxAge = 12 * time.Hour

// clock is the current time; the caller holds mu or has no other use of now.
func (b *backgroundWork) clock() time.Time {
	if b.now != nil {
		return b.now()
	}
	return time.Now()
}

// taskStillRunning reads a vendor task state the way the window does
// (summaryModel.ts RUNNING_TASK_STATES): "", "running" and "pending" are
// running; any other value ended the task.
func taskStillRunning(state string) bool {
	switch state {
	case "", "running", "pending":
		return true
	}
	return false
}

// apply folds one async_task_* update into the set. Only a spawn adds a task, so
// a late update of a task that was already dropped cannot bring it back.
//
// It reports what the update did to the session's snapshot (see activityChange).
func (b *backgroundWork) apply(sessionID string, kind UpdateKind, task *AsyncTask) activityChange {
	if task == nil || task.ID == "" {
		return activityChange{}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	before := b.snapshotLocked(sessionID)
	switch kind {
	case UpdateAsyncTaskSpawned:
		if !taskStillRunning(task.State) {
			break
		}
		if b.tasks == nil {
			b.tasks = make(map[string]map[string]bgTask)
		}
		if b.tasks[sessionID] == nil {
			b.tasks[sessionID] = make(map[string]bgTask)
		}
		b.tasks[sessionID][task.ID] = bgTask{spawned: b.clock(), turn: task.TaskType == taskTypeTurn}
	case UpdateAsyncTaskState:
		if taskStillRunning(task.State) {
			break
		}
		if set := b.tasks[sessionID]; set != nil {
			delete(set, task.ID)
			if len(set) == 0 {
				delete(b.tasks, sessionID)
			}
		}
	}
	return activityChange{Before: before, After: b.snapshotLocked(sessionID)}
}

// running reports whether the session's process still has a task that is younger
// than backgroundWorkMaxAge.
func (b *backgroundWork) running(sessionID string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.clock()
	for _, task := range b.tasks[sessionID] {
		if now.Sub(task.spawned) < backgroundWorkMaxAge {
			return true
		}
	}
	return false
}

// snapshot is what the session's process is doing that no DevKit turn started.
// A task past backgroundWorkMaxAge does not count, so an indicator cannot outlive
// the valve that frees the process.
func (b *backgroundWork) snapshot(sessionID string) SessionActivity {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.snapshotLocked(sessionID)
}

func (b *backgroundWork) snapshotLocked(sessionID string) SessionActivity {
	var a SessionActivity
	now := b.clock()
	for _, task := range b.tasks[sessionID] {
		if now.Sub(task.spawned) >= backgroundWorkMaxAge {
			continue
		}
		if task.turn {
			a.AgentTurn = true
		} else {
			a.Tasks++
		}
	}
	return a
}

// forget drops a session's tasks: its process is gone. It reports what that did
// to the snapshot; a session whose tasks had all gone stale changed nothing.
func (b *backgroundWork) forget(sessionID string) activityChange {
	b.mu.Lock()
	defer b.mu.Unlock()
	before := b.snapshotLocked(sessionID)
	delete(b.tasks, sessionID)
	return activityChange{Before: before}
}
