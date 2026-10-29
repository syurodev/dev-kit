# Agent write-grant request: implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or
> superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** An agent session can ask the person for write access to one folder of its own workspace
(`workspace.requestWrite`); the grant is stored on the session and the session's process is reopened at the end of
the turn when that is safe. The same background-work guard also stops idle release from killing a session that
still has a vendor task running.

**Architecture:** Spec `docs/superpowers/specs/2026-10-29-agent-write-grant-design.md` (read it first, sections 2 to
8). `agenthub.Service` owns the checks (`PrepareWorkspaceWrite`, `CommitWorkspaceWrite`) and the store write
(`ExtraPathStore`, compare-and-set). The MCP tool owns the confirmation (the `peerConfirm` pattern). An in-memory
`backgroundWork` set, fed from `handleUpdate`, guards both idle release and the reopen after a grant.

**Tech Stack:** Go 1.26, SQLite (`modernc`, via `internal/storage`), the MCP Go SDK
(`github.com/modelcontextprotocol/go-sdk/mcp`), `internal/interaction.Hub`.

Base: `origin/main` at `cf5a884a` (harness active in production). Work in a worktree on a new branch from the
branch that carries this plan; the plan and spec stay in the same pull request.

## Conventions for every task

- Test command, always with the memory cap (never `go test ./...`):

  ```bash
  systemd-run --user --scope -q -p MemoryMax=4G timeout 600 env GOMEMLIMIT=2GiB \
    go test -p 1 -count=1 -test.timeout 300s ./internal/agenthub -run '<Name>'
  ```

- Commit trailer: `Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>`.
- Agents commit locally only. The coordinator pushes and opens the pull request; the owner merges.
- Never touch `~/.config/DevKit`, never read credential files, no real accounts.
- Test helpers that already exist (do not redefine): `newIdleFixture`, `markPrompted`, `testIdlePolicy`
  (`idle_release_test.go`); `newIdentityService`, `agentCtx`, `uiCtx`, `projectDir`, `rowExtras`
  (`service_identity_test.go`, `service_inherit_extras_test.go`); `newServiceForPreviewTest`
  (`preview_service_test.go`); `Service.RuntimeForTest`, `SetLastUsedForTest` (`export_test.go`); in `internal/mcp`:
  `fakeCaller`, `fakeAgentHub`, `agentHubTestDeps`, `workspaceServer`, `unwrap`; in `internal/app`:
  `briefWorkspaces`, `briefFixture` (`agentbrief_test.go`).

## File map

| File | Change |
| --- | --- |
| `internal/agenthub/bgwork.go` (new) | `backgroundWork`: per-session set of running vendor task ids |
| `internal/agenthub/bgwork_test.go` (new) | unit tests of the set (package `agenthub`) |
| `internal/agenthub/bgwork_service_test.go` (new) | `handleUpdate` feeds the set; `CloseSession` clears it |
| `internal/agenthub/service.go` | fields `bgwork`, `reopenForGrant`, hook `WorkspaceFolders`; `handleUpdate` calls `bgwork.apply`; `Invoke`'s deferred block calls `reopenAfterGrant`; `forget` where runtimes are deleted |
| `internal/agenthub/idle_release.go` | skip a session with background work (sweep and final check) |
| `internal/agenthub/store.go` | `ExtraPathStore`, `ErrExtraPathsContended`, `SQLStore.AddExtraReadWritePath` |
| `internal/agenthub/extra_path_store_test.go` (new) | store tests |
| `internal/agenthub/workspace_write.go` (new) | types, `PrepareWorkspaceWrite`, `CommitWorkspaceWrite`, `scheduleReopenForGrant` |
| `internal/agenthub/workspace_write_test.go` (new) | service tests |
| `internal/agenthub/reopen_grant.go` (new) | `reopenAfterGrant`, `releasableNow` |
| `internal/agenthub/reopen_grant_test.go` (new) | reopen tests |
| `internal/agenthub/export_test.go` | test accessors |
| `internal/agenthub/environment.go` | wording of the writable-folder row |
| `internal/app/agentfolders.go` (new), `agentfolders_test.go` (new) | adapter `AgentWorkspaceFolders` |
| `internal/mcp/tools_workspace.go`, `server.go` | `WorkspaceWriteAPI`, `Deps.WorkspaceWrite`, the tool |
| `internal/mcp/tools_workspace_write_test.go` (new) | tool tests |
| `internal/mcp/tools_git.go`, `internal/mcp/git_repo.go` | `git.repoNotGranted` wording |
| `internal/bridge/errors.go` | three codes in `keptCodes` |
| `internal/app/container.go` | the `ws:grant` capability grant |
| `main.go` | wire `WorkspaceFolders` and `Deps.WorkspaceWrite` |
| docs | `agent-hub-vendors-sessions.md`, `mcp-tools.md`, `agent-hub-coordination.md` |

---

### Task 0: Confirm what the spec left open (no code)

**Files:** none changed; findings go into the final task's commit message and the pull request body.

- [ ] **Step 1: No grant cache between the store and a running session's `git.*` scope**

  Run: `grep -n 'ExtraReadWritePaths' internal/mcp/git_repo.go internal/mcp/tools_git.go internal/mcp/tools_workspace.go`
  Then read `internal/mcp/git_repo.go` lines 60-100 (where `scope` is built).
  Expected: the scope is built from `AgentHub.GetStatus(ctx, caller.SessionID)` on every call. If a cache sits in
  between (a map of sessions kept by the MCP server or the endpoint), stop and tell the owner: the grant would not be
  seen until the cache turns over.

- [ ] **Step 2: The window refetches the session when a turn finishes**

  Run: `grep -rn 'OnTurnFinished' main.go internal/app/*.go | head` and read the handler it points to.
  Expected: it emits an event the window answers by refetching the session list. If it does not refetch the row
  (only status), the Environment view will keep showing the old list until the next refresh; note it for the pull
  request as a known limit and do not add an event in this change.

- [ ] **Step 3: Codex and opencode rebuild the jail from the stored row**

  Run: `grep -n 'ExtraReadWritePaths' internal/agenthub/adapter_codex.go internal/agenthub/adapter_opencode.go internal/agenthub/adapter_claude.go internal/agenthub/service.go | head -20`
  Expected: `resumeRuntime` builds the `OpenSpec` from the stored row for every vendor (the field is read from
  `sess.ExtraReadWritePaths`, not from a vendor-specific copy). Record the answer. If a vendor does not, the tool's
  answer must use `after_reopen` for it: add that case to `scheduleReopenForGrant` in Task 4 behind a vendor check.

- [ ] **Step 4: Where `delete(s.runtimes` happens**

  Run: `grep -n 'delete(s.runtimes' internal/agenthub/*.go | grep -v _test`
  Expected: the list used in Task 1 Step 7. Write it down.

No commit.

---

### Task 1: A per-session set of running vendor tasks

**Files:**
- Create: `internal/agenthub/bgwork.go`, `internal/agenthub/bgwork_test.go`, `internal/agenthub/bgwork_service_test.go`
- Modify: `internal/agenthub/service.go` (fields, `handleUpdate`, `delete(s.runtimes` sites), `internal/agenthub/export_test.go`

- [ ] **Step 1: Write the failing unit tests**

  Create `internal/agenthub/bgwork_test.go`:

  ```go
  package agenthub

  import "testing"

  func shellTask(id string) *AsyncTask   { return &AsyncTask{ID: id, TaskType: "shell"} }
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
  ```

- [ ] **Step 2: Run to verify they fail**

  Run the test command with `-run 'TestBackgroundWork'`. Expected: build failure, `undefined: backgroundWork`.

- [ ] **Step 3: Implement the set**

  Create `internal/agenthub/bgwork.go`:

  ```go
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
  ```

- [ ] **Step 4: Run the unit tests**

  Run with `-run 'TestBackgroundWork'`. Expected: PASS (6 tests).

- [ ] **Step 5: Add the field and feed it from `handleUpdate`**

  In `internal/agenthub/service.go`, in the `Service` struct next to `lastUsed` (around line 312) add:

  ```go
  	// bgwork is the vendor tasks each live process is still running (bgwork.go).
  	bgwork backgroundWork
  ```

  In `handleUpdate`, directly after `parsed := ParseSessionUpdate(update)` add:

  ```go
  	if parsed.AsyncTask != nil {
  		s.bgwork.apply(sessionID, parsed.Kind, parsed.AsyncTask)
  	}
  ```

- [ ] **Step 6: Test accessors**

  Append to `internal/agenthub/export_test.go`:

  ```go
  // HandleUpdateForTest feeds one session/update through the path a runtime's updates take.
  func (s *Service) HandleUpdateForTest(sessionID string, update json.RawMessage) {
  	s.handleUpdate(context.Background(), sessionID, update)
  }

  // HasBackgroundWorkForTest reports whether the session's process still has a tracked vendor task.
  func (s *Service) HasBackgroundWorkForTest(sessionID string) bool { return s.bgwork.running(sessionID) }
  ```

- [ ] **Step 7: Forget where a runtime is deleted**

  For each site listed in Task 0 Step 4 (`delete(s.runtimes, …)` outside `releaseRuntime`), add
  `s.bgwork.forget(<id>)` on the line after it, under the same lock discipline (the set has its own mutex, so it is
  safe to call with `s.mu` held). Known sites: `liveRuntime`'s dead-process branch (`service.go` near line 2625) and
  the teardown half of `CloseSession`. Leave `releaseRuntime` alone: Task 2 stops it from releasing a session that
  has tasks, so there is nothing to forget there.

