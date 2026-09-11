# CodeGraph Auto Re-index (fsnotify) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Finish the ["Incremental re-index (add-on v1)"](base-doc `content/projects/dev-kit/codegraph.mdx:507-603`) design — the piece `2026-08-19-codegraph-blast-radius.md` deliberately deferred: watch the active project with `fsnotify`, debounce file-change events, automatically re-run the analyzer without a manual button press, and give callers `WaitForIndexReady` as a sync point so they never read a stale index mid-debounce.

**Scope decision, confirmed against the design doc:** "incremental" here means the **trigger** is automatic — re-analysis itself stays a full `Analyzer.Analyze(ctx, projectPath)` call, same as today's manual `OpenProject`. True partial re-parse (only re-resolving the affected file set) is a Java-CLI-side optimization the design doc itself flags as inherently approximate (`codegraph.mdx:597-603`, "Rủi ro thật: sai affected set") and explicitly not worth building until a real project proves full re-analyze too slow. This plan does not touch the Java analyzer at all.

**Architecture:** One background goroutine per open project (`Service.watchLoop`), started/restarted by `OpenProject`, non-fatal if it fails to start (the project still opens and is queryable — auto-trigger is an enhancement, not a hard requirement, same "manual re-index stays a permanent fallback" stance the design doc takes). The goroutine reads `fsnotify` events, resets a debounce timer on each one, and on quiet-period expiry re-runs the analyzer with a cancellable context that a newer event immediately cancels (`cancel-on-newer-event`, per the design doc). `Service.Close()` stops the goroutine cleanly on app shutdown. `WaitForIndexReady` polls a mutex-guarded `reindexing` flag — deliberately a polling loop, not a `sync.Cond`, since `sync.Cond.Wait()` doesn't compose with `context.Context` cancellation without extra machinery, and the debounce timescale (hundreds of ms) makes a 50ms poll interval unnoticeable.

**Known limitation, accepted deliberately (not fixed here):** there is a microsecond-scale window where `WaitForIndexReady`, called immediately after an external file write, could observe `reindexing == false` before the OS has delivered the `fsnotify` event for that write — the poll would return "ready" fractionally before the debounced reindex it should have waited for even starts. This is the same class of staleness the design doc's own "Rủi ro thật" callout already accepts as a known ceiling of this feature (bounded by one debounce interval, self-correcting), not a new risk this plan introduces — closing it fully would require the *writer* (e.g. `CodeGraphWriteFile` from `2026-08-19-codegraph-file-editing.md`) to synchronously mark "pending" before this method's poll can ever run, which is out of scope for a plan that doesn't own `WriteFile`. Noted as a follow-up integration point in "What this plan does NOT do."

**Tech Stack:** `github.com/fsnotify/fsnotify` (new dependency — verified against its real API before writing this plan: `NewWatcher() (*Watcher, error)`, `Add(path string) error`, `Events chan Event`, `Errors chan error`, `Close() error`, `Event{Name string, Op Op}`, `event.Has(fsnotify.Write)`; watching a directory is **non-recursive** — subdirectories must be `Add`ed individually, including ones created after watching starts).

---

## File Structure

Modify:
- `go.mod`/`go.sum` — add `github.com/fsnotify/fsnotify`.
- `internal/codegraph/service.go` — `projectRoot` (if not already added by `2026-08-19-codegraph-file-editing.md`), `indexVersion`/`reindexing`/`debounceInterval`/watcher-lifecycle fields, `addRecursive`, `restartWatcher`, `watchLoop`, `runReindex`, `WaitForIndexReady`, `Close`.
- `internal/codegraph/service_test.go` — tests for all of the above.
- `main.go` — `defer container.CodeGraph.Close()`.
- `internal/bridge/codegraph.go` — `CodeGraphWaitForIndexReady`.
- `internal/bridge/codegraph_test.go` — test for it.
- `internal/mcp/server.go` — extend `CodeGraphAPI`.
- `internal/mcp/tools_codegraph.go` — register `codegraph.waitForIndexReady`.
- `internal/mcp/tools_codegraph_test.go` — extend `fakeCodeGraphAPI`, test for the new tool.

---

## Task 1: `Service.projectRoot` (skip if already present)

**Files:**
- Modify: `internal/codegraph/service.go`

- [ ] **Step 1: Check whether this already exists**

```bash
grep -n "projectRoot" internal/codegraph/service.go
```

