# CodeGraph Project Path Wiring Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close the one gap called out at the end of `2026-08-19-codegraph-frontend-module.md`: `CodeGraphOpenProject(path)` currently runs the analyzer against whatever store `internal/app/container.go` constructed at startup (`codegraph.NewNilStore()`), so every real `OpenProject` call fails with `ErrNoActiveProject` no matter what folder the user picks. This plan derives a real, persistent per-project SQLite path from the chosen folder and wires it in before the analyze runs, so the frontend module's folder picker → re-index → endpoint list flow actually works end to end.

**Root cause, precisely:** `internal/codegraph/service.go`'s `Service` has two methods — `SetProjectPath(dbPath string) error` (opens/creates a SQLite store at `dbPath` and makes it active) and `OpenProject(ctx, projectPath string) error` (runs the analyzer, replaces the index in whatever store is currently set). They are deliberately separate (see `SetProjectPath`'s doc comment: "Call this once per OpenProject, before running the analyzer"). Every existing Go test that opens a project calls both, in that order, with a `t.TempDir()`-derived path (`internal/codegraph/service_test.go:161-171`, `:184-190`). But `internal/bridge/codegraph.go`'s `CodeGraphOpenProject` — the only production caller — only calls `OpenProject`, never `SetProjectPath`, so in production the store stays whatever `container.go:218` set at construction (`NewNilStore()`), and `nilStore.ReplaceIndex` always returns `ErrNoActiveProject`.

**Fix, precisely:** teach the bridge method to derive a stable `dbPath` from the project folder path and call `SetProjectPath` before `OpenProject` — mirroring exactly what the existing Go tests already do by hand. `Service`'s public API does not change (no new methods, no signature changes) — this keeps every existing `internal/codegraph` test valid as-is.

**Path convention (verified against real precedent in this repo, not invented):** `internal/mcp/host.go:16-22` derives `filepath.Join(os.UserConfigDir(), "DevKit", "mcp.sock")`; `internal/cloudconfig/cache.go:24` derives `filepath.Join(configDir, "DevKit", "desktop-config.json")` from a `configDir` parameter that `main.go:108` passes in as the raw `os.UserConfigDir()` result (`main.go:54`, *before* `main.go:58` appends `/DevKit` to make its own `dataDir` for the vault SQLite file). The established pattern is: pass the **raw OS config dir** around, let each package append `"DevKit"` and its own subpath itself. This plan follows that pattern — `Container.ConfigDir` holds the raw value, and `codegraph.ProjectDBPath` appends `"DevKit"/"codegraph"/<hash>.db"` itself.