- [ ] **Step 8: Write the service-level tests**

  Create `internal/agenthub/bgwork_service_test.go`:

  ```go
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
  	f.svc.HandleUpdateForTest(f.kid.ID, []byte(spawnShellJSON))
  	if !f.svc.HasBackgroundWorkForTest(f.kid.ID) {
  		t.Fatal("a spawned shell task is not tracked")
  	}
  	f.svc.HandleUpdateForTest(f.kid.ID, []byte(doneShellJSON))
  	if f.svc.HasBackgroundWorkForTest(f.kid.ID) {
  		t.Fatal("a completed shell task is still tracked")
  	}
  }

  func TestHandleUpdateTracksAnAutonomousTurn(t *testing.T) {
  	f := newIdleFixture(t)
  	f.svc.HandleUpdateForTest(f.parent.ID, []byte(spawnTurnJSON))
  	if !f.svc.HasBackgroundWorkForTest(f.parent.ID) {
  		t.Fatal("an agent-started turn is not tracked")
  	}
  	if f.svc.HasBackgroundWorkForTest(f.kid.ID) {
  		t.Fatal("another session's work leaked")
  	}
  }

  func TestClosingASessionForgetsItsBackgroundWork(t *testing.T) {
  	f := newIdleFixture(t)
  	f.svc.HandleUpdateForTest(f.kid.ID, []byte(spawnShellJSON))
  	if err := f.svc.CloseSession(context.Background(), f.kid.ID); err != nil {
  		t.Fatal(err)
  	}
  	if f.svc.HasBackgroundWorkForTest(f.kid.ID) {
  		t.Fatal("a closed session still has tracked work")
  	}
  }
  ```

- [ ] **Step 9: Run both test files**

  Run with `-run 'TestBackgroundWork|TestHandleUpdateTracks|TestClosingASessionForgets'`. Expected: PASS.

- [ ] **Step 10: Commit**

  ```bash
  git add internal/agenthub/bgwork.go internal/agenthub/bgwork_test.go internal/agenthub/bgwork_service_test.go \
          internal/agenthub/service.go internal/agenthub/export_test.go
  git commit -m "feat(agenthub): track the vendor tasks a session's process is still running"
  ```

---

### Task 2: Idle release leaves a session with background work alone

Its own commit: it fixes a bug that exists on `main` (a long build killed 10 or 30 minutes after the turn ended).

**Files:**
- Modify: `internal/agenthub/idle_release.go`, `internal/agenthub/idle_release_test.go`

- [ ] **Step 1: Write the failing test**

  Append to `internal/agenthub/idle_release_test.go`:

  ```go
  func TestReleaseIdleKeepsASessionWithBackgroundWork(t *testing.T) {
  	f := newIdleFixture(t)
  	ctx := context.Background()
  	now := time.Now().Add(11 * time.Minute) // past the child's 10 minutes, short of the parent's 30
  	f.svc.HandleUpdateForTest(f.kid.ID, []byte(spawnShellJSON))
  	if n := f.svc.ReleaseIdle(ctx, now, testIdlePolicy); n != 0 || !f.hasRuntime(f.kid.ID) {
  		t.Fatalf("released %d: a session whose build is still running must keep its process", n)
  	}
  	f.svc.HandleUpdateForTest(f.kid.ID, []byte(doneShellJSON))
  	if n := f.svc.ReleaseIdle(ctx, now, testIdlePolicy); n != 1 || f.hasRuntime(f.kid.ID) {
  		t.Fatalf("released %d: once the build ended the idle session should go", n)
  	}
  }
  ```

- [ ] **Step 2: Run to verify it fails**

  Run with `-run 'TestReleaseIdleKeepsASessionWithBackgroundWork'`. Expected: FAIL, "released 1: a session whose
  build is still running must keep its process".

- [ ] **Step 3: Add the guard in both places**

  In `ReleaseIdle`'s candidate loop (around line 108) change

  ```go
  		if s.runtimes[sess.ID] == nil || s.resuming[sess.ID] != nil || activeChild[sess.ID] {
  ```

  to

  ```go
  		if s.runtimes[sess.ID] == nil || s.resuming[sess.ID] != nil || activeChild[sess.ID] || s.bgwork.running(sess.ID) {
  ```

  In `releaseRuntime` (around line 212) change

  ```go
  	if rt == nil || s.resuming[id] != nil || now.Sub(s.lastUsed[id]) < after {
  ```

  to

  ```go
  	if rt == nil || s.resuming[id] != nil || now.Sub(s.lastUsed[id]) < after || s.bgwork.running(id) {
  ```

  Also add to the doc comment of `ReleaseIdle` one sentence: "A session whose process still has a vendor task
  (bgwork.go) is kept: releasing the process would kill it."

- [ ] **Step 4: Run the test and the whole idle file**

  Run with `-run 'TestReleaseIdle|TestAReleasedSession'`. Expected: PASS.

- [ ] **Step 5: Mutation**

  Remove `|| s.bgwork.running(sess.ID)` from the sweep only, run Step 4's first test. Expected: FAIL. Restore it and
  confirm `git diff --stat` shows only the intended files. Repeat for the `releaseRuntime` copy: this one has no
  direct test, so add this to the test file first, then mutate:

  ```go
  func TestReleaseRuntimeRechecksBackgroundWorkUnderTheLock(t *testing.T) {
  	f := newIdleFixture(t)
  	ctx := context.Background()
  	now := time.Now().Add(11 * time.Minute)
  	// The work starts after the sweep chose its candidates and before the release.
  	f.svc.HandleUpdateForTest(f.kid.ID, []byte(spawnShellJSON))
  	if released := f.svc.ReleaseRuntimeForTest(ctx, f.kid.ID, now, 10*time.Minute); released {
  		t.Fatal("releaseRuntime released a session that has a task")
  	}
  }
  ```

  with, in `export_test.go`:

  ```go
  // ReleaseRuntimeForTest runs the final, locked half of an idle release on one session.
  func (s *Service) ReleaseRuntimeForTest(ctx context.Context, id string, now time.Time, after time.Duration) bool {
  	sess, err := s.store.GetSession(ctx, id)
  	if err != nil {
  		return false
  	}
  	return s.releaseRuntime(ctx, sess, now, after)
  }
  ```

  Mutate the `releaseRuntime` guard, expect FAIL, restore.

- [ ] **Step 6: Commit**

  ```bash
  git add internal/agenthub/idle_release.go internal/agenthub/idle_release_test.go internal/agenthub/export_test.go
  git commit -m "fix(agenthub): idle release keeps a session whose vendor still runs a task"
  ```

---

### Task 3: Add one folder to a session's grant, compare-and-set

**Files:**
- Modify: `internal/agenthub/store.go`
- Create: `internal/agenthub/extra_path_store_test.go`

- [ ] **Step 1: Write the failing tests**

  Create `internal/agenthub/extra_path_store_test.go`:

  ```go
  package agenthub_test

  import (
  	"context"
  	"errors"
  	"fmt"
  	"slices"
  	"sync"
  	"testing"

  	"devkit/internal/agenthub"
  )

  func TestAddExtraReadWritePathAppendsOnceAndKeepsWhatWasThere(t *testing.T) {
  	svc, st := newServiceForPreviewTest(t, agenthub.NewMockAdapter())
  	ctx := context.Background()
  	first := t.TempDir()
  	sess, err := svc.CreateSession(ctx, agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: t.TempDir(), ExtraReadWritePaths: []string{first}})
  	if err != nil {
  		t.Fatal(err)
  	}
  	added := t.TempDir()
  	if ok, err := st.AddExtraReadWritePath(ctx, sess.ID, added); err != nil || !ok {
  		t.Fatalf("first add = %v, %v", ok, err)
  	}
  	if ok, err := st.AddExtraReadWritePath(ctx, sess.ID, added); err != nil || ok {
  		t.Fatalf("second add = %v, %v; want false (already listed)", ok, err)
  	}
  	got, err := st.GetSession(ctx, sess.ID)
  	if err != nil {
  		t.Fatal(err)
  	}
  	if want := []string{first, added}; !slices.Equal(got.ExtraReadWritePaths, want) {
  		t.Fatalf("extras = %v, want %v", got.ExtraReadWritePaths, want)
  	}
  }

  func TestAddExtraReadWritePathToAnUnknownSession(t *testing.T) {
  	_, st := newServiceForPreviewTest(t, agenthub.NewMockAdapter())
  	if _, err := st.AddExtraReadWritePath(context.Background(), "no-such-session", "/w/x"); !errors.Is(err, agenthub.ErrSessionNotFound) {
  		t.Fatalf("err = %v, want ErrSessionNotFound", err)
  	}
  }

  func TestAddExtraReadWritePathDoesNotLoseARacingWriter(t *testing.T) {
  	svc, st := newServiceForPreviewTest(t, agenthub.NewMockAdapter())
  	ctx := context.Background()
  	sess, err := svc.CreateSession(ctx, agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: t.TempDir()})
  	if err != nil {
  		t.Fatal(err)
  	}
  	const n = 6
  	var wg sync.WaitGroup
  	for i := range n {
  		wg.Add(1)
  		go func() {
  			defer wg.Done()
  			if _, err := st.AddExtraReadWritePath(ctx, sess.ID, fmt.Sprintf("/w/folder-%d", i)); err != nil {
  				t.Errorf("add %d: %v", i, err)
  			}
  		}()
  	}
  	wg.Wait()
  	got, err := st.GetSession(ctx, sess.ID)
  	if err != nil {
  		t.Fatal(err)
  	}
  	if len(got.ExtraReadWritePaths) != n {
  		t.Fatalf("extras = %v, want all %d folders", got.ExtraReadWritePaths, n)
  	}
  }
  ```

- [ ] **Step 2: Run to verify they fail**

  Run with `-run 'TestAddExtraReadWritePath'`. Expected: build failure, `st.AddExtraReadWritePath undefined`.