If `2026-08-19-codegraph-file-editing.md` (Task 2) has already been implemented, this will show a `projectRoot string` field on `Service`, `s.projectRoot = projectPath` inside `OpenProject`, and an `activeProject() (Store, string, error)` method. **If so, skip this entire task** — proceed to Task 2. This plan's watcher needs `projectRoot` for the same reason that plan's file-editing methods do (knowing what directory to operate on), and both plans were written to add it identically to avoid divergence if implemented independently.

- [ ] **Step 2: If missing, add it**

In `internal/codegraph/service.go`, add a field to `Service`:

```go
type Service struct {
	mu          sync.RWMutex
	store       Store
	analyzer    Analyzer
	active      bool
	projectRoot string
}
```

Set it in `OpenProject`, alongside `s.active = true`:

```go
func (s *Service) OpenProject(ctx context.Context, projectPath string) error {
	result, err := s.analyzer.Analyze(ctx, projectPath)
	if err != nil {
		return fmt.Errorf("codegraph: analyze %q: %w", projectPath, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.store.ReplaceIndex(ctx, result); err != nil {
		s.active = false
		return fmt.Errorf("codegraph: replace index: %w", err)
	}
	s.active = true
	s.projectRoot = projectPath
	return nil
}
```

Replace `activeStore()` with a combined accessor, keeping `activeStore()` as a thin wrapper so existing callers don't change:

```go
func (s *Service) activeProject() (Store, string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.active {
		return nil, "", ErrNoActiveProject
	}
	return s.store, s.projectRoot, nil
}

func (s *Service) activeStore() (Store, error) {
	store, _, err := s.activeProject()
	return store, err
}
```

- [ ] **Step 3: Build and test**

```bash
go build ./...
go test ./internal/codegraph/... -race
```

- [ ] **Step 4: Commit (only if Step 2 was needed)**

```bash
git add internal/codegraph/service.go
git commit -m "feat(codegraph): Service tracks the active project's root path"
```

---

## Task 2: Add the `fsnotify` dependency and `Service`'s new state

**Files:**
- Modify: `go.mod`, `go.sum`
- Modify: `internal/codegraph/service.go`

- [ ] **Step 1: Add the dependency**

```bash
go get github.com/fsnotify/fsnotify
go build ./...
```

Expected: `go.mod`/`go.sum` gain the entry; build succeeds (nothing imports it yet).

- [ ] **Step 2: Extend `Service`**

In `internal/codegraph/service.go`:

```go
type Service struct {
	mu          sync.RWMutex
	store       Store
	analyzer    Analyzer
	active      bool
	projectRoot string

	indexVersion int
	reindexing   bool
	// debounceInterval is the quiet period after the last fsnotify event
	// before a re-index actually runs. 750ms in production; tests shorten it
	// (this is an unexported field set directly in-package, same pattern
	// internal/cloudconfig.RefreshOptions uses for its own test-shortenable
	// PollInterval, just without needing an exported options struct here
	// since Service already owns its own lifecycle).
	debounceInterval time.Duration

	watchCancel context.CancelFunc
	watchDone   chan struct{}
}

func NewService(store Store, analyzer Analyzer) *Service {
	return &Service{store: store, analyzer: analyzer, debounceInterval: 750 * time.Millisecond}
}
```

Add `"time"` to the imports (currently `"context"`, `"fmt"`, `"sync"`, plus whatever Task 1 added).

- [ ] **Step 3: Build**

```bash
go build ./...
go test ./internal/codegraph/... -race
```

Expected: succeeds — `NewService`'s signature is unchanged, every existing caller still compiles.

- [ ] **Step 4: Commit**

```bash
git add go.mod go.sum internal/codegraph/service.go
git commit -m "feat(codegraph): add fsnotify dependency and Service watcher state"
```

---

## Task 3: `addRecursive` — watch every subdirectory

**Files:**
- Modify: `internal/codegraph/service.go`
- Modify: `internal/codegraph/service_test.go`

`fsnotify.Watcher.Add` is non-recursive (verified against the real API) — a directory's own files are watched, but subdirectories are not. Must walk the tree once and `Add` each one.

- [ ] **Step 1: Write the failing test**

Add to `internal/codegraph/service_test.go`:

```go
func TestAddRecursiveWatchesNestedDirs(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "src", "main", "java"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}

	w, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	defer w.Close()

	if err := addRecursive(w, dir); err != nil {
		t.Fatalf("addRecursive: %v", err)
	}

	watched := w.WatchList()
	has := func(p string) bool {
		for _, w := range watched {
			if w == p {
				return true
			}
		}
		return false
	}
	if !has(dir) {
		t.Fatalf("expected root %q watched, got %v", dir, watched)
	}
	if !has(filepath.Join(dir, "src", "main", "java")) {
		t.Fatalf("expected nested dir watched, got %v", watched)
	}
	if has(filepath.Join(dir, ".git")) {
		t.Fatalf(".git should not be watched, got %v", watched)
	}
}
```

Add `"os"` and `"github.com/fsnotify/fsnotify"` to `internal/codegraph/service_test.go`'s imports.

- [ ] **Step 2: Run to confirm failure**

```bash
go test ./internal/codegraph/... -run TestAddRecursiveWatchesNestedDirs -v
```

Expected: FAIL — compile error, `addRecursive` doesn't exist. (`Watcher.WatchList()` is a real method on `*fsnotify.Watcher` — if the installed version's API differs, `go doc github.com/fsnotify/fsnotify.Watcher` will show the actual method set; adjust the assertion to whatever it exposes rather than guessing further.)

- [ ] **Step 3: Implement**

In `internal/codegraph/service.go`, add:

```go
// addRecursive watches root and every subdirectory under it (fsnotify.Add
// is non-recursive — each directory must be added individually), skipping
// .git the same way Service.ListFiles does.
func addRecursive(w *fsnotify.Watcher, root string) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		if d.Name() == ".git" {
			return filepath.SkipDir
		}
		return w.Add(path)
	})
}
```

Add `"io/fs"` and `"github.com/fsnotify/fsnotify"` to `internal/codegraph/service.go`'s imports.

- [ ] **Step 4: Run the test**

```bash
go test ./internal/codegraph/... -run TestAddRecursiveWatchesNestedDirs -v
```

Expected: passes.

- [ ] **Step 5: Commit**

```bash
git add internal/codegraph/service.go internal/codegraph/service_test.go
git commit -m "feat(codegraph): addRecursive watches every subdirectory under a project root"
```

---

## Task 4: The watch loop — debounce, cancel-on-newer-event, background re-index

**Files:**
- Modify: `internal/codegraph/service.go`

This is the core of the feature. No dedicated unit test in this task — `watchLoop`'s behavior is only meaningfully observable end-to-end (Task 7 covers that with real files and real `fsnotify` events); this task is implementation, wired in immediately by Task 5's `OpenProject` change so Task 7 can exercise the whole path at once.

- [ ] **Step 1: Implement `restartWatcher`**

```go
// restartWatcher stops any watcher from a previously opened project (waiting
// for its goroutine to fully exit, so its *fsnotify.Watcher is closed before
// a new one opens — avoids two watchers racing on the same underlying OS
// resources across a rapid re-open) and starts a fresh one for root.
func (s *Service) restartWatcher(root string) error {
	s.mu.Lock()
	oldCancel := s.watchCancel
	oldDone := s.watchDone
	s.mu.Unlock()
	if oldCancel != nil {
		oldCancel()
		<-oldDone
	}

	w, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("codegraph: create watcher: %w", err)
	}
	if err := addRecursive(w, root); err != nil {
		w.Close()
		return fmt.Errorf("codegraph: watch %q: %w", root, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	s.mu.Lock()
	s.watchCancel = cancel
	s.watchDone = done
	s.mu.Unlock()

	go s.watchLoop(ctx, w, done)
	return nil
}
```

- [ ] **Step 2: Implement `watchLoop`**

```go
// watchLoop owns w for its entire lifetime (closes it on exit) and debounces
// fsnotify events into background re-index runs. A newer event cancels a
// still-running re-index rather than letting it overwrite the index with
// stale results once a fresher analyze has already started.
func (s *Service) watchLoop(ctx context.Context, w *fsnotify.Watcher, done chan struct{}) {
	defer close(done)
	defer w.Close()

	var timer *time.Timer
	var timerC <-chan time.Time
	var reindexCancel context.CancelFunc

	resetDebounce := func() {
		s.mu.RLock()
		interval := s.debounceInterval
		s.mu.RUnlock()
		if timer == nil {
			timer = time.NewTimer(interval)
		} else {
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(interval)
		}
		timerC = timer.C
	}

	for {
		select {
		case <-ctx.Done():
			if timer != nil {
				timer.Stop()
			}
			if reindexCancel != nil {
				reindexCancel()
			}
			return

		case event, ok := <-w.Events:
			if !ok {
				return
			}
			if event.Has(fsnotify.Create) {
				if info, statErr := os.Stat(event.Name); statErr == nil && info.IsDir() && filepath.Base(event.Name) != ".git" {
					_ = w.Add(event.Name) // best-effort: a new package/dir created after watching started
				}
			}
			resetDebounce()

		case <-timerC:
			timerC = nil
			if reindexCancel != nil {
				reindexCancel() // a slower-than-debounce previous run is still in flight — cancel it, its result would already be stale
			}
			reindexCtx, cancel := context.WithCancel(ctx)
			reindexCancel = cancel
			go s.runReindex(reindexCtx)

		case _, ok := <-w.Errors:
			if !ok {
				return
			}
			// best-effort: a single watcher error (e.g. a transient OS-level
			// read error) doesn't stop watching — the loop keeps running.
		}
	}
}
```