**Tech Stack:** Go only — `crypto/sha256` (stdlib, already used elsewhere for hashing in this repo's style) for turning an arbitrary filesystem path into a safe, fixed-length filename; no new dependencies.

---

## File Structure

Modify:
- `internal/codegraph/service.go` — add `os.MkdirAll` before opening the store in `SetProjectPath` (new subdirectories under `~/.config/DevKit/codegraph/` won't exist yet on first use).
- `internal/codegraph/paths.go` — **new file**, `ProjectDBPath(configDir, projectPath string) string`.
- `internal/app/container.go` — add `ConfigDir string` field to `Container`.
- `main.go` — set `container.ConfigDir = configDir` (the raw value already computed at line 54, before it's suffixed into `dataDir`).
- `internal/bridge/codegraph.go` — `CodeGraphOpenProject` derives `dbPath` and calls `SetProjectPath` before `OpenProject`.
- `internal/bridge/codegraph_test.go` — update `TestCodeGraphOpenProjectThenListEndpoints` to exercise the new derivation instead of manually calling `SetProjectPath`.

---

## Task 1: `ProjectDBPath` — derive a stable per-project SQLite path

**Files:**
- Create: `internal/codegraph/paths.go`
- Create: `internal/codegraph/paths_test.go`

- [ ] **Step 1: Write the test first**

```go
// internal/codegraph/paths_test.go
package codegraph

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectDBPathIsStableForSamePath(t *testing.T) {
	a := ProjectDBPath("/home/user/.config", "/Users/dev/my-project")
	b := ProjectDBPath("/home/user/.config", "/Users/dev/my-project")
	if a != b {
		t.Fatalf("ProjectDBPath not stable: %q != %q", a, b)
	}
}

func TestProjectDBPathDiffersForDifferentProjects(t *testing.T) {
	a := ProjectDBPath("/home/user/.config", "/Users/dev/project-a")
	b := ProjectDBPath("/home/user/.config", "/Users/dev/project-b")
	if a == b {
		t.Fatalf("ProjectDBPath collided for different projects: %q", a)
	}
}

func TestProjectDBPathUnderConfigDevKitCodegraph(t *testing.T) {
	got := ProjectDBPath("/home/user/.config", "/Users/dev/my-project")
	want := filepath.Join("/home/user/.config", "DevKit", "codegraph")
	if !strings.HasPrefix(got, want+string(filepath.Separator)) {
		t.Fatalf("ProjectDBPath = %q, want prefix %q", got, want)
	}
	if !strings.HasSuffix(got, ".db") {
		t.Fatalf("ProjectDBPath = %q, want .db suffix", got)
	}
}
```

- [ ] **Step 2: Implement**

```go
// internal/codegraph/paths.go
package codegraph

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
)

// ProjectDBPath derives the on-disk SQLite index path for a project folder.
// Same convention as internal/cloudconfig and internal/mcp: callers pass the
// raw OS config dir (os.UserConfigDir()), this appends "DevKit" and its own
// subpath. The project path is hashed rather than used as a filename since
// it may contain characters invalid in a path segment (and to keep the
// resulting filename length-bounded regardless of how deep the project
// folder is nested).
func ProjectDBPath(configDir, projectPath string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(projectPath)))
	name := hex.EncodeToString(sum[:]) + ".db"
	return filepath.Join(configDir, "DevKit", "codegraph", name)
}
```

- [ ] **Step 3: Run the tests**

```bash
go test ./internal/codegraph/... -run TestProjectDBPath -v
```

Expected: all 3 pass.

- [ ] **Step 4: Commit**

```bash
git add internal/codegraph/paths.go internal/codegraph/paths_test.go
git commit -m "feat(codegraph): derive a stable per-project SQLite index path"
```

---

## Task 2: `SetProjectPath` creates its parent directory

**Files:**
- Modify: `internal/codegraph/service.go`
- Modify: `internal/codegraph/service_test.go`

`ProjectDBPath` returns a path like `~/.config/DevKit/codegraph/<hash>.db`, and nothing has created the `codegraph/` subdirectory yet on a fresh install — `sql.Open("sqlite", path)` fails if its parent directory doesn't exist. Every existing test happens to use `t.TempDir()`, which already exists, so this gap has never been exercised.

- [ ] **Step 1: Write the test first**

Add to `internal/codegraph/service_test.go` (matches the existing `TestServiceSetProjectPathSwapsStore` test right below it, per `internal/codegraph/service_test.go:160-172`):

```go
func TestServiceSetProjectPathCreatesParentDir(t *testing.T) {
	svc := NewService(nilStore{}, fakeAnalyzer{})
	nested := filepath.Join(t.TempDir(), "does", "not", "exist", "yet", "index.db")
	if err := svc.SetProjectPath(nested); err != nil {
		t.Fatalf("SetProjectPath with nested new dir: %v", err)
	}
}
```

(`filepath` is already imported in this test file per `internal/codegraph/service_test.go:186`'s existing use of `filepath.Join`.)

- [ ] **Step 2: Implement**

In `internal/codegraph/service.go`, `SetProjectPath` (lines 33-47):

```go
func (s *Service) SetProjectPath(dbPath string) error {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
		return fmt.Errorf("codegraph: create project store dir: %w", err)
	}
	store, err := newSQLiteStore(dbPath)
	if err != nil {
		return fmt.Errorf("codegraph: open project store: %w", err)
	}
	s.mu.Lock()
	old := s.store
	s.store = store
	s.active = false
	s.mu.Unlock()
	if old != nil {
		old.Close()
	}
	return nil
}
```

Add `"os"` and `"path/filepath"` to the existing import block at the top of `internal/codegraph/service.go` (currently just `"context"`, `"fmt"`, `"sync"`).

- [ ] **Step 3: Run the tests**

```bash
go test ./internal/codegraph/... -run TestServiceSetProjectPath -v
```

Expected: both `TestServiceSetProjectPathSwapsStore` (pre-existing) and the new `TestServiceSetProjectPathCreatesParentDir` pass.

- [ ] **Step 4: Full package regression**

```bash
go test ./internal/codegraph/... -race
```

Expected: all pass, including `TestServiceConcurrentAccessIsRaceFree` (unaffected by this change — `MkdirAll` runs before the mutex is touched).

- [ ] **Step 5: Commit**

```bash
git add internal/codegraph/service.go internal/codegraph/service_test.go
git commit -m "fix(codegraph): SetProjectPath creates its parent directory if missing"
```

---

## Task 3: Thread the raw config dir through `Container`

**Files:**
- Modify: `internal/app/container.go`
- Modify: `main.go`

- [ ] **Step 1: Add the field**

In `internal/app/container.go`, add to the `Container` struct (near the other plain-value fields, not the service pointers — e.g. right after the `Container` struct's doc-commented mutable fields around line 90, or simplest: right after `CodeGraph *codegraph.Service` at line 74, since it exists specifically to support that service today, with a comment noting it's general-purpose):

```go
	// ConfigDir is the raw OS user-config directory (os.UserConfigDir()),
	// set by main before use. Empty in ephemeral/test containers built via
	// NewContainer/NewContainerWithStores — callers that need a real path
	// (e.g. CodeGraph's per-project SQLite index) must handle that case,
	// since not every test wants a real filesystem dependency.
	ConfigDir string
```

- [ ] **Step 2: Set it in `main.go`**

In `main.go`, right after `container := app.NewContainerWithStores(...)` (line 71-78) — alongside the existing post-construction wiring like `container.Preferences = ...` at line 79:

```go
	container.ConfigDir = configDir
```

(`configDir` is already in scope from line 54 — the raw `os.UserConfigDir()` result, captured *before* it's joined into `dataDir` at line 58. Do not use `dataDir` here — per the path convention established in Task 1, `ProjectDBPath` appends `"DevKit"` itself.)

- [ ] **Step 3: Build**

```bash
go build ./...
```

Expected: succeeds (this task only adds a field and sets it — no behavior depends on it yet until Task 4).

- [ ] **Step 4: Commit**

```bash
git add internal/app/container.go main.go
git commit -m "feat(app): thread the raw OS config dir into Container"
```

---

## Task 4: Wire it into the bridge method

**Files:**
- Modify: `internal/bridge/codegraph.go`
- Modify: `internal/bridge/codegraph_test.go`

- [ ] **Step 1: Update the existing test first**

`TestCodeGraphOpenProjectThenListEndpoints` (`internal/bridge/codegraph_test.go:23-45`) currently calls `SetProjectPath` by hand before `CodeGraphOpenProject`. Change it to exercise the derivation the bridge method will now do itself — set `s.container.ConfigDir` to a temp dir instead:

```go
func TestCodeGraphOpenProjectThenListEndpoints(t *testing.T) {
	s := newTestService(t)
	result := codegraph.AnalysisResult{
		Nodes:     []codegraph.Node{{ID: "n1", Label: "A.foo", Kind: "method", Layer: "controller"}},
		Endpoints: []codegraph.Endpoint{{ID: "ep1", HTTPMethod: "GET", Path: "/a", EntryNode: "n1"}},
	}
	s.container.CodeGraph = codegraph.NewService(codegraph.NewNilStore(), fakeCodeGraphAnalyzer{result: result})
	s.container.ConfigDir = t.TempDir()

	if err := s.CodeGraphOpenProject("/fake/project"); err != nil {
		t.Fatalf("CodeGraphOpenProject: %v", err)
	}

	endpoints, err := s.CodeGraphListEndpoints()
	if err != nil {
		t.Fatalf("CodeGraphListEndpoints: %v", err)
	}
	if len(endpoints) != 1 || endpoints[0].Path != "/a" {
		t.Fatalf("unexpected endpoints: %+v", endpoints)
	}
}
```

Also add a test for opening the *same* project path twice (the re-index case the frontend plan's manual QA script Step 5 exercises by hand — worth locking in as an automated test now that it's cheap to):

```go
func TestCodeGraphOpenProjectTwiceReusesSameStore(t *testing.T) {
	s := newTestService(t)
	result := codegraph.AnalysisResult{
		Nodes:     []codegraph.Node{{ID: "n1", Label: "A.foo", Kind: "method", Layer: "controller"}},
		Endpoints: []codegraph.Endpoint{{ID: "ep1", HTTPMethod: "GET", Path: "/a", EntryNode: "n1"}},
	}
	s.container.CodeGraph = codegraph.NewService(codegraph.NewNilStore(), fakeCodeGraphAnalyzer{result: result})
	s.container.ConfigDir = t.TempDir()

	if err := s.CodeGraphOpenProject("/fake/project"); err != nil {
		t.Fatalf("first CodeGraphOpenProject: %v", err)
	}
	if err := s.CodeGraphOpenProject("/fake/project"); err != nil {
		t.Fatalf("second CodeGraphOpenProject: %v", err)
	}

	endpoints, err := s.CodeGraphListEndpoints()
	if err != nil {
		t.Fatalf("CodeGraphListEndpoints: %v", err)
	}
	if len(endpoints) != 1 {
		t.Fatalf("expected 1 endpoint after re-index, got %d: %+v", len(endpoints), endpoints)
	}
}
```

- [ ] **Step 2: Run the tests (expect failure — bridge method not changed yet)**

```bash
go test ./internal/bridge/... -run TestCodeGraphOpenProject -v
```

Expected: FAILS — `ConfigDir` is set but `CodeGraphOpenProject` doesn't use it yet, so the store is still `NewNilStore()` and `OpenProject` returns `ErrNoActiveProject`. This is the expected red step.

- [ ] **Step 3: Implement**

In `internal/bridge/codegraph.go`:

```go
func (s *AppService) CodeGraphOpenProject(path string) error {
	_, err := s.call(context.Background(), "builtin.codegraph", "codegraph:index", "project:new", map[string]any{
		"action": "open_project",
	}, func(ctx context.Context) (any, error) {
		dbPath := codegraph.ProjectDBPath(s.container.ConfigDir, path)
		if err := s.container.CodeGraph.SetProjectPath(dbPath); err != nil {
			return nil, err
		}
		return nil, s.container.CodeGraph.OpenProject(ctx, path)
	})
	return err
}
```

This is the only change in this file — `CodeGraphListEndpoints`/`CodeGraphGetApiFlow`/`CodeGraphSearchSymbol` are unaffected, they already just read whatever store is currently active.

- [ ] **Step 4: Run the tests again**

```bash
go test ./internal/bridge/... -run TestCodeGraph -v
```

Expected: all pass, including the pre-existing `TestCodeGraphListEndpointsBeforeOpenProjectFails` (untouched — still expects `ErrNoActiveProject` since it never calls `CodeGraphOpenProject` at all).

- [ ] **Step 5: Full repo regression**

```bash
go test ./internal/... ./cmd/... -count=1
go build ./...
```

Expected: everything passes, matching this repo's documented standard check (`AGENTS.md`).

- [ ] **Step 6: Commit**

```bash
git add internal/bridge/codegraph.go internal/bridge/codegraph_test.go
git commit -m "fix(codegraph): CodeGraphOpenProject derives and sets a real per-project store path"
```

---

## Task 5: Manual verification against the frontend module

**Files:** none (verification only)

This closes the exact gap `2026-08-19-codegraph-frontend-module.md`'s Task 6 (manual QA script) flagged. If that plan has already been implemented by the time this one lands, re-run its Task 6, Steps 3-5 — Step 3's "analyzer not configured" error should now be the *only* remaining error path (no more `ErrNoActiveProject`), and Step 4/5 (real end-to-end open + re-index) should now work exactly as written there, once the Java analyzer jar is also wired.

- [ ] **Step 1:** If the frontend module isn't built yet, skip this task — there's no UI to click through. The Go-level tests in Tasks 1-4 are sufficient proof this plan's fix is correct on their own.

- [ ] **Step 2:** If it is built, run through frontend plan Task 6 Steps 2-3 again and confirm: opening any real folder no longer surfaces an `ErrNoActiveProject`-flavored message — only `ErrAnalyzerNotConfigured` (or a real result, if the Java analyzer is also wired) should be possible now.

---

## What this plan does NOT do

No cleanup of stale per-project index files when a project folder is deleted, renamed, or moved (the hash-derived filename simply orphans — acceptable for v1, matches this module's already-documented "manual full re-index button as a correctness fallback" stance on staleness; add a cleanup pass later only if `~/.config/DevKit/codegraph/` growing unbounded actually becomes a real problem). No UI surfacing of the derived `dbPath` anywhere — it's an internal implementation detail, same as `devkit.db`'s location isn't surfaced today either.