- [ ] **Step 3: Implement**

  In `internal/agenthub/store.go`, after `decodePathList`, add (and add `"errors"` to the imports if missing):

  ```go
  // ErrExtraPathsContended is AddExtraReadWritePath giving up: the row kept changing
  // under it. Each failed try means another writer succeeded, so this takes more
  // writers than tries.
  var ErrExtraPathsContended = errors.New("agenthub: the session's folder grants kept changing")

  // ExtraPathStore adds one folder to a session's write grant. It is optional so a
  // test Store need not implement it; SQLStore does.
  type ExtraPathStore interface {
  	// AddExtraReadWritePath appends path to the session's extra_rw_paths and reports
  	// whether it did (false: the folder was already listed). The write is a
  	// compare-and-set on the stored text, so two callers each add their own folder.
  	AddExtraReadWritePath(ctx context.Context, id, path string) (added bool, err error)
  }

  const addExtraPathTries = 10

  func (st *SQLStore) AddExtraReadWritePath(ctx context.Context, id, path string) (bool, error) {
  	for range addExtraPathTries {
  		var raw string
  		err := st.db.QueryRowContext(ctx, `SELECT extra_rw_paths FROM `+tableAgentSessions+` WHERE id = ?`, id).Scan(&raw)
  		if errors.Is(err, sql.ErrNoRows) {
  			return false, ErrSessionNotFound
  		}
  		if err != nil {
  			return false, err
  		}
  		paths, err := decodePathList(raw)
  		if err != nil {
  			return false, err
  		}
  		if slices.Contains(paths, path) {
  			return false, nil
  		}
  		res, err := st.db.ExecContext(ctx,
  			`UPDATE `+tableAgentSessions+` SET extra_rw_paths = ? WHERE id = ? AND extra_rw_paths = ?`,
  			encodePathList(append(paths, path)), id, raw)
  		if err != nil {
  			return false, err
  		}
  		if n, err := res.RowsAffected(); err != nil {
  			return false, err
  		} else if n == 1 {
  			return true, nil
  		}
  	}
  	return false, ErrExtraPathsContended
  }
  ```

  Add `"slices"` to the imports if missing. The row's `updated_at` is deliberately not touched: a grant is not
  session activity, and idle release reads `updated_at`.

- [ ] **Step 4: Run**

  Run with `-run 'TestAddExtraReadWritePath'`. Expected: PASS. Run once more with `-race` added to the `go test`
  flags for the racing test.

- [ ] **Step 5: Commit**

  ```bash
  git add internal/agenthub/store.go internal/agenthub/extra_path_store_test.go
  git commit -m "feat(agenthub): add one folder to a session's write grant with a compare-and-set"
  ```

---

### Task 4: The owner's checks: prepare and commit

**Files:**
- Create: `internal/agenthub/workspace_write.go`, `internal/agenthub/workspace_write_test.go`
- Modify: `internal/agenthub/service.go` (hook field, `reopenForGrant` field), `internal/agenthub/export_test.go`

- [ ] **Step 1: Fields**

  In `internal/agenthub/service.go`, next to `MatchingWorkspaces` (around line 379):

  ```go
  	// WorkspaceFolders names a workspace and lists its folders (services). It is
  	// what a request for write access is checked against. nil: such a request
  	// is refused.
  	WorkspaceFolders func(workspaceID string) (name string, folders []WorkspaceFolder, err error)
  ```

  next to `bgwork`:

  ```go
  	// reopenForGrant marks sessions whose process should be reopened when the
  	// current turn ends because a folder was just added to their grant (mu).
  	reopenForGrant map[string]bool
  ```

- [ ] **Step 2: Write the failing tests**

  Create `internal/agenthub/workspace_write_test.go`:

  ```go
  package agenthub_test

  import (
  	"context"
  	"errors"
  	"os"
  	"path/filepath"
  	"slices"
  	"testing"

  	"devkit/internal/agenthub"
  	"devkit/internal/identity"
  )

  type grantRig struct {
  	svc                  *agenthub.Service
  	root                 agenthub.AgentSession
  	primary, audit, away string
  	folders              []agenthub.WorkspaceFolder
  }

  func newGrantRig(t *testing.T) *grantRig {
  	t.Helper()
  	svc, _, _ := newIdentityService(t)
  	base := t.TempDir()
  	r := &grantRig{svc: svc, primary: filepath.Join(base, "event-router"), audit: filepath.Join(base, "audit"), away: filepath.Join(base, "elsewhere")}
  	for _, d := range []string{r.primary, r.audit, r.away} {
  		if err := os.MkdirAll(d, 0o755); err != nil {
  			t.Fatal(err)
  		}
  	}
  	r.folders = []agenthub.WorkspaceFolder{{Name: "event-router", Path: r.primary}, {Name: "fsap-audit-service", Path: r.audit}}
  	svc.MatchingWorkspaces = func(cwd string) ([]string, error) {
  		if cwd == r.away {
  			return nil, nil
  		}
  		return []string{"ws1"}, nil
  	}
  	svc.WorkspaceFolders = func(id string) (string, []agenthub.WorkspaceFolder, error) {
  		if id != "ws1" {
  			return "", nil, nil
  		}
  		return "fsap-event", r.folders, nil
  	}
  	root, err := svc.CreateSession(uiCtx(), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: r.primary, WorkspaceID: "ws1"})
  	if err != nil {
  		t.Fatal(err)
  	}
  	r.root = root
  	return r
  }

  func isRefused(err error) bool {
  	var refused *agenthub.GrantRefusedError
  	return errors.As(err, &refused)
  }

  func TestPrepareWorkspaceWritePlansAFolderOfTheWorkspace(t *testing.T) {
  	r := newGrantRig(t)
  	plan, err := r.svc.PrepareWorkspaceWrite(agentCtx(r.root.ID), "fsap-audit-service")
  	if err != nil {
  		t.Fatal(err)
  	}
  	if plan.Path != r.audit || plan.WorkspaceName != "fsap-event" || plan.SessionID != r.root.ID || plan.AlreadyGranted {
  		t.Fatalf("plan = %+v", plan)
  	}
  }

  func TestPrepareWorkspaceWriteNotesAFolderAlreadyWritable(t *testing.T) {
  	r := newGrantRig(t)
  	plan, err := r.svc.PrepareWorkspaceWrite(agentCtx(r.root.ID), "event-router") // the session's own cwd
  	if err != nil || !plan.AlreadyGranted {
  		t.Fatalf("plan = %+v, err = %v; the cwd is already writable", plan, err)
  	}
  }

  func TestPrepareWorkspaceWriteRefusals(t *testing.T) {
  	r := newGrantRig(t)
  	loose, err := r.svc.CreateSession(uiCtx(), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: r.away})
  	if err != nil {
  		t.Fatal(err)
  	}
  	kid, err := r.svc.CreateSession(agentCtx(r.root.ID), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock})
  	if err != nil {
  		t.Fatal(err)
  	}
  	closed, err := r.svc.CreateSession(uiCtx(), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: r.audit, WorkspaceID: "ws1"})
  	if err != nil {
  		t.Fatal(err)
  	}
  	if err := r.svc.CloseSession(uiCtx(), closed.ID); err != nil {
  		t.Fatal(err)
  	}
  	home, _ := os.UserHomeDir()
  	cases := []struct {
  		name   string
  		ctx    context.Context
  		folder string
  		mutate func()
  	}{
  		{"a terminal caller", identity.WithCaller(context.Background(), identity.UserTerminal()), "fsap-audit-service", nil},
  		{"the window", uiCtx(), "fsap-audit-service", nil},
  		{"no folder named", agentCtx(r.root.ID), "  ", nil},
  		{"a folder that is not in the workspace", agentCtx(r.root.ID), "not-a-service", nil},
  		{"a sub-session", agentCtx(kid.ID), "fsap-audit-service", nil},
  		{"a session outside every workspace", agentCtx(loose.ID), "fsap-audit-service", nil},
  		{"a closed session", agentCtx(closed.ID), "fsap-audit-service", nil},
  		{"the filesystem root", agentCtx(r.root.ID), "fsap-audit-service", func() {
  			r.folders[1].Path = "/"
  		}},
  		{"the home folder", agentCtx(r.root.ID), "fsap-audit-service", func() {
  			r.folders[1].Path = home
  		}},
  	}
  	for _, c := range cases {
  		t.Run(c.name, func(t *testing.T) {
  			saved := r.folders[1].Path
  			t.Cleanup(func() { r.folders[1].Path = saved })
  			if c.mutate != nil {
  				c.mutate()
  			}
  			if _, err := r.svc.PrepareWorkspaceWrite(c.ctx, c.folder); !isRefused(err) {
  				t.Fatalf("err = %v, want a *GrantRefusedError", err)
  			}
  		})
  	}
  }

  func TestCommitWorkspaceWriteStoresTheFolder(t *testing.T) {
  	r := newGrantRig(t)
  	ctx := agentCtx(r.root.ID)
  	plan, err := r.svc.PrepareWorkspaceWrite(ctx, "fsap-audit-service")
  	if err != nil {
  		t.Fatal(err)
  	}
  	res, err := r.svc.CommitWorkspaceWrite(ctx, plan)
  	if err != nil {
  		t.Fatal(err)
  	}
  	if !res.Granted || res.AlreadyGranted || res.Effective.Git != agenthub.EffectNow || res.Effective.NewSessions != agenthub.EffectNow ||
  		res.Effective.ShellAndFiles != agenthub.EffectAfterThisTurn {
  		t.Fatalf("result = %+v", res)
  	}
  	if got := rowExtras(t, r.svc, r.root.ID); !slices.Equal(got, []string{r.audit}) {
  		t.Fatalf("stored extras = %v", got)
  	}
  	// Asking again changes nothing and asks nobody.
  	again, err := r.svc.PrepareWorkspaceWrite(ctx, "fsap-audit-service")
  	if err != nil || !again.AlreadyGranted {
  		t.Fatalf("second plan = %+v, %v", again, err)
  	}
  	res, err = r.svc.CommitWorkspaceWrite(ctx, again)
  	if err != nil || !res.AlreadyGranted || len(rowExtras(t, r.svc, r.root.ID)) != 1 {
  		t.Fatalf("second commit = %+v, %v", res, err)
  	}
  }

  func TestCommitWorkspaceWriteRefusesAnotherSessionsPlan(t *testing.T) {
  	r := newGrantRig(t)
  	plan, err := r.svc.PrepareWorkspaceWrite(agentCtx(r.root.ID), "fsap-audit-service")
  	if err != nil {
  		t.Fatal(err)
  	}
  	other, err := r.svc.CreateSession(uiCtx(), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: r.audit, WorkspaceID: "ws1"})
  	if err != nil {
  		t.Fatal(err)
  	}
  	if _, err := r.svc.CommitWorkspaceWrite(agentCtx(other.ID), plan); !isRefused(err) {
  		t.Fatalf("err = %v, want a refusal", err)
  	}
  	if got := rowExtras(t, r.svc, r.root.ID); len(got) != 0 {
  		t.Fatalf("a refused commit stored %v", got)
  	}
  }

  func TestCommitWorkspaceWriteRechecksAfterTheWait(t *testing.T) {
  	t.Run("the service was removed from the workspace", func(t *testing.T) {
  		r := newGrantRig(t)
  		ctx := agentCtx(r.root.ID)
  		plan, _ := r.svc.PrepareWorkspaceWrite(ctx, "fsap-audit-service")
  		r.folders = r.folders[:1]
  		r.svc.WorkspaceFolders = func(string) (string, []agenthub.WorkspaceFolder, error) { return "fsap-event", r.folders, nil }
  		if _, err := r.svc.CommitWorkspaceWrite(ctx, plan); !isRefused(err) {
  			t.Fatalf("err = %v, want a refusal", err)
  		}
  		if got := rowExtras(t, r.svc, r.root.ID); len(got) != 0 {
  			t.Fatalf("stored %v", got)
  		}
  	})
  	t.Run("the service now points elsewhere", func(t *testing.T) {
  		r := newGrantRig(t)
  		ctx := agentCtx(r.root.ID)
  		plan, _ := r.svc.PrepareWorkspaceWrite(ctx, "fsap-audit-service")
  		r.folders[1].Path = r.away
  		if _, err := r.svc.CommitWorkspaceWrite(ctx, plan); !isRefused(err) {
  			t.Fatalf("err = %v, want a refusal: the person approved another folder", err)
  		}
  		if got := rowExtras(t, r.svc, r.root.ID); len(got) != 0 {
  			t.Fatalf("stored %v", got)
  		}
  	})
  	t.Run("the session was closed", func(t *testing.T) {
  		r := newGrantRig(t)
  		ctx := agentCtx(r.root.ID)
  		plan, _ := r.svc.PrepareWorkspaceWrite(ctx, "fsap-audit-service")
  		if err := r.svc.CloseSession(uiCtx(), r.root.ID); err != nil {
  			t.Fatal(err)
  		}
  		if _, err := r.svc.CommitWorkspaceWrite(ctx, plan); !isRefused(err) {
  			t.Fatalf("err = %v, want a refusal", err)
  		}
  	})
  }

  // A sub-session keeps the copy it took when it was created; one created after the
  // grant takes the parent's new list (service.go inheritedExtraPaths).
  func TestAGrantReachesOnlyChildrenCreatedAfterIt(t *testing.T) {
  	r := newGrantRig(t)
  	ctx := agentCtx(r.root.ID)
  	before, err := r.svc.CreateSession(ctx, agenthub.CreateInput{AgentType: agenthub.AgentTypeMock})
  	if err != nil {
  		t.Fatal(err)
  	}
  	plan, _ := r.svc.PrepareWorkspaceWrite(ctx, "fsap-audit-service")
  	if _, err := r.svc.CommitWorkspaceWrite(ctx, plan); err != nil {
  		t.Fatal(err)
  	}
  	after, err := r.svc.CreateSession(ctx, agenthub.CreateInput{AgentType: agenthub.AgentTypeMock})
  	if err != nil {
  		t.Fatal(err)
  	}
  	if got := rowExtras(t, r.svc, before.ID); len(got) != 0 {
  		t.Fatalf("the earlier child has %v: a grant must not widen an existing child", got)
  	}
  	if got := rowExtras(t, r.svc, after.ID); !slices.Equal(got, []string{r.audit}) {
  		t.Fatalf("the later child has %v, want the parent's list", got)
  	}
  }
  ```