- [ ] **Step 3: Implement `runReindex`**

```go
// runReindex is the background counterpart to OpenProject's analyze+replace
// — deliberately more lenient on failure. OpenProject (an explicit, foreground
// user action) marks the project inactive on any error, per its own existing
// contract. A background trigger must not do that: a transient failure (e.g.
// catching a file mid-save with temporarily invalid syntax, or ctx cancelled
// by a newer event) should silently leave the last known-good index in
// place, not blow away an already-working project out from under the user.
// The next debounced trigger (or a manual re-index) gets another chance.
func (s *Service) runReindex(ctx context.Context) {
	s.mu.Lock()
	s.reindexing = true
	root := s.projectRoot
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.reindexing = false
		s.mu.Unlock()
	}()

	result, err := s.analyzer.Analyze(ctx, root)
	if err != nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.store.ReplaceIndex(ctx, result); err != nil {
		return
	}
	s.indexVersion++
}
```

- [ ] **Step 4: Build**

```bash
go build ./...
```

Expected: succeeds. Nothing calls `restartWatcher`/`runReindex` yet (Task 5 wires `restartWatcher` into `OpenProject`) — this is expected to compile as dead code for one task, same as every other plan in this sequence that separates "implement" from "wire in" across tasks.

- [ ] **Step 5: Commit**

```bash
git add internal/codegraph/service.go
git commit -m "feat(codegraph): watch loop — debounced, cancellable background re-index"
```

---

## Task 5: Wire the watcher into `OpenProject`

**Files:**
- Modify: `internal/codegraph/service.go`
- Modify: `internal/codegraph/service_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestServiceOpenProjectStartsWatcherNonFatally(t *testing.T) {
	// A project path that doesn't exist on disk can't be watched (fsnotify.Add
	// fails), but OpenProject must still succeed — auto-trigger degrades
	// gracefully, it never blocks the project from opening.
	svc := newTestService(t, fakeAnalyzer{result: sampleResult()})
	if err := svc.OpenProject(context.Background(), "/fake/project"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	t.Cleanup(func() { svc.Close() })

	endpoints, err := svc.ListEndpoints(context.Background())
	if err != nil {
		t.Fatalf("ListEndpoints: %v", err)
	}
	if len(endpoints) == 0 {
		t.Fatal("expected the project to still be usable despite the watcher failing to start")
	}
}
```

- [ ] **Step 2: Run to confirm failure**

```bash
go test ./internal/codegraph/... -run TestServiceOpenProjectStartsWatcherNonFatally -v
```

Expected: PASSES already (nothing changed `OpenProject` yet, and it never touched watching before) — this is fine, it's here to lock in the "must still work" contract before the next step adds the watcher call that could regress it if done wrong. Proceed to Step 3 regardless.

- [ ] **Step 3: Implement**

In `internal/codegraph/service.go`, change `OpenProject` (from Task 1) to also start the watcher, ignoring a watcher-start failure:

```go
func (s *Service) OpenProject(ctx context.Context, projectPath string) error {
	result, err := s.analyzer.Analyze(ctx, projectPath)
	if err != nil {
		return fmt.Errorf("codegraph: analyze %q: %w", projectPath, err)
	}
	s.mu.Lock()
	if err := s.store.ReplaceIndex(ctx, result); err != nil {
		s.active = false
		s.mu.Unlock()
		return fmt.Errorf("codegraph: replace index: %w", err)
	}
	s.active = true
	s.projectRoot = projectPath
	s.indexVersion++
	s.mu.Unlock()

	_ = s.restartWatcher(projectPath) // non-fatal: see restartWatcher's doc comment
	return nil
}
```

