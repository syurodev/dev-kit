package agenthub

import "sync"

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
type backgroundWork struct {
	mu    sync.Mutex
	tasks map[string]map[string]struct{} // session id -> task ids
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
func (b *backgroundWork) apply(sessionID string, kind UpdateKind, task *AsyncTask) {
	if task == nil || task.ID == "" {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	switch kind {
	case UpdateAsyncTaskSpawned:
		if !taskStillRunning(task.State) {
			return
		}
		if b.tasks == nil {
			b.tasks = make(map[string]map[string]struct{})
		}
		if b.tasks[sessionID] == nil {
			b.tasks[sessionID] = make(map[string]struct{})
		}
		b.tasks[sessionID][task.ID] = struct{}{}
	case UpdateAsyncTaskState:
		if taskStillRunning(task.State) {
			return
		}
		if set := b.tasks[sessionID]; set != nil {
			delete(set, task.ID)
			if len(set) == 0 {
				delete(b.tasks, sessionID)
			}
		}
	}
}

// running reports whether the session's process still has a task.
func (b *backgroundWork) running(sessionID string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.tasks[sessionID]) > 0
}

// forget drops a session's tasks: its process is gone.
func (b *backgroundWork) forget(sessionID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.tasks, sessionID)
}