- [ ] **Step 3: Run to verify they fail**

  Run with `-run 'TestPrepareWorkspaceWrite|TestCommitWorkspaceWrite|TestAGrantReaches'`. Expected: build failure
  (`undefined: agenthub.WorkspaceFolder`, `PrepareWorkspaceWrite`, `EffectNow`...).

- [ ] **Step 4: Implement**

  Create `internal/agenthub/workspace_write.go`:

  ```go
  package agenthub

  import (
  	"context"
  	"errors"
  	"fmt"
  	"os"
  	"path/filepath"
  	"strings"

  	"devkit/internal/identity"
  )

  // WorkspaceFolder is one folder (service) of a workspace as a write request sees it.
  type WorkspaceFolder struct {
  	Name string
  	Path string
  }

  // GrantRefusedError is a request for folder access that cannot be granted. Reason
  // is a fixed sentence: it names no path, and no folder the caller did not name.
  type GrantRefusedError struct{ Reason string }

  func (e *GrantRefusedError) Error() string { return e.Reason }

  func refuseGrant(reason string) error { return &GrantRefusedError{Reason: reason} }

  // WriteGrantPlan is what the person is asked to approve, built by
  // PrepareWorkspaceWrite and checked again, whole, by CommitWorkspaceWrite.
  type WriteGrantPlan struct {
  	SessionID      string
  	WorkspaceID    string
  	WorkspaceName  string
  	Folder         string
  	Path           string
  	AlreadyGranted bool
  }

  // When each reader sees a granted folder (WriteGrantEffect).
  const (
  	EffectNow           = "now"
  	EffectAfterThisTurn = "after_this_turn" // DevKit will try to reopen the process when the turn ends
  	EffectAfterReopen   = "after_reopen"    // DevKit cannot now; the next reopen applies it
  )

  // WriteGrantEffect says when git, new sub-sessions, and the session's own shell and
  // file tools see the folder.
  type WriteGrantEffect struct {
  	Git           string `json:"git"`
  	NewSessions   string `json:"newSessions"`
  	ShellAndFiles string `json:"shellAndFiles"`
  }

  // WriteGrantResult is what the agent is told.
  type WriteGrantResult struct {
  	Folder         string           `json:"folder"`
  	Granted        bool             `json:"granted"`
  	AlreadyGranted bool             `json:"alreadyGranted"`
  	Effective      WriteGrantEffect `json:"effective"`
  }

  // PrepareWorkspaceWrite runs every check of a request for write access to one folder
  // of the caller's own workspace and returns what the person should be asked. It
  // stores nothing. See docs/superpowers/specs/2026-10-29-agent-write-grant-design.md.
  func (s *Service) PrepareWorkspaceWrite(ctx context.Context, folder string) (WriteGrantPlan, error) {
  	caller, _ := identity.FromContext(ctx)
  	if !caller.IsAgent() {
  		return WriteGrantPlan{}, refuseGrant("Only an agent session can ask for access to a folder.")
  	}
  	return s.planWriteGrant(ctx, caller.SessionID, strings.TrimSpace(folder))
  }

  func (s *Service) planWriteGrant(ctx context.Context, sessionID, folder string) (WriteGrantPlan, error) {
  	if folder == "" {
  		return WriteGrantPlan{}, refuseGrant("Say which folder: pass its name as workspace.list shows it.")
  	}
  	sess, err := s.store.GetSession(ctx, sessionID)
  	if errors.Is(err, ErrSessionNotFound) {
  		return WriteGrantPlan{}, refuseGrant("This session no longer exists.")
  	}
  	if err != nil {
  		return WriteGrantPlan{}, err
  	}
  	switch {
  	case sess.Status == SessionStatusClosed:
  		return WriteGrantPlan{}, refuseGrant("This session is closed.")
  	case sess.ParentSessionID != nil:
  		return WriteGrantPlan{}, refuseGrant("A sub-session cannot hold more folders than its parent. Ask your parent session to request it, or ask the person.")
  	case sess.WorkspaceID == "":
  		return WriteGrantPlan{}, refuseGrant("This session is not in a workspace.")
  	case s.WorkspaceFolders == nil:
  		return WriteGrantPlan{}, refuseGrant("Workspace folders are not available.")
  	}
  	workspaceName, folders, err := s.WorkspaceFolders(sess.WorkspaceID)
  	if err != nil {
  		return WriteGrantPlan{}, err
  	}
  	path := ""
  	for _, f := range folders {
  		if f.Name == folder {
  			path = filepath.Clean(f.Path)
  			break
  		}
  	}
  	if path == "" || path == "." {
  		return WriteGrantPlan{}, refuseGrant(fmt.Sprintf("%q is not a folder of this workspace. Call workspace.list to see its folders.", folder))
  	}
  	home, _ := os.UserHomeDir()
  	if !filepath.IsAbs(path) || path == string(filepath.Separator) || (home != "" && path == filepath.Clean(home)) {
  		return WriteGrantPlan{}, refuseGrant("That folder cannot be granted.")
  	}
  	writable := append([]string{sess.ProjectPath}, sess.ExtraReadWritePaths...)
  	return WriteGrantPlan{
  		SessionID: sessionID, WorkspaceID: sess.WorkspaceID, WorkspaceName: workspaceName,
  		Folder: folder, Path: path, AlreadyGranted: folderCovered(path, writable),
  	}, nil
  }

  // folderCovered reports whether path is one of roots or inside one. A folder whose
  // name merely starts like a root's does not count.
  func folderCovered(path string, roots []string) bool {
  	path = filepath.Clean(path)
  	for _, root := range roots {
  		if root == "" {
  			continue
  		}
  		root = filepath.Clean(root)
  		if path == root || strings.HasPrefix(path, root+string(filepath.Separator)) {
  			return true
  		}
  	}
  	return false
  }

  // CommitWorkspaceWrite stores a folder the person approved. The wait for the answer can
  // be long, so every check runs again against the stored session and the current
  // workspace; a folder that is no longer the one the person saw is refused.
  func (s *Service) CommitWorkspaceWrite(ctx context.Context, plan WriteGrantPlan) (WriteGrantResult, error) {
  	caller, _ := identity.FromContext(ctx)
  	if !caller.IsAgent() || caller.SessionID != plan.SessionID {
  		return WriteGrantResult{}, refuseGrant("This approval belongs to another session.")
  	}
  	now, err := s.planWriteGrant(ctx, plan.SessionID, plan.Folder)
  	if err != nil {
  		return WriteGrantResult{}, err
  	}
  	if now.Path != plan.Path {
  		return WriteGrantResult{}, refuseGrant("The folder changed while the person was answering. Ask again.")
  	}
  	result := WriteGrantResult{Folder: plan.Folder, Granted: true, Effective: WriteGrantEffect{Git: EffectNow, NewSessions: EffectNow}}
  	if now.AlreadyGranted {
  		result.AlreadyGranted = true
  		result.Effective.ShellAndFiles = EffectNow
  		return result, nil
  	}
  	store, ok := s.store.(ExtraPathStore)
  	if !ok {
  		return WriteGrantResult{}, errors.New("agenthub: this store cannot add a folder grant")
  	}
  	if _, err := store.AddExtraReadWritePath(ctx, plan.SessionID, now.Path); err != nil {
  		if errors.Is(err, ErrExtraPathsContended) {
  			return WriteGrantResult{}, refuseGrant("The session's folders kept changing. Ask again.")
  		}
  		return WriteGrantResult{}, err
  	}
  	result.Effective.ShellAndFiles = s.scheduleReopenForGrant(ctx, plan.SessionID)
  	return result, nil
  }

  // scheduleReopenForGrant marks the session so its process is reopened when the
  // turn ends (reopen_grant.go), and says whether DevKit expects to. A vendor that
  // cannot load a session again is never marked: releasing it would end the conversation.
  func (s *Service) scheduleReopenForGrant(ctx context.Context, id string) string {
  	sess, err := s.store.GetSession(ctx, id)
  	if err != nil || !s.reopenable(ctx, sess) {
  		return EffectAfterReopen
  	}
  	s.mu.Lock()
  	if s.reopenForGrant == nil {
  		s.reopenForGrant = make(map[string]bool)
  	}
  	s.reopenForGrant[id] = true
  	s.mu.Unlock()
  	return EffectAfterThisTurn
  }
  ```

  If Task 0 Step 3 found a vendor whose reopen does not rebuild the jail from the stored row, return
  `EffectAfterReopen` for that vendor at the top of `scheduleReopenForGrant`.