(`s.indexVersion++` is added here too, alongside `OpenProject`'s existing success path — `WaitForIndexReady`, Task 6, needs the version to bump on *every* successful index, not just background ones.)

- [ ] **Step 4: Run the tests**

```bash
go test ./internal/codegraph/... -run TestServiceOpenProjectStartsWatcherNonFatally -v
go test ./internal/codegraph/... -race
```

Expected: passes; full package regression clean (in particular `TestServiceConcurrentAccessIsRaceFree` and `TestServiceOpenProjectSurfacesAnalyzerError` — the latter's existing contract, "a failed analyze leaves no active project", is unchanged since the watcher start happens strictly after the existing success path, never on the error path).

- [ ] **Step 5: Commit**

```bash
git add internal/codegraph/service.go internal/codegraph/service_test.go
git commit -m "feat(codegraph): OpenProject starts the file watcher"
```

---

## Task 6: `WaitForIndexReady`

**Files:**
- Modify: `internal/codegraph/service.go`
- Modify: `internal/codegraph/service_test.go`

- [ ] **Step 1: Write the failing tests**

```go
func TestServiceWaitForIndexReadyReturnsImmediatelyWhenIdle(t *testing.T) {
	svc := newTestService(t, fakeAnalyzer{result: sampleResult()})
	if err := svc.OpenProject(context.Background(), "/fake/project"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	t.Cleanup(func() { svc.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	version, err := svc.WaitForIndexReady(ctx)
	if err != nil {
		t.Fatalf("WaitForIndexReady: %v", err)
	}
	if version != 1 {
		t.Fatalf("version = %d, want 1 (one successful OpenProject)", version)
	}
}

func TestServiceWaitForIndexReadyBeforeOpenProjectFails(t *testing.T) {
	svc := NewService(nilStore{}, fakeAnalyzer{})
	if _, err := svc.WaitForIndexReady(context.Background()); !errors.Is(err, ErrNoActiveProject) {
		t.Fatalf("expected ErrNoActiveProject, got %v", err)
	}
}

func TestServiceWaitForIndexReadyRespectsContextCancellation(t *testing.T) {
	svc := newTestService(t, fakeAnalyzer{result: sampleResult()})
	if err := svc.OpenProject(context.Background(), "/fake/project"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	t.Cleanup(func() { svc.Close() })

	svc.mu.Lock()
	svc.reindexing = true // simulate a reindex that never finishes
	svc.mu.Unlock()
	defer func() {
		svc.mu.Lock()
		svc.reindexing = false
		svc.mu.Unlock()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := svc.WaitForIndexReady(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded, got %v", err)
	}
}
```

- [ ] **Step 2: Run to confirm failure**

```bash
go test ./internal/codegraph/... -run TestServiceWaitForIndexReady -v
```

Expected: FAIL — compile error, `WaitForIndexReady` doesn't exist yet.

- [ ] **Step 3: Implement**

In `internal/codegraph/service.go`:

```go
// WaitForIndexReady blocks until no background re-index is pending or
// running, then returns the current index version. Callers that just wrote
// a file (or otherwise expect a re-index) should call this before trusting
// any other query's result — see this plan's "Known limitation" note for the
// one narrow timing edge this does not close.
//
// Implemented as a short poll rather than a sync.Cond: the debounce interval
// is hundreds of milliseconds, so a 50ms poll is imperceptible, and it composes
// cleanly with ctx cancellation (sync.Cond.Wait does not, without extra
// goroutine plumbing this doesn't otherwise need).
func (s *Service) WaitForIndexReady(ctx context.Context) (int, error) {
	for {
		s.mu.RLock()
		active := s.active
		reindexing := s.reindexing
		version := s.indexVersion
		s.mu.RUnlock()

		if !active {
			return 0, ErrNoActiveProject
		}
		if !reindexing {
			return version, nil
		}

		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}
```

- [ ] **Step 4: Run the tests**

```bash
go test ./internal/codegraph/... -run TestServiceWaitForIndexReady -v
```

Expected: all 3 pass.

- [ ] **Step 5: Full package regression**

```bash
go test ./internal/codegraph/... -race
```

- [ ] **Step 6: Commit**

```bash
git add internal/codegraph/service.go internal/codegraph/service_test.go
git commit -m "feat(codegraph): Service.WaitForIndexReady"
```

---

## Task 7: `Service.Close` + `main.go` wiring

**Files:**
- Modify: `internal/codegraph/service.go`
- Modify: `internal/codegraph/service_test.go`
- Modify: `main.go`

Every test in Tasks 5-6 already calls `t.Cleanup(func() { svc.Close() })` — this task makes that call real instead of a no-op/compile error, and wires it into the app's shutdown path so a real run doesn't leak the watcher goroutine.

- [ ] **Step 1: Write the failing test**

```go
func TestServiceCloseStopsWatcher(t *testing.T) {
	svc := newTestService(t, fakeAnalyzer{result: sampleResult()})
	if err := svc.OpenProject(context.Background(), "/fake/project"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	svc.mu.RLock()
	done := svc.watchDone
	svc.mu.RUnlock()

	if err := svc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	select {
	case <-done:
		// watchLoop goroutine exited, as expected
	case <-time.After(time.Second):
		t.Fatal("watchLoop did not exit after Close")
	}
}
```

- [ ] **Step 2: Run to confirm failure**

```bash
go test ./internal/codegraph/... -run TestServiceCloseStopsWatcher -v
```

Expected: FAIL — compile error, `Close` doesn't exist yet.

- [ ] **Step 3: Implement**

In `internal/codegraph/service.go`:

```go
// Close stops the background file watcher, if one is running, and waits for
// its goroutine to fully exit. Safe to call even if no project was ever
// opened (watchCancel is nil then — a no-op). Does not close the Store; the
// container that constructed this Service owns that lifecycle separately
// (see internal/app/container.go's existing Database/SSH/Kafka Close calls).
func (s *Service) Close() error {
	s.mu.Lock()
	cancel := s.watchCancel
	done := s.watchDone
	s.mu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
	return nil
}
```

- [ ] **Step 4: Run the tests**

```bash
go test ./internal/codegraph/... -run TestServiceCloseStopsWatcher -v
go test ./internal/codegraph/... -race
```

Expected: all pass.

- [ ] **Step 5: Wire into `main.go`**

In `main.go`, add alongside the existing `defer container.Database.Close()` / `defer container.SSH.Close()` / `defer container.Kafka.Close()` (around line 195-197):

```go
	defer container.CodeGraph.Close()
```

- [ ] **Step 6: Build**

```bash
go build ./...
```

- [ ] **Step 7: Commit**

```bash
git add internal/codegraph/service.go internal/codegraph/service_test.go main.go
git commit -m "feat(codegraph): Service.Close stops the watcher; wired into app shutdown"
```

---

## Task 8: End-to-end test — real files, real `fsnotify`, real debounce

**Files:**
- Modify: `internal/codegraph/service_test.go`

Everything up to this task is either implementation or a unit test using `svc.reindexing = true` as a stand-in. This task proves the whole pipeline works together against a real temp directory and real OS-level file events — the only way to genuinely validate `fsnotify` wiring, matching how this session has validated every other concurrency-sensitive piece with a real, targeted test rather than trusting the code by inspection alone.

- [ ] **Step 1: Write the test**

```go
type countingAnalyzer struct {
	result AnalysisResult
	calls  *atomic.Int32
}

func (c countingAnalyzer) Analyze(ctx context.Context, projectPath string) (AnalysisResult, error) {
	c.calls.Add(1)
	return c.result, nil
}

func TestServiceAutoReindexOnFileChange(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Foo.java"), []byte("class Foo {}"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	var calls atomic.Int32
	svc := newTestService(t, countingAnalyzer{result: sampleResult(), calls: &calls})
	svc.debounceInterval = 20 * time.Millisecond
	if err := svc.OpenProject(context.Background(), dir); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	t.Cleanup(func() { svc.Close() })

	initialCalls := calls.Load()

	if err := os.WriteFile(filepath.Join(dir, "Foo.java"), []byte("class Foo { void bar() {} }"), 0o644); err != nil {
		t.Fatalf("modify file: %v", err)
	}
	// Give fsnotify time to actually deliver the OS-level event before
	// polling — WaitForIndexReady only observes state that has already been
	// set by the watch loop, it cannot wait for an event that hasn't arrived
	// yet (see this plan's "Known limitation"). 50ms is generous headroom
	// over real fsnotify delivery latency (typically sub-5ms locally) while
	// staying well under the debounce interval's own margin below.
	time.Sleep(50 * time.Millisecond)

	waitCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := svc.WaitForIndexReady(waitCtx); err != nil {
		t.Fatalf("WaitForIndexReady: %v", err)
	}

	if calls.Load() <= initialCalls {
		t.Fatalf("expected the analyzer to be called again after the file change, calls = %d (was %d)", calls.Load(), initialCalls)
	}
}

func TestServiceReopeningStopsWatchingOldProject(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()
	if err := os.WriteFile(filepath.Join(dirA, "Foo.java"), []byte("class Foo {}"), 0o644); err != nil {
		t.Fatalf("seed A: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dirB, "Bar.java"), []byte("class Bar {}"), 0o644); err != nil {
		t.Fatalf("seed B: %v", err)
	}

	var calls atomic.Int32
	svc := newTestService(t, countingAnalyzer{result: sampleResult(), calls: &calls})
	svc.debounceInterval = 20 * time.Millisecond
	if err := svc.OpenProject(context.Background(), dirA); err != nil {
		t.Fatalf("OpenProject A: %v", err)
	}
	if err := svc.OpenProject(context.Background(), dirB); err != nil {
		t.Fatalf("OpenProject B: %v", err)
	}
	t.Cleanup(func() { svc.Close() })

	callsAfterBothOpens := calls.Load()

	// Modify a file in the now-abandoned project A — must NOT trigger a
	// reindex; the watcher should have moved entirely to B.
	if err := os.WriteFile(filepath.Join(dirA, "Foo.java"), []byte("class Foo { void bar() {} }"), 0o644); err != nil {
		t.Fatalf("modify A: %v", err)
	}
	time.Sleep(100 * time.Millisecond) // comfortably longer than debounceInterval

	if calls.Load() != callsAfterBothOpens {
		t.Fatalf("expected no reindex from changes in the abandoned project, calls = %d (was %d)", calls.Load(), callsAfterBothOpens)
	}
}
```

Add `"sync/atomic"` to `internal/codegraph/service_test.go`'s imports.

- [ ] **Step 2: Run**

```bash
go test ./internal/codegraph/... -run "TestServiceAutoReindexOnFileChange|TestServiceReopeningStopsWatchingOldProject" -v -count=3
```

Expected: both pass, reliably across repeated runs (`-count=3` — this is the flakiness-prone class of test in this plan, worth running a few times before trusting it).

- [ ] **Step 3: Full package regression**

```bash
go test ./internal/codegraph/... -race -count=1
```

- [ ] **Step 4: Commit**

```bash
git add internal/codegraph/service_test.go
git commit -m "test(codegraph): end-to-end auto-reindex test against real fsnotify events"
```

---

## Task 9: Bridge method + MCP tool

**Files:**
- Modify: `internal/bridge/codegraph.go`
- Modify: `internal/bridge/codegraph_test.go`
- Modify: `internal/mcp/server.go`
- Modify: `internal/mcp/tools_codegraph.go`
- Modify: `internal/mcp/tools_codegraph_test.go`

No new capability grant — per the design doc, `CodeGraphWaitForIndexReady` uses `codegraph:read`/`project:active`, already granted.

- [ ] **Step 1: Bridge test**

Add to `internal/bridge/codegraph_test.go`:

```go
func TestCodeGraphWaitForIndexReady(t *testing.T) {
	s := newTestService(t)
	s.container.CodeGraph = codegraph.NewService(codegraph.NewNilStore(), fakeCodeGraphAnalyzer{result: codegraph.AnalysisResult{}})
	if err := s.container.CodeGraph.SetProjectPath(t.TempDir() + "/index.db"); err != nil {
		t.Fatalf("SetProjectPath: %v", err)
	}
	if err := s.container.CodeGraph.OpenProject(context.Background(), "/fake/project"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	t.Cleanup(func() { s.container.CodeGraph.Close() })

	version, err := s.CodeGraphWaitForIndexReady()
	if err != nil {
		t.Fatalf("CodeGraphWaitForIndexReady: %v", err)
	}
	if version != 1 {
		t.Fatalf("version = %d, want 1", version)
	}
}
```

- [ ] **Step 2: Run to confirm failure**

```bash
go test ./internal/bridge/... -run TestCodeGraphWaitForIndexReady -v
```

Expected: FAIL — compile error.

- [ ] **Step 3: Bridge implementation**

In `internal/bridge/codegraph.go`:

```go
func (s *AppService) CodeGraphWaitForIndexReady() (int, error) {
	result, err := s.call(context.Background(), "builtin.codegraph", "codegraph:read", "project:active", map[string]any{
		"action": "wait_for_index_ready",
	}, func(ctx context.Context) (any, error) {
		return s.container.CodeGraph.WaitForIndexReady(ctx)
	})
	if err != nil {
		return 0, err
	}
	return result.(int), nil
}
```

- [ ] **Step 4: Run bridge tests**

```bash
go test ./internal/bridge/... -run TestCodeGraph -v
```

Expected: all pass.

- [ ] **Step 5: MCP test**

Add to `internal/mcp/tools_codegraph_test.go` (extend `fakeCodeGraphAPI` with an `indexVersion int` field):

```go
func TestToolCodeGraphWaitForIndexReady(t *testing.T) {
	deps := Deps{
		Caller:    &fakeCaller{},
		CodeGraph: fakeCodeGraphAPI{indexVersion: 3},
	}
	s := NewServer(deps)
	_, v, err := s.toolCodeGraphWaitForIndexReady(context.Background(), nil, struct{}{})
	if err != nil {
		t.Fatalf("toolCodeGraphWaitForIndexReady: %v", err)
	}
	version, ok := v.(int)
	if !ok || version != 3 {
		t.Fatalf("unexpected result: %#v", v)
	}
}
```

```go
func (f fakeCodeGraphAPI) WaitForIndexReady(ctx context.Context) (int, error) {
	return f.indexVersion, f.err
}
```

- [ ] **Step 6: Run to confirm failure**

```bash
go test ./internal/mcp/... -run TestToolCodeGraphWaitForIndexReady -v
```

Expected: FAIL — compile error.

- [ ] **Step 7: Extend the interface and register the tool**

In `internal/mcp/server.go`, add to `CodeGraphAPI`:

```go
	WaitForIndexReady(ctx context.Context) (int, error)
```

In `internal/mcp/tools_codegraph.go`:

```go
	sdk.AddTool(s.mcp, &sdk.Tool{
		Name:        "codegraph.waitForIndexReady",
		Description: "Block until any pending or in-progress re-index finishes, then return the current index version",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolCodeGraphWaitForIndexReady)
```

```go
func (s *Server) toolCodeGraphWaitForIndexReady(ctx context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.codegraph", "codegraph:read", "project:active", meta("wait_for_index_ready", nil), func(context.Context) (any, error) {
		return s.deps.CodeGraph.WaitForIndexReady(ctx)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, v, nil
}
```

- [ ] **Step 8: Run the tests**

```bash
go test ./internal/mcp/... -run TestToolCodeGraph -v
```

Expected: all pass.

- [ ] **Step 9: Commit**

```bash
git add internal/bridge/codegraph.go internal/bridge/codegraph_test.go internal/mcp/server.go internal/mcp/tools_codegraph.go internal/mcp/tools_codegraph_test.go
git commit -m "feat(codegraph): expose waitForIndexReady via bridge and MCP"
```

---

## Task 10: Full regression

**Files:** none (verification only)

- [ ] **Step 1: Standard check**

```bash
go test ./internal/... ./cmd/... -count=1
go build ./...
```

- [ ] **Step 2: Race + repeat the flakiness-prone tests specifically**

```bash
go test ./internal/... ./cmd/... -race -count=1
go test ./internal/codegraph/... -run "TestServiceAutoReindexOnFileChange|TestServiceReopeningStopsWatchingOldProject" -race -count=5
```

Expected: everything passes, including 5 repeated runs of the two real-`fsnotify` tests — if any of those 5 fail, do not treat it as a one-off; go back and re-examine the debounce/cancel state machine in Task 4 (a genuine race, not test flakiness, is far more likely than the platform's `fsnotify` implementation itself being wrong).

---

## What this plan does NOT do

**True partial re-parse** — every trigger still does a full `Analyzer.Analyze`, per the scope decision at the top. **Does not modify `2026-08-19-codegraph-file-editing.md`'s `WriteFile`** to synchronously mark a pending reindex before returning, which would close this plan's one accepted micro-race entirely — flagged there as a follow-up integration point once both plans have landed, not built preemptively against a method this plan doesn't own. **No configurable debounce interval exposed to the user** — 750ms is a fixed constant, matching the design doc's "~500ms–1s" range; expose a preference only if real usage shows it needs tuning. **No `.gitignore`-aware filtering** in `addRecursive`, same deliberate simplification `ListFiles` already accepts (only `.git` itself is skipped) — a project with a huge generated/vendor directory will have every subdirectory watched, acceptable for v1.