- [ ] **Step 5: Run**

  Run with `-run 'TestPrepareWorkspaceWrite|TestCommitWorkspaceWrite|TestAGrantReaches'`. Expected: PASS. If
  `CreateSession` for a child returns a model-choice error under an agent context, copy the setup used by
  `TestAgentChildInheritsParentExtraPaths` (it creates the child with `agentCtx(parent.ID)` and no other fields,
  as above), which already passes on `main`.

- [ ] **Step 6: Mutation checks**

  Run each, expect the named test to FAIL, restore, and check `git status` is clean of strays:
  1. Delete the `sess.ParentSessionID != nil` case: `TestPrepareWorkspaceWriteRefusals/a_sub-session`.
  2. In `CommitWorkspaceWrite` delete the `now.Path != plan.Path` block: `.../the_service_now_points_elsewhere`.
  3. Replace `planWriteGrant(ctx, plan.SessionID, plan.Folder)` in `CommitWorkspaceWrite` with the plan's values
     (skip the re-check): `.../the_service_was_removed_from_the_workspace`.

- [ ] **Step 7: Commit**

  ```bash
  git add internal/agenthub/workspace_write.go internal/agenthub/workspace_write_test.go internal/agenthub/service.go
  git commit -m "feat(agenthub): check and store a request for write access to a workspace folder"
  ```

---

### Task 5: Reopen the process when the turn ends

**Files:**
- Create: `internal/agenthub/reopen_grant.go`, `internal/agenthub/reopen_grant_test.go`
- Modify: `internal/agenthub/service.go` (`Invoke`'s deferred block), `internal/agenthub/export_test.go`

- [ ] **Step 1: Test accessors**

  Append to `internal/agenthub/export_test.go`:

  ```go
  // MarkReopenForGrantForTest marks a session as the grant tool does.
  func (s *Service) MarkReopenForGrantForTest(sessionID string) {
  	s.mu.Lock()
  	defer s.mu.Unlock()
  	if s.reopenForGrant == nil {
  		s.reopenForGrant = make(map[string]bool)
  	}
  	s.reopenForGrant[sessionID] = true
  }

  // ReopenMarkedForTest reports whether the session is still marked.
  func (s *Service) ReopenMarkedForTest(sessionID string) bool {
  	s.mu.Lock()
  	defer s.mu.Unlock()
  	return s.reopenForGrant[sessionID]
  }

  // ReopenAfterGrantForTest runs the end-of-turn step on its own.
  func (s *Service) ReopenAfterGrantForTest(ctx context.Context, sessionID string) {
  	s.reopenAfterGrant(ctx, sessionID)
  }
  ```

- [ ] **Step 2: Write the failing tests**

  Create `internal/agenthub/reopen_grant_test.go`:

  ```go
  package agenthub_test

  import (
  	"context"
  	"testing"
  	"time"

  	"devkit/internal/agenthub"
  )

  func TestAMarkedSessionIsReleasedAtTheEndOfItsTurn(t *testing.T) {
  	f := newIdleFixture(t)
  	ctx := context.Background()
  	f.svc.MarkReopenForGrantForTest(f.parent.ID)
  	if _, err := f.svc.Invoke(ctx, agenthub.InvokeInput{SessionID: f.parent.ID, Prompt: "hello"}); err != nil {
  		t.Fatal(err)
  	}
  	if f.hasRuntime(f.parent.ID) {
  		t.Fatal("the marked session kept its old process: the folder would not reach its shell")
  	}
  	if f.svc.ReopenMarkedForTest(f.parent.ID) {
  		t.Fatal("the mark outlived its turn")
  	}
  	if got := f.status(t, f.parent.ID); got != agenthub.SessionStatusIdle {
  		t.Fatalf("status = %q: a release must leave the session idle, not closed", got)
  	}
  	if _, err := f.svc.Invoke(ctx, agenthub.InvokeInput{SessionID: f.parent.ID, Prompt: "again"}); err != nil || !f.hasRuntime(f.parent.ID) {
  		t.Fatalf("the next prompt did not reopen the session: %v", err)
  	}
  }

  func TestAnUnmarkedSessionKeepsItsProcess(t *testing.T) {
  	f := newIdleFixture(t)
  	if _, err := f.svc.Invoke(context.Background(), agenthub.InvokeInput{SessionID: f.parent.ID, Prompt: "hello"}); err != nil {
  		t.Fatal(err)
  	}
  	if !f.hasRuntime(f.parent.ID) {
  		t.Fatal("a session nobody marked was released at the end of a turn")
  	}
  }

  func TestTheReopenGuards(t *testing.T) {
  	ctx := context.Background()
  	cases := map[string]func(t *testing.T, f *idleFixture){
  		"a child is running": func(t *testing.T, f *idleFixture) {
  			if err := f.store.UpdateSessionStatus(ctx, f.kid.ID, agenthub.SessionStatusRunning, time.Now().UTC()); err != nil {
  				t.Fatal(err)
  			}
  		},
  		"a vendor task is running": func(t *testing.T, f *idleFixture) {
  			f.svc.HandleUpdateForTest(f.parent.ID, []byte(spawnShellJSON))
  		},
  		"an agent-started turn is running": func(t *testing.T, f *idleFixture) {
  			f.svc.HandleUpdateForTest(f.parent.ID, []byte(spawnTurnJSON))
  		},
  		"a peer message is queued": func(t *testing.T, f *idleFixture) {
  			rec := agenthub.PeerOutboxRecord{ID: "msg-q", From: agenthub.MessageSource{Kind: "session", SessionID: f.kid.ID}, FromSession: f.kid.ID,
  				To: f.parent.ID, At: time.Now().UTC(), UpdatedAt: time.Now().UTC(), State: "queued"}
  			if err := f.store.SavePeerOutbox(ctx, rec); err != nil {
  				t.Fatal(err)
  			}
  		},
  		"the vendor cannot load the session again": func(t *testing.T, f *idleFixture) {
  			markPrompted(t, f.store, f.parent.ID)
  		},
  	}
  	for name, setup := range cases {
  		t.Run(name, func(t *testing.T) {
  			f := newIdleFixture(t)
  			setup(t, f)
  			f.svc.MarkReopenForGrantForTest(f.parent.ID)
  			f.svc.ReopenAfterGrantForTest(ctx, f.parent.ID)
  			if !f.hasRuntime(f.parent.ID) {
  				t.Fatalf("released although %s: that would kill work or lose the conversation", name)
  			}
  			if f.svc.ReopenMarkedForTest(f.parent.ID) {
  				t.Fatal("the mark stayed: a blocked reopen must not retry in a loop")
  			}
  		})
  	}
  }

  func TestAGrantMarksASessionOnlyWhenTheVendorCanReopenIt(t *testing.T) {
  	r := newGrantRig(t)
  	ctx := agentCtx(r.root.ID)
  	plan, _ := r.svc.PrepareWorkspaceWrite(ctx, "fsap-audit-service")
  	res, err := r.svc.CommitWorkspaceWrite(ctx, plan)
  	if err != nil || res.Effective.ShellAndFiles != agenthub.EffectAfterThisTurn {
  		t.Fatalf("result = %+v, %v; a never-prompted mock session can be reopened", res, err)
  	}
  	if !r.svc.ReopenMarkedForTest(r.root.ID) {
  		t.Fatal("the session was not marked")
  	}
  }
  ```

- [ ] **Step 3: Run to verify they fail**

  Run with `-run 'TestAMarked|TestAnUnmarked|TestTheReopenGuards|TestAGrantMarks'`. Expected: build failure
  (`reopenAfterGrant` undefined).

- [ ] **Step 4: Implement**

  Create `internal/agenthub/reopen_grant.go`:

  ```go
  package agenthub

  import (
  	"context"
  	"log"
  	"time"
  )

  // reopenAfterGrant is the end-of-turn half of a folder grant. The grant is already
  // stored; what is left is the session's process, whose jail was built before the
  // folder existed. If the session was marked, release the process (the half of
  // CloseSession that tears it down) when it is safe: the next prompt, peer message
  // or settings call reopens it from the stored row, as after a restart.
  //
  // It is one attempt. A session that is not safe to release keeps its process, the
  // mark is spent, and the folder applies at the next reopen (idle release, a
  // restart). A failed release never undoes the stored grant.
  func (s *Service) reopenAfterGrant(ctx context.Context, id string) {
  	s.mu.Lock()
  	marked := s.reopenForGrant[id]
  	delete(s.reopenForGrant, id)
  	s.mu.Unlock()
  	if !marked {
  		return
  	}
  	sess, err := s.store.GetSession(ctx, id)
  	if err != nil {
  		return
  	}
  	if !s.releasableNow(ctx, sess) {
  		log.Printf("agenthub: session %s keeps its process after a folder grant; the folder applies after the next reopen", id)
  		return
  	}
  	s.releaseRuntime(ctx, sess, time.Now(), 0)
  }

  // releasableNow is the idle-release safety checks without the clock: the session is
  // idle or in error, no child is running or waiting, a process exists and is not being
  // reopened, no vendor task would die with it, nothing is waiting on it (a question,
  // a permission, a peer message on the way), and the vendor can load it again.
  func (s *Service) releasableNow(ctx context.Context, sess AgentSession) bool {
  	if sess.Status != SessionStatusIdle && sess.Status != SessionStatusError {
  		return false
  	}
  	sessions, err := s.store.ListSessions(ctx)
  	if err != nil {
  		return false
  	}
  	for _, other := range sessions {
  		if other.ParentSessionID != nil && *other.ParentSessionID == sess.ID &&
  			(other.Status == SessionStatusRunning || other.Status == SessionStatusAwaitingPermission) {
  			return false
  		}
  	}
  	s.mu.Lock()
  	live := s.runtimes[sess.ID] != nil && s.resuming[sess.ID] == nil
  	s.mu.Unlock()
  	if !live || s.bgwork.running(sess.ID) {
  		return false
  	}
  	blocked, ok := s.idleBlockedSessions(ctx)
  	if !ok || blocked[sess.ID] {
  		return false
  	}
  	return s.reopenable(ctx, sess)
  }
  ```

- [ ] **Step 5: Call it from `Invoke`**

  In `internal/agenthub/service.go`, in the deferred func of `Invoke` (the block that ends with the
  `OnTurnFinished` call, around line 2060), add after that `if s.OnTurnFinished != nil { … }` block and before the
  closing `}()`:

  ```go
  		// The turn is over and its result recorded: a folder granted during it can
  		// now reach the session's own shell, if the process can be swapped safely.
  		s.reopenAfterGrant(context.WithoutCancel(ctx), in.SessionID)
  ```

  (`defer s.kickPeers(...)` earlier in the same block runs after this line, so a waiting peer message is
  delivered to the reopened process.)

- [ ] **Step 6: Run**

  Run with `-run 'TestAMarked|TestAnUnmarked|TestTheReopenGuards|TestAGrantMarks|TestReleaseIdle|TestAReleased'`.
  Expected: PASS. Run once more with `-race`. If `TestAMarkedSessionIsReleasedAtTheEndOfItsTurn` shows the process is
  still there, the deferred call ran before the status returned to `idle`: check the order in the block and place the
  call after `release()` and after the status write.

- [ ] **Step 7: Mutations**

  Run each; expect the named test to FAIL; restore:
  1. Delete `|| s.bgwork.running(sess.ID)`'s check in `releasableNow`: `TestTheReopenGuards/a_vendor_task_is_running` and
     `.../an_agent-started_turn_is_running`.
  2. Delete the child loop: `.../a_child_is_running`.
  3. Delete the `s.reopenable` line: `.../the_vendor_cannot_load_the_session_again`.
  4. Delete the `s.reopenAfterGrant(...)` line in `Invoke`: `TestAMarkedSessionIsReleasedAtTheEndOfItsTurn`.

- [ ] **Step 8: Commit**

  ```bash
  git add internal/agenthub/reopen_grant.go internal/agenthub/reopen_grant_test.go internal/agenthub/service.go internal/agenthub/export_test.go
  git commit -m "feat(agenthub): reopen a session's process at the end of the turn that gained a folder"
  ```

---

### Task 6: Say what a granted folder is in the Environment view, and wire the folder lookup

**Files:**
- Modify: `internal/agenthub/environment.go`
- Create: `internal/app/agentfolders.go`, `internal/app/agentfolders_test.go`
- Modify: `main.go`

- [ ] **Step 1: Wording**

  In `internal/agenthub/environment.go` (the loop over `snap.Spec.ExtraReadWritePaths`) change
  `Detail: "Granted when this session was created."` to `Detail: "Granted to this session."`. Run
  `grep -rn 'Granted when this session was created' internal frontend/src docs` and update any test or doc that
  names the old sentence.

- [ ] **Step 2: Write the failing adapter test**

  Create `internal/app/agentfolders_test.go`:

  ```go
  package app

  import (
  	"testing"

  	"devkit/internal/agenthub"
  )

  func TestAgentWorkspaceFoldersNamesTheWorkspaceAndItsServices(t *testing.T) {
  	name, folders, err := AgentWorkspaceFolders(briefFixture())("sec")
  	if err != nil {
  		t.Fatal(err)
  	}
  	if name != "fsap-security" || len(folders) != 3 {
  		t.Fatalf("name = %q, folders = %v", name, folders)
  	}
  	if folders[1] != (agenthub.WorkspaceFolder{Name: "fsap-authentication-service", Path: "/w/auth"}) {
  		t.Fatalf("folders[1] = %+v", folders[1])
  	}
  }

  func TestAgentWorkspaceFoldersForAnUnknownWorkspaceIsEmpty(t *testing.T) {
  	name, folders, err := AgentWorkspaceFolders(briefFixture())("nope")
  	if err != nil || name != "" || len(folders) != 0 {
  		t.Fatalf("got %q, %v, %v", name, folders, err)
  	}
  }
  ```

- [ ] **Step 3: Run to verify it fails**

  `go test -p 1 -count=1 ./internal/app -run 'TestAgentWorkspaceFolders'` (with the memory cap prefix). Expected:
  `undefined: AgentWorkspaceFolders`.

- [ ] **Step 4: Implement**

  Create `internal/app/agentfolders.go`:

  ```go
  package app

  import "devkit/internal/agenthub"

  // AgentWorkspaceFolders answers Agent Hub's question "what is this workspace called
  // and which folders does it hold" for a request for write access
  // (agenthub.Service.WorkspaceFolders). An unknown workspace is an empty answer, not
  // an error: the request is then refused for naming a folder that is not there.
  func AgentWorkspaceFolders(ws workspaceLister) func(workspaceID string) (string, []agenthub.WorkspaceFolder, error) {
  	return func(workspaceID string) (string, []agenthub.WorkspaceFolder, error) {
  		all, err := ws.ListWorkspaces()
  		if err != nil {
  			return "", nil, err
  		}
  		for _, w := range all {
  			if w.ID != workspaceID {
  				continue
  			}
  			services, err := ws.ListServices(w.ID)
  			if err != nil {
  				return "", nil, err
  			}
  			folders := make([]agenthub.WorkspaceFolder, 0, len(services))
  			for _, svc := range services {
  				folders = append(folders, agenthub.WorkspaceFolder{Name: svc.Name, Path: svc.Path})
  			}
  			return w.Name, folders, nil
  		}
  		return "", nil, nil
  	}
  }
  ```

- [ ] **Step 5: Wire it in `main.go`**

  In the `if container.Workspace != nil {` block (around line 686) add after `agentSvc.MatchingWorkspaces = …`:

  ```go
  		agentSvc.WorkspaceFolders = app.AgentWorkspaceFolders(container.Workspace)
  ```

- [ ] **Step 6: Run and commit**

  Run `-run 'TestAgentWorkspaceFolders'` and `go build ./...` is not allowed to be skipped for `main`: create the
  temporary embed directory first (`mkdir -p frontend/dist && touch frontend/dist/.keep`, never commit it), run
  `go build -o /dev/null .`, then remove it. Expected: PASS and a clean build.

  ```bash
  git add internal/agenthub/environment.go internal/app/agentfolders.go internal/app/agentfolders_test.go main.go
  git commit -m "feat(app): give Agent Hub the folders of a workspace for write requests"
  ```

---

### Task 7: The tool, its capability, its codes, and the hint that names it

**Files:**
- Modify: `internal/mcp/tools_workspace.go`, `internal/mcp/server.go`, `internal/mcp/tools_git.go`, `internal/mcp/git_repo.go`,
  `internal/bridge/errors.go`, `internal/app/container.go`, `main.go`
- Create: `internal/mcp/tools_workspace_write_test.go`

- [ ] **Step 1: Write the failing tool tests**

  Create `internal/mcp/tools_workspace_write_test.go`:

  ```go
  package mcp

  import (
  	"context"
  	"errors"
  	"testing"

  	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

  	"devkit/internal/agenthub"
  	"devkit/internal/apperrors"
  	"devkit/internal/harness"
  	"devkit/internal/identity"
  	"devkit/internal/interaction"
  )

  type fakeWorkspaceWrite struct {
  	plan      agenthub.WriteGrantPlan
  	prepErr   error
  	commitErr error
  	prepared  int
  	committed int
  }

  func (f *fakeWorkspaceWrite) PrepareWorkspaceWrite(_ context.Context, folder string) (agenthub.WriteGrantPlan, error) {
  	f.prepared++
  	f.plan.Folder = folder
  	return f.plan, f.prepErr
  }

  func (f *fakeWorkspaceWrite) CommitWorkspaceWrite(_ context.Context, plan agenthub.WriteGrantPlan) (agenthub.WriteGrantResult, error) {
  	f.committed++
  	return agenthub.WriteGrantResult{Folder: plan.Folder, Granted: true,
  		Effective: agenthub.WriteGrantEffect{Git: agenthub.EffectNow, NewSessions: agenthub.EffectNow, ShellAndFiles: agenthub.EffectAfterThisTurn}}, f.commitErr
  }

  const grantSID = "11111111-2222-3333-4444-555555555555"

  // hubAnswering is a hub whose agent confirmation answers with answer (or err).
  func hubAnswering(answer string, err error, asked *int) *interaction.Hub {
  	h := interaction.NewHub(interaction.NoopPrompter{})
  	h.ConfirmAgent = func(context.Context, interaction.ConfirmRequest) (string, error) {
  		*asked++
  		return answer, err
  	}
  	return h
  }

  func grantServer(t *testing.T, caller *fakeCaller, w WorkspaceWriteAPI, hub *interaction.Hub) *Server {
  	t.Helper()
  	d := agentHubTestDeps(caller, &fakeAgentHub{})
  	d.WorkspaceWrite = w
  	d.Hub = hub
  	return NewServer(d)
  }

  func callGrant(srv *Server, folder string) (any, error) {
  	ctx := identity.WithCaller(context.Background(), identity.Agent(grantSID))
  	_, v, err := srv.toolWorkspaceRequestWrite(ctx, &sdk.CallToolRequest{}, workspaceRequestWriteInput{Folder: folder})
  	return v, err
  }

  func errCode(err error) string {
  	var app *apperrors.AppError
  	if errors.As(err, &app) {
  		return app.Code
  	}
  	return ""
  }

  func TestRequestWriteAsksThenStores(t *testing.T) {
  	asked, caller := 0, &fakeCaller{}
  	w := &fakeWorkspaceWrite{plan: agenthub.WriteGrantPlan{SessionID: grantSID, Path: "/w/audit", WorkspaceName: "fsap-event"}}
  	v, err := callGrant(grantServer(t, caller, w, hubAnswering("allow", nil, &asked)), "fsap-audit-service")
  	if err != nil {
  		t.Fatal(err)
  	}
  	got, ok := unwrap(v).(agenthub.WriteGrantResult)
  	if !ok || !got.Granted || got.Effective.ShellAndFiles != agenthub.EffectAfterThisTurn {
  		t.Fatalf("result = %#v", v)
  	}
  	if asked != 1 || w.committed != 1 {
  		t.Fatalf("asked %d times, committed %d times; want one each", asked, w.committed)
  	}
  	if caller.last.subject != "builtin.workspace" || caller.last.capability != "ws:grant" || caller.last.scope != "workspace:requestWrite" {
  		t.Fatalf("Call = %+v", caller.last)
  	}
  }

  func TestRequestWriteOfAWritableFolderAsksNobody(t *testing.T) {
  	asked := 0
  	w := &fakeWorkspaceWrite{plan: agenthub.WriteGrantPlan{SessionID: grantSID, AlreadyGranted: true}}
  	if _, err := callGrant(grantServer(t, &fakeCaller{}, w, hubAnswering("allow", nil, &asked)), "x"); err != nil {
  		t.Fatal(err)
  	}
  	if asked != 0 {
  		t.Fatal("the person was asked about a folder the session can already write")
  	}
  }

  func TestRequestWriteRefusals(t *testing.T) {
  	cases := []struct {
  		name      string
  		w         *fakeWorkspaceWrite
  		hub       func(*int) *interaction.Hub
  		wantCode  string
  		wantAsked int
  		wantStore int
  	}{
  		{"a refused plan", &fakeWorkspaceWrite{prepErr: &agenthub.GrantRefusedError{Reason: "no"}}, func(a *int) *interaction.Hub { return hubAnswering("allow", nil, a) }, "workspace.grantRefused", 0, 0},
  		{"the person declines", &fakeWorkspaceWrite{}, func(a *int) *interaction.Hub { return hubAnswering("deny", nil, a) }, "workspace.grantDeclined", 1, 0},
  		{"nobody can answer", &fakeWorkspaceWrite{}, func(*int) *interaction.Hub { return &interaction.Hub{Headless: true} }, "workspace.grantUnavailable", 0, 0},
  		{"a refusal at commit", &fakeWorkspaceWrite{commitErr: &agenthub.GrantRefusedError{Reason: "moved"}}, func(a *int) *interaction.Hub { return hubAnswering("allow", nil, a) }, "workspace.grantRefused", 1, 1},
  	}
  	for _, c := range cases {
  		t.Run(c.name, func(t *testing.T) {
  			asked := 0
  			_, err := callGrant(grantServer(t, &fakeCaller{}, c.w, c.hub(&asked)), "fsap-audit-service")
  			if got := errCode(err); got != c.wantCode {
  				t.Fatalf("code = %q (%v), want %q", got, err, c.wantCode)
  			}
  			if asked != c.wantAsked || c.w.committed != c.wantStore {
  				t.Fatalf("asked %d, committed %d; want %d and %d", asked, c.w.committed, c.wantAsked, c.wantStore)
  			}
  		})
  	}
  }

  // allow_always must never short-circuit this question.
  func TestRequestWriteIsNeverRemembered(t *testing.T) {
  	asked := 0
  	w := &fakeWorkspaceWrite{}
  	srv := grantServer(t, &fakeCaller{}, w, hubAnswering("allow_always", nil, &asked))
  	for range 2 {
  		if _, err := callGrant(srv, "fsap-audit-service"); err != nil {
  			t.Fatal(err)
  		}
  	}
  	if asked != 2 {
  		t.Fatalf("asked %d times for two requests", asked)
  	}
  }

  func TestRequestWriteWithoutTheServiceSaysSo(t *testing.T) {
  	d := agentHubTestDeps(&fakeCaller{}, &fakeAgentHub{})
  	if _, err := callGrant(NewServer(d), "x"); errCode(err) != "internal_error" {
  		t.Fatalf("err = %v", err)
  	}
  }

  // The tool shares the `workspace` harness group with workspace.list: a session that
  // turned the group off cannot call it (serving.go answers harness.groupOff).
  func TestRequestWriteIsInTheWorkspaceHarnessGroup(t *testing.T) {
  	if harness.Group("workspace.requestWrite") != harness.Group("workspace.list") {
  		t.Fatal("workspace.requestWrite left the workspace group")
  	}
  }
  ```

- [ ] **Step 2: Run to verify they fail**

  `go test -p 1 -count=1 ./internal/mcp -run 'TestRequestWrite'` (memory cap prefix). Expected: build failure
  (`WorkspaceWriteAPI`, `Deps.WorkspaceWrite`, `toolWorkspaceRequestWrite` undefined).

- [ ] **Step 3: Implement the tool**

  In `internal/mcp/tools_workspace.go` add imports `"errors"`, `"fmt"`, `"devkit/internal/agenthub"`,
  `"devkit/internal/interaction"` and, below `WorkspaceAPI`:

  ```go
  // WorkspaceWriteAPI is the owner of folder grants, Agent Hub.
  type WorkspaceWriteAPI interface {
  	PrepareWorkspaceWrite(ctx context.Context, folder string) (agenthub.WriteGrantPlan, error)
  	CommitWorkspaceWrite(ctx context.Context, plan agenthub.WriteGrantPlan) (agenthub.WriteGrantResult, error)
  }

  // Public codes of workspace.requestWrite. Each is published in bridge keptCodes.
  const (
  	codeGrantRefused     = "workspace.grantRefused"
  	codeGrantDeclined    = "workspace.grantDeclined"
  	codeGrantUnavailable = "workspace.grantUnavailable"
  )

  type workspaceRequestWriteInput struct {
  	Folder string `json:"folder" jsonschema:"the folder's name, exactly as workspace.list shows it"`
  }
  ```

  In `registerWorkspace`, after the `workspace.list` registration:

  ```go
  	sdk.AddTool(ms, s.gateOwnedTool(&sdk.Tool{
  		Name: "workspace.requestWrite",
  		Description: "Ask the person for write access to one folder of your workspace that workspace.list shows with granted:false. " +
  			"Pass the folder's name. The person is asked every time. Once approved, git and DevKit tools can use the folder and " +
  			"sub-sessions you create afterwards inherit it; your own shell and file tools get it when DevKit reopens this session " +
  			"at the end of the turn (the answer says when). Only a top-level session can ask: a sub-session asks its parent.",
  		Annotations: &sdk.ToolAnnotations{DestructiveHint: boolPtr(false)},
  	}), s.toolWorkspaceRequestWrite)
  ```

  If a `boolPtr` helper does not exist in the package, drop the `Annotations` line (the tool is not read-only by
  default).

  Append:

  ```go
  func (s *Server) toolWorkspaceRequestWrite(ctx context.Context, _ *sdk.CallToolRequest, in workspaceRequestWriteInput) (*sdk.CallToolResult, any, error) {
  	if s.deps.WorkspaceWrite == nil {
  		return nil, nil, apperrors.New("internal_error", "Folder access requests are not available.", true)
  	}
  	v, err := s.deps.Caller.Call(ctx, "builtin.workspace", "ws:grant", "workspace:requestWrite",
  		meta("request_workspace_write", nil), // the folder is the agent's string: not recorded
  		func(callCtx context.Context) (any, error) {
  			plan, err := s.deps.WorkspaceWrite.PrepareWorkspaceWrite(callCtx, in.Folder)
  			if err != nil {
  				return nil, mapGrantError(err)
  			}
  			if !plan.AlreadyGranted {
  				if err := s.grantConfirm(callCtx, plan); err != nil {
  					return nil, mapGrantError(err)
  				}
  			}
  			res, err := s.deps.WorkspaceWrite.CommitWorkspaceWrite(callCtx, plan)
  			if err != nil {
  				return nil, mapGrantError(err)
  			}
  			return res, nil
  		})
  	if err != nil {
  		return nil, nil, err
  	}
  	return nil, structured(v), nil
  }

  // grantConfirm asks every time: no allow-always (EnvBlocks), the same rule as peerConfirm.
  func (s *Server) grantConfirm(ctx context.Context, plan agenthub.WriteGrantPlan) error {
  	return effectiveHub(s.deps.Hub).Confirm(ctx, interaction.ConfirmRequest{
  		Tool: "workspace.requestWrite", Kind: interaction.KindAgent, EnvBlocks: true,
  		Summary: fmt.Sprintf("Let this session write in %q (workspace %q)? Path: %s. This lasts for as long as the session does.",
  			plan.Folder, plan.WorkspaceName, plan.Path),
  	})
  }

  func mapGrantError(err error) error {
  	var refused *agenthub.GrantRefusedError
  	switch {
  	case errors.As(err, &refused):
  		return apperrors.New(codeGrantRefused, refused.Reason, false)
  	case errors.Is(err, interaction.ErrConfirmationDenied):
  		return apperrors.New(codeGrantDeclined, "The person declined to give this session write access to that folder.", false)
  	case errors.Is(err, interaction.ErrConfirmationRequired):
  		return apperrors.New(codeGrantUnavailable, "No one can approve this right now: there is no window to ask in.", false)
  	default:
  		return mapInteractErr(err)
  	}
  }
  ```

  In `internal/mcp/server.go`, in `Deps` next to `Workspace` (line 226) add:

  ```go
  	// WorkspaceWrite answers workspace.requestWrite. Optional nil: the tool then
  	// says folder access requests are not available.
  	WorkspaceWrite WorkspaceWriteAPI
  ```

- [ ] **Step 4: Run the tool tests**

  `-run 'TestRequestWrite'`. Expected: PASS. If an existing test asserts that every `gateOwnedTool` handler is
  reached only after a `Confirm`, run `go test -p 1 -count=1 ./internal/mcp` now and read any failure: the
  already-writable path skips the question on purpose (nothing is granted), so adjust that assertion for this tool
  only and say so in the commit message.

- [ ] **Step 5: Publish the codes**

  In `internal/bridge/errors.go`, in `keptCodes` after `"agenthub.resumeRefused": true,` add:

  ```go
  	// internal/mcp/tools_workspace.go workspace.requestWrite: fixed sentences, or the
  	// refusal reason Agent Hub wrote (agenthub.GrantRefusedError), which names no path
  	// and no folder the caller did not name itself.
  	"workspace.grantRefused":     true,
  	"workspace.grantDeclined":    true,
  	"workspace.grantUnavailable": true,
  ```

- [ ] **Step 6: Grant the capability and say the tool's name in the git hint**

  In `internal/app/container.go`, after the `{SubjectID: "builtin.workspace", Capability: "ws:read", Scope: "service:*"}` line:

  ```go
  		// workspace.requestWrite: the service refuses every caller but an agent
  		// session, and the person confirms each request.
  		{SubjectID: "builtin.workspace", Capability: "ws:grant", Scope: "workspace:requestWrite"},
  ```

  In `internal/mcp/tools_git.go` (line 157) replace `Ask the person to add it to the session, or work in "+scope.cwd+"."` with
  `Ask for it with workspace.requestWrite (a sub-session asks its parent), or work in "+scope.cwd+"."` and make the
  same replacement in `internal/mcp/git_repo.go` (line 241, where the message continues on the next line; keep the
  `scope.cwd` part as it is). Run `grep -rn 'Ask the person to add it' internal docs` and fix any remaining copy.

- [ ] **Step 7: Wire the service in `main.go`**

  Above the `mcp.Deps{` literal that holds `AgentHub: container.AgentHub,` (around line 1150) add:

  ```go
  	// workspace.requestWrite: nil stays nil (an interface holding a nil pointer would not).
  	var workspaceWrite mcp.WorkspaceWriteAPI
  	if container.AgentHub != nil {
  		workspaceWrite = container.AgentHub
  	}
  ```

  and in the literal, next to `Workspace: container.Workspace,`:

  ```go
  		WorkspaceWrite:  workspaceWrite,
  ```

  Confirm `container.AgentHub` is the same `*agenthub.Service` as `agentSvc` (`grep -n 'container.AgentHub =' main.go`).
  If it is not, use `agentSvc` here so the hook set in Task 6 is on the service that answers.

- [ ] **Step 8: Run the wider suites**

  With the memory cap, one package at a time: `./internal/mcp`, `./internal/bridge`, `./internal/app`,
  `./internal/agenthub`, `./internal/arch`, `./internal/harness`. Then `go vet -tags server ./internal/mcp ./internal/app ./internal/agenthub`
  and, with the temporary `frontend/dist/.keep`, `go build -o /dev/null .`. Expected: all pass. The known
  machine-only failure `internal/git` `TestResolveSideTakesTheStageExactly` is out of scope.

- [ ] **Step 9: Mutations**

  Expect the named test to FAIL, then restore:
  1. Delete the `s.grantConfirm(...)` call: `TestRequestWriteRefusals/the_person_declines`.
  2. Set `EnvBlocks: false` in `grantConfirm`: `TestRequestWriteIsNeverRemembered`.
  3. Remove one entry from `keptCodes`: the bridge test that checks published codes (run `./internal/bridge`); if none
     fails, add the three codes to that test's expected list in this task.

- [ ] **Step 10: Commit**

  ```bash
  git add internal/mcp internal/bridge/errors.go internal/app/container.go main.go
  git commit -m "feat(mcp): workspace.requestWrite lets an agent ask for a folder of its workspace"
  ```

---

### Task 8: Docs and the pull request

**Files:**
- Modify: `docs/agent-reference/agent-hub-vendors-sessions.md`, `docs/agent-reference/mcp-tools.md`, `docs/agent-reference/agent-hub-coordination.md`

- [ ] **Step 1: `agent-hub-vendors-sessions.md`**

  After the bullet "A child session may write what its parent may write, never more" (line 60) add a bullet:

  ```markdown
  - **An agent can ask for more folders of its own workspace** (`workspace.requestWrite`, `agenthub.Service.PrepareWorkspaceWrite`
    / `CommitWorkspaceWrite`, `ExtraPathStore.AddExtraReadWritePath`; spec
    `docs/superpowers/specs/2026-10-29-agent-write-grant-design.md`). One folder, by the name `workspace.list` shows, of the
    session's own workspace; only a top-level session (a child is refused: it already holds what its parent held); the
    person confirms every request (`EnvBlocks`, never remembered) and every check runs again after the answer. The folder
    is appended to the session's stored `extra_rw_paths` with a compare-and-set, so `workspace.list`, `git.*` and children
    created later see it at once and existing children never change. The session's own shell and file tools are bound to
    the jail built when the process started, so the session is marked and `reopenAfterGrant` releases its process at the
    end of the turn (the half of `CloseSession` that tears it down; the next prompt reopens it from the stored row) when
    `releasableNow` allows: idle or error, no running child, no waiting question or permission or queued peer message, the
    vendor can reload it, and **no vendor task running** (`bgwork.go`). A session that fails a guard keeps its process,
    the mark is spent and the folder applies at the next reopen. Idle release uses the same background-work guard: a
    session whose vendor still runs a task (a build, an agent-started turn) is not released.
  ```

- [ ] **Step 2: `mcp-tools.md`**

  After the `workspace.list` bullet (line 22 and its continuation) add a bullet for `workspace.requestWrite` with: the
  input, the three codes (`workspace.grantRefused`, `workspace.grantDeclined`, `workspace.grantUnavailable`, all in
  `bridge.keptCodes`), the capability `ws:grant`, scope `workspace:requestWrite`, that it is gate-owned and never
  remembered, that it belongs to the `workspace` harness group, and that the `git.repoNotGranted` hint now names it.

- [ ] **Step 3: `agent-hub-coordination.md`**

  In the parent/child control section add one sentence: "A sub-session cannot request more folders
  (`workspace.requestWrite` answers `workspace.grantRefused`); its parent asks, and children created afterwards inherit."

- [ ] **Step 4: Final checks**

  Run: `wc -c AGENTS.md` (must stay at or below 10240; it is not edited) and
  `git diff --stat origin/main...HEAD` (expect only the files in the file map plus the spec and this plan).
  Then run the whole verification of Task 7 Step 8 once more from a clean state.

- [ ] **Step 5: Independent review**

  The coordinator (not the implementing agent) starts one read-only Sonnet review agent with the spec, this plan and
  `git diff origin/main...HEAD`, asking it to look for: a path that stores a grant without the confirmation; a child
  that can obtain a folder its parent lacks; a release that can kill a running vendor task; `keptCodes` gaps; and
  documents that still say a grant is fixed at creation. Fix findings in new commits; do not squash.

- [ ] **Step 6: Commit and open the pull request**

  ```bash
  git add docs/agent-reference
  git commit -m "docs(agenthub): describe the folder-grant request and the background-work guard"
  git push -u origin HEAD
  gh pr create --title "feat(agenthub): an agent asks for write access to a workspace folder; idle release spares running tasks" --body-file <(cat <<'EOF'
  (Fill from the spec's summary: what changed, the two behaviour changes, the not-done list, the verification run, the Task 0 findings.)
  EOF
  )
  ```

  The pull request body ends with `🤖 Generated with [Claude Code](https://claude.com/claude-code)`. The owner merges.

---

## Self-review against the spec

| Spec section | Task |
| --- | --- |
| 2 D1 (one folder, by name) | 4, 7 |
| 2 D2 (confirm every time) | 7 (`grantConfirm`, `TestRequestWriteIsNeverRemembered`) |
| 2 D3 (stored on the session) | 3, 4 |
| 2 D4 (readers, reopen at turn end) | 4, 5 |
| 2 D5 (child refused) | 4 (`a sub-session`), 8 (docs) |
| 2 D6 (out of scope) | nothing built |
| 3 (tool contract, result shape) | 4 (`WriteGrantResult`), 7 |
| 4 steps 1 to 6 | 4 (`planWriteGrant`, `CommitWorkspaceWrite`), 7 (confirmation), 3 (compare-and-set) |
| 5 (who sees it) | Task 0 Step 1, 4 (`TestAGrantReachesOnlyChildrenCreatedAfterIt`) |
| 6 (reopen and the background-work guard, idle release) | 1, 2, 5 |
| 7 (codes, audit) | 7 (codes, `Caller.Call` metadata); audit is the gateway's |
| 8 (window, agent text) | 6 (Environment wording), 7 (git hint); notice left to the UI plan |
| 9 (tests) | each task's tests; mutation steps in 2, 4, 5, 7 |
| 10 (to confirm) | Task 0 |
| 11 (docs) | 8 |

Known differences from the spec, already written into it: the confirmation sits in the MCP layer between
`PrepareWorkspaceWrite` and `CommitWorkspaceWrite`; the store retries the compare-and-set up to ten times; the audit
record is the gateway's.

## Changes after the independent review

The code in this plan was corrected in follow-up commits; where the text above and the code differ, the code wins.
In short: `releaseRuntime` refuses a session whose turn claim is held and takes a `since` floor
(`releaseRuntimeSince`) that the end-of-turn reopen sets to the instant its turn ended; `CommitWorkspaceWrite` answers
`shellAndFiles: now` only for a folder under the session's working directory, refuses a plan that claims
`alreadyGranted` when the folder is not, and reports a lost compare-and-set race as `alreadyGranted`; the folder name
is clipped to 80 runes where a refusal repeats it and is not put in audit metadata; background-work records expire
12 hours after their spawn, `releaseRuntime` forgets them, and closing a session drops its reopen mark.
