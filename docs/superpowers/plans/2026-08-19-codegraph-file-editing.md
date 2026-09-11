# CodeGraph File Editing Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement Phase 2 of the CodeGraph rollout — the ["File editing (add-on v1)"](https://github.com — see `base-doc/content/projects/dev-kit/codegraph.mdx:230-300`) design: list/read/write any file in the active project (not just `.java`), plus `GetCodeSnippet` for the "click a graph node, see its code" flow already partially designed in the same doc's `codegraph.mdx:394-456`. This is backend-only (Go: Service + Store + bridge + MCP) — the file-panel UI itself is a separate future plan, same split already used for CodeGraph Core Go vs. its frontend module.

**Architecture:** Four new `codegraph.Service` methods (`ListFiles`, `ReadFile`, `WriteFile`, `GetCodeSnippet`) built on a new path-safety primitive (`resolvePath`) that re-validates every relative path against the active project's root on **every call**, per the design doc's explicit requirement (`FR-12`) — never trust a path the UI already resolved once. `GetCodeSnippet` additionally needs one new `Store` method, `FindNodeAtLine`, to snap an arbitrary line number to the enclosing method/class's real boundaries using the index already built by `OpenProject`. `WriteFile` is Service/bridge-only — per the design doc it is deliberately **not** exposed over MCP (agent-driven source edits are a materially bigger risk than the reads already exposed).

**Also fixed here:** a pre-existing bug found while grounding this plan — `internal/mcp/server.go`'s `CodeGraphAPI` interface has method signatures without `ctx context.Context`, while `codegraph.Service`'s real methods all take `ctx`. Because of this mismatch, `main.go`'s `mcp.Deps{...}` literal never sets a `CodeGraph` field at all (it would be a compile error to wire the real service), so the three CodeGraph MCP tools already registered (`codegraph.listEndpoints`, `codegraph.getApiFlow`, `codegraph.searchSymbol`) are dead in the running app today — `s.deps.CodeGraph` is `nil`, and calling any of them would panic. Task 1 below fixes this before adding new tools on top of it.

**Tech Stack:** Go only — `path/filepath` (`EvalSymlinks`, `Rel`, `WalkDir`) for path safety and directory listing, `os` for file I/O, `strings` for line-splitting. No new dependencies.

---

## File Structure

Modify:
- `internal/mcp/server.go` — fix `CodeGraphAPI` interface (add `ctx`), extend it with the 3 new read methods.
- `internal/mcp/tools_codegraph.go` — pass `ctx` through the 3 existing handlers, add 3 new tool registrations.
- `internal/mcp/tools_codegraph_test.go` — update `fakeCodeGraphAPI` to the corrected/extended interface.
- `main.go` — wire `CodeGraph: container.CodeGraph` into `mcp.Deps{...}`.
- `internal/codegraph/codegraph.go` — add `ErrPathOutsideProject`, `FileEntry`, `CodeSnippet` types; extend `Store` interface with `FindNodeAtLine`.
- `internal/codegraph/service.go` — add `projectRoot` field, `activeProject()` helper (refactor of `activeStore()`), `ListFiles`, `ReadFile`, `WriteFile`, `GetCodeSnippet`.
- `internal/codegraph/service_test.go` — tests for the 4 new Service methods.
- `internal/codegraph/paths.go` — add `resolvePath(projectRoot, relativePath string) (string, error)` next to the existing `ProjectDBPath`.
- `internal/codegraph/paths_test.go` — traversal/symlink test cases for `resolvePath`.
- `internal/codegraph/store.go` — implement `FindNodeAtLine` on `sqliteStore`.
- `internal/codegraph/store_test.go` — narrowest-match test cases for `FindNodeAtLine` (create this file if it doesn't exist yet — check first).
- `internal/app/container.go` — add 2 new `codegraph:fs` capability grants.
- `internal/bridge/codegraph.go` — 4 new `AppService` methods.
- `internal/bridge/codegraph_test.go` — tests for the 4 new bridge methods.

---

## Task 1: Fix the pre-existing CodeGraph MCP wiring bug

**Files:**
- Modify: `internal/mcp/server.go:105-110`
- Modify: `internal/mcp/tools_codegraph.go`
- Modify: `internal/mcp/tools_codegraph_test.go`
- Modify: `main.go:211-223`

- [ ] **Step 1: Confirm the bug**

```bash
grep -n "CodeGraph" main.go
```

Expected: no output — `mcp.Deps{...}` at `main.go:211-223` has no `CodeGraph:` field, confirming the tools are currently wired to a `nil` `CodeGraphAPI`.

- [ ] **Step 2: Fix the interface**

In `internal/mcp/server.go`, replace the `CodeGraphAPI` interface (lines 105-110):

```go
// CodeGraphAPI is the codegraph domain surface MCP tools invoke.
type CodeGraphAPI interface {
	ListEndpoints(ctx context.Context) ([]codegraph.Endpoint, error)
	GetApiFlow(ctx context.Context, endpointID string) (codegraph.ApiFlow, error)
	SearchSymbol(ctx context.Context, query string) ([]codegraph.SymbolMatch, error)
}
```

(`context` is already imported in this file — every other `*API` interface in it, e.g. `DatabaseAPI` at line 90, already takes `ctx`.)

- [ ] **Step 3: Update the 3 existing handlers to pass `ctx` through**

In `internal/mcp/tools_codegraph.go`, each handler already receives `ctx context.Context` as its first parameter — just forward it instead of calling with no arguments:

```go
func (s *Server) toolCodeGraphListEndpoints(ctx context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.codegraph", "codegraph:read", "project:active", meta("list_endpoints", nil), func(context.Context) (any, error) {
		return s.deps.CodeGraph.ListEndpoints(ctx)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, v, nil
}
```

Same one-line change (`s.deps.CodeGraph.X(...)` → `s.deps.CodeGraph.X(ctx, ...)`) in `toolCodeGraphGetApiFlow` and `toolCodeGraphSearchSymbol`.

- [ ] **Step 4: Update the test fake**

In `internal/mcp/tools_codegraph_test.go`, `fakeCodeGraphAPI`'s methods need `ctx context.Context` as their first parameter to satisfy the corrected interface:

```go
func (f fakeCodeGraphAPI) ListEndpoints(ctx context.Context) ([]codegraph.Endpoint, error) {
	return f.endpoints, f.err
}
func (f fakeCodeGraphAPI) GetApiFlow(ctx context.Context, id string) (codegraph.ApiFlow, error) {
	return f.flow, f.err
}
func (f fakeCodeGraphAPI) SearchSymbol(ctx context.Context, q string) ([]codegraph.SymbolMatch, error) {
	return f.matches, f.err
}
```

- [ ] **Step 5: Wire the real service in `main.go`**

In `main.go`, add `CodeGraph: container.CodeGraph,` to the `mcp.Deps{...}` literal at line 211-223 — alongside the other real container services (`SSH: container.SSH`, `DB: container.Database`, `Kafka: container.Kafka`), not through `appService` (consistent with how those three are wired: the MCP-level `Caller.Call` in each tool handler is the capability gate, not a second bridge-level check):

```go
	mcpServer := mcp.NewServerFromContainer(mcp.Deps{
		Caller:    appService,
		Notes:     container.Notes,
		Vault:     container.Vault,
		Search:    mcp.NewSearchAPI(container.Items),
		Tools:     container.HashTools,
		Envs:      container.Environments,
		SSH:       container.SSH,
		DB:        container.Database,
		Kafka:     container.Kafka,
		CodeGraph: container.CodeGraph,
		AgentHub:  container.AgentHub,
		Hub:       hub,
	})
```

- [ ] **Step 6: Build and test**

```bash
go build ./...
go test ./internal/mcp/... -run TestToolCodeGraph -v
```

Expected: builds clean (confirms `*codegraph.Service` now actually satisfies `CodeGraphAPI`), `TestToolCodeGraphListEndpoints` passes.

- [ ] **Step 7: Commit**

```bash
git add internal/mcp/server.go internal/mcp/tools_codegraph.go internal/mcp/tools_codegraph_test.go main.go
git commit -m "fix(mcp): wire the real CodeGraph service into MCP Deps

CodeGraphAPI's ctx-less signatures didn't match codegraph.Service,
so main.go silently left the field nil and all 3 codegraph MCP tools
were dead (nil dereference if ever called)."
```

---

## Task 2: `Service` tracks the active project's root path

**Files:**
- Modify: `internal/codegraph/service.go`
- Modify: `internal/codegraph/service_test.go`

Every new method in this plan (`ListFiles`/`ReadFile`/`WriteFile`/`GetCodeSnippet`) needs to know the active project's root folder to validate paths against. `OpenProject(ctx, projectPath)` already receives it but never stores it.

- [ ] **Step 1: Write the failing test**

Add to `internal/codegraph/service_test.go`:

```go
func TestServiceOpenProjectThenListFilesUsesProjectRoot(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	svc := NewService(nilStore{}, fakeAnalyzer{result: sampleResult()})
	if err := svc.SetProjectPath(filepath.Join(t.TempDir(), "index.db")); err != nil {
		t.Fatalf("SetProjectPath: %v", err)
	}
	if err := svc.OpenProject(context.Background(), dir); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	entries, err := svc.ListFiles(context.Background())
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	if len(entries) != 1 || entries[0].Path != "a.txt" {
		t.Fatalf("unexpected entries: %+v", entries)
	}
}
```

(`os` is not yet imported in this test file — add it; `filepath` is already used per `internal/codegraph/service_test.go:186`.)

- [ ] **Step 2: Run it to confirm it fails**

```bash
go test ./internal/codegraph/... -run TestServiceOpenProjectThenListFiles -v
```

Expected: FAIL — `ListFiles` doesn't exist yet (compile error). This is expected; Task 5 implements `ListFiles` itself. For now, just confirm `projectRoot` plumbing compiles by temporarily stubbing `ListFiles` to return `nil, nil` — no, simpler: skip running this specific test until Task 5; for this task, verify the plumbing with a narrower check instead (Step 3 below adds a lower-level test that doesn't need `ListFiles`).

Replace the test above with one that doesn't depend on a not-yet-written method — test the internal accessor directly instead:

```go
func TestServiceOpenProjectSetsProjectRoot(t *testing.T) {
	svc := NewService(nilStore{}, fakeAnalyzer{result: sampleResult()})
	if err := svc.SetProjectPath(filepath.Join(t.TempDir(), "index.db")); err != nil {
		t.Fatalf("SetProjectPath: %v", err)
	}
	if err := svc.OpenProject(context.Background(), "/fake/project"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	_, root, err := svc.activeProject()
	if err != nil {
		t.Fatalf("activeProject: %v", err)
	}
	if root != "/fake/project" {
		t.Fatalf("projectRoot = %q, want /fake/project", root)
	}
}
```

(Drop the `TestServiceOpenProjectThenListFilesUsesProjectRoot` test from Step 1 — Task 5 adds its own `ListFiles`-specific test instead, so this task stays self-contained.)

- [ ] **Step 3: Run it to confirm it fails**

```bash
go test ./internal/codegraph/... -run TestServiceOpenProjectSetsProjectRoot -v
```

Expected: FAIL — `activeProject` doesn't exist yet.

- [ ] **Step 4: Implement**

In `internal/codegraph/service.go`, add a field to `Service` (line 18-23):

```go
type Service struct {
	mu          sync.RWMutex
	store       Store
	analyzer    Analyzer
	active      bool
	projectRoot string
}
```

Set it in `OpenProject` (lines 52-65) — store it alongside `s.active = true`:

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

Replace `activeStore()` (lines 73-80) with a combined accessor, and make `activeStore()` a thin wrapper so `ListEndpoints`/`SearchSymbol`/`GetApiFlow` don't need to change:

```go
// activeProject returns the current store and project root under the read
// lock, so neither field is ever read concurrently with the writes
// SetProjectPath/OpenProject make to them under s.mu.Lock(). Both are used
// after the lock is released — *sql.DB (what sqliteStore wraps) and a plain
// string are both safe to use unsynchronized once captured.
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

- [ ] **Step 5: Run the tests**

```bash
go test ./internal/codegraph/... -run TestServiceOpenProjectSetsProjectRoot -v
go test ./internal/codegraph/... -race
```

Expected: new test passes; full package (including `TestServiceConcurrentAccessIsRaceFree`) still passes — `activeStore()`'s callers are unchanged, only its internals moved.

- [ ] **Step 6: Commit**

```bash
git add internal/codegraph/service.go internal/codegraph/service_test.go
git commit -m "feat(codegraph): Service tracks the active project's root path"
```

---

## Task 3: Path-safety primitive — `resolvePath`

**Files:**
- Modify: `internal/codegraph/paths.go`
- Modify: `internal/codegraph/paths_test.go`
- Modify: `internal/codegraph/codegraph.go`

This is the security boundary FR-12 requires: every relative path from the UI must resolve inside the project root, symlinks included, re-checked on every call (not cached from a prior check).

- [ ] **Step 1: Write the failing tests**

Add to `internal/codegraph/paths_test.go`:

```go
func TestResolvePathAllowsFileInsideRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	got, err := resolvePath(root, "a.txt")
	if err != nil {
		t.Fatalf("resolvePath: %v", err)
	}
	want, _ := filepath.EvalSymlinks(filepath.Join(root, "a.txt"))
	if got != want {
		t.Fatalf("resolvePath = %q, want %q", got, want)
	}
}

func TestResolvePathAllowsNestedNewFile(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// "new.txt" does not exist yet — WriteFile's first save of a new file
	// must still resolve, since EvalSymlinks fails on a nonexistent path.
	got, err := resolvePath(root, "src/new.txt")
	if err != nil {
		t.Fatalf("resolvePath: %v", err)
	}
	wantDir, _ := filepath.EvalSymlinks(filepath.Join(root, "src"))
	if filepath.Dir(got) != wantDir || filepath.Base(got) != "new.txt" {
		t.Fatalf("resolvePath = %q, want dir %q base new.txt", got, wantDir)
	}
}

func TestResolvePathRejectsDotDotTraversal(t *testing.T) {
	root := t.TempDir()
	if _, err := resolvePath(root, "../../etc/passwd"); !errors.Is(err, ErrPathOutsideProject) {
		t.Fatalf("resolvePath = %v, want ErrPathOutsideProject", err)
	}
}

func TestResolvePathRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("nope"), 0o644); err != nil {
		t.Fatalf("seed outside file: %v", err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(root, "link.txt")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if _, err := resolvePath(root, "link.txt"); !errors.Is(err, ErrPathOutsideProject) {
		t.Fatalf("resolvePath = %v, want ErrPathOutsideProject", err)
	}
}

func TestResolvePathRejectsAbsolutePathEscape(t *testing.T) {
	root := t.TempDir()
	if _, err := resolvePath(root, "/etc/passwd"); !errors.Is(err, ErrPathOutsideProject) {
		t.Fatalf("resolvePath = %v, want ErrPathOutsideProject", err)
	}
}
```

Add `"errors"` and `"os"` to the imports at the top of `internal/codegraph/paths_test.go` (currently just `"path/filepath"`, `"strings"`, `"testing"`).

- [ ] **Step 2: Run the tests to confirm they fail**

```bash
go test ./internal/codegraph/... -run TestResolvePath -v
```

Expected: FAIL — compile error, `resolvePath`/`ErrPathOutsideProject` don't exist yet.

- [ ] **Step 3: Implement**

In `internal/codegraph/codegraph.go`, add to the `var (...)` error block (lines 10-21):

```go
	// ErrPathOutsideProject is returned by ReadFile/WriteFile/GetCodeSnippet
	// when the resolved path (symlinks included) falls outside the active
	// project's root.
	ErrPathOutsideProject = errors.New("codegraph: path outside project root")
```

In `internal/codegraph/paths.go`, add below `ProjectDBPath`:

```go
// resolvePath validates that relativePath, once joined onto projectRoot and
// resolved through any symlinks, stays inside projectRoot. Returns the
// resolved absolute path, or ErrPathOutsideProject if it escapes.
//
// relativePath's target does not need to exist yet (WriteFile's first save
// of a brand new file resolves its not-yet-existing leaf name against its
// parent directory instead).
func resolvePath(projectRoot, relativePath string) (string, error) {
	resolvedRoot, err := filepath.EvalSymlinks(projectRoot)
	if err != nil {
		return "", fmt.Errorf("codegraph: resolve project root: %w", err)
	}

	joined := filepath.Join(projectRoot, relativePath)
	resolved, err := filepath.EvalSymlinks(joined)
	if err != nil {
		parent, parentErr := filepath.EvalSymlinks(filepath.Dir(joined))
		if parentErr != nil {
			return "", fmt.Errorf("codegraph: resolve path: %w", err)
		}
		resolved = filepath.Join(parent, filepath.Base(joined))
	}

	rel, err := filepath.Rel(resolvedRoot, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", ErrPathOutsideProject
	}
	return resolved, nil
}
```

`"fmt"` and `"strings"` need to be added to `internal/codegraph/paths.go`'s imports alongside the existing `"crypto/sha256"`, `"encoding/hex"`, `"path/filepath"`.

- [ ] **Step 4: Run the tests**

```bash
go test ./internal/codegraph/... -run TestResolvePath -v
```

Expected: all 5 pass.

- [ ] **Step 5: Commit**

```bash
git add internal/codegraph/paths.go internal/codegraph/paths_test.go internal/codegraph/codegraph.go
git commit -m "feat(codegraph): resolvePath path-traversal guard for file editing"
```

---

## Task 4: `Store.FindNodeAtLine` — snap a line number to its enclosing node

**Files:**
- Modify: `internal/codegraph/codegraph.go`
- Modify: `internal/codegraph/store.go`
- Create: `internal/codegraph/store_test.go` (check first — it may already exist from Phase 1a; if so, add to it instead)

- [ ] **Step 1: Check whether the file already exists**

```bash
ls internal/codegraph/store_test.go 2>/dev/null && echo exists || echo "does not exist"
```

If it exists, read it first and add the new test to it, matching its existing helper conventions (e.g. however it constructs a `*sqliteStore` for a test — likely `newSQLiteStore(filepath.Join(t.TempDir(), "index.db"))`). If it doesn't exist, create it fresh as shown below.

- [ ] **Step 2: Write the failing tests**

```go
// internal/codegraph/store_test.go
package codegraph

import (
	"context"
	"path/filepath"
	"testing"
)

func newTestStore(t *testing.T) *sqliteStore {
	t.Helper()
	store, err := newSQLiteStore(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatalf("newSQLiteStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestFindNodeAtLineReturnsNarrowestEnclosingNode(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	result := AnalysisResult{
		Nodes: []Node{
			{ID: "class1", Label: "com.acme.OrderService", Kind: "class", Layer: "service", FilePath: "OrderService.java", Line: 1, EndLine: 50},
			{ID: "method1", Label: "com.acme.OrderService.create", Kind: "method", Layer: "service", FilePath: "OrderService.java", Line: 10, EndLine: 20},
		},
	}
	if err := store.ReplaceIndex(ctx, result); err != nil {
		t.Fatalf("ReplaceIndex: %v", err)
	}

	node, ok, err := store.FindNodeAtLine(ctx, "OrderService.java", 15)
	if err != nil {
		t.Fatalf("FindNodeAtLine: %v", err)
	}
	if !ok {
		t.Fatal("expected a match")
	}
	if node.ID != "method1" {
		t.Fatalf("node.ID = %q, want method1 (the narrower method, not the enclosing class)", node.ID)
	}
}

func TestFindNodeAtLineNoMatch(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if err := store.ReplaceIndex(ctx, AnalysisResult{
		Nodes: []Node{{ID: "n1", Label: "com.acme.Foo", Kind: "class", Layer: "unknown", FilePath: "Foo.java", Line: 1, EndLine: 10}},
	}); err != nil {
		t.Fatalf("ReplaceIndex: %v", err)
	}

	_, ok, err := store.FindNodeAtLine(ctx, "Foo.java", 100)
	if err != nil {
		t.Fatalf("FindNodeAtLine: %v", err)
	}
	if ok {
		t.Fatal("expected no match for a line outside every node's range")
	}
}

func TestFindNodeAtLineWrongFile(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if err := store.ReplaceIndex(ctx, AnalysisResult{
		Nodes: []Node{{ID: "n1", Label: "com.acme.Foo", Kind: "class", Layer: "unknown", FilePath: "Foo.java", Line: 1, EndLine: 10}},
	}); err != nil {
		t.Fatalf("ReplaceIndex: %v", err)
	}

	_, ok, err := store.FindNodeAtLine(ctx, "Bar.java", 5)
	if err != nil {
		t.Fatalf("FindNodeAtLine: %v", err)
	}
	if ok {
		t.Fatal("expected no match for a different file")
	}
}
```

- [ ] **Step 3: Run the tests to confirm they fail**

```bash
go test ./internal/codegraph/... -run TestFindNodeAtLine -v
```

Expected: FAIL — compile error, `FindNodeAtLine` doesn't exist yet.

- [ ] **Step 4: Add to the `Store` interface**

In `internal/codegraph/codegraph.go`, add to the `Store` interface (lines 113-122):

```go
type Store interface {
	ReplaceIndex(ctx context.Context, result AnalysisResult) error
	HasIndex(ctx context.Context) (bool, error)
	ListEndpoints(ctx context.Context) ([]Endpoint, error)
	GetEndpoint(ctx context.Context, endpointID string) (Endpoint, error)
	GetNode(ctx context.Context, nodeID string) (Node, error)
	GetOutEdges(ctx context.Context, nodeID string) ([]Edge, error)
	SearchSymbol(ctx context.Context, query string) ([]SymbolMatch, error)
	FindNodeAtLine(ctx context.Context, filePath string, line int) (Node, bool, error)
	Close() error
}
```

- [ ] **Step 5: Implement on `sqliteStore`**

In `internal/codegraph/store.go`, add below `SearchSymbol`:

```go
// FindNodeAtLine returns the narrowest node in filePath whose [line, end_line]
// range contains line — e.g. a method nested inside a class both technically
// contain the line, but the method (the smaller range) is the useful answer
// for "what am I looking at". Returns ok=false, not an error, when nothing
// in the index covers that line (unindexed file, or line past EOF).
func (s *sqliteStore) FindNodeAtLine(ctx context.Context, filePath string, line int) (Node, bool, error) {
	var n Node
	err := s.db.QueryRowContext(ctx,
		`SELECT id, label, kind, layer, file_path, line, end_line FROM nodes
		 WHERE file_path = ? AND ? BETWEEN line AND end_line
		 ORDER BY (end_line - line) ASC LIMIT 1`,
		filePath, line,
	).Scan(&n.ID, &n.Label, &n.Kind, &n.Layer, &n.FilePath, &n.Line, &n.EndLine)
	if err == sql.ErrNoRows {
		return Node{}, false, nil
	}
	if err != nil {
		return Node{}, false, fmt.Errorf("codegraph: FindNodeAtLine: %w", err)
	}
	return n, true, nil
}
```

- [ ] **Step 6: Implement on `nilStore`**

In `internal/codegraph/service.go`, add next to `nilStore`'s other methods (around line 168):

```go
func (nilStore) FindNodeAtLine(context.Context, string, int) (Node, bool, error) {
	return Node{}, false, ErrNoActiveProject
}
```

- [ ] **Step 7: Run the tests**

```bash
go test ./internal/codegraph/... -run TestFindNodeAtLine -v
go build ./...
```

Expected: all 3 pass; build succeeds (confirms `sqliteStore` and `nilStore` both still satisfy `Store`).

- [ ] **Step 8: Commit**

```bash
git add internal/codegraph/codegraph.go internal/codegraph/store.go internal/codegraph/store_test.go internal/codegraph/service.go
git commit -m "feat(codegraph): Store.FindNodeAtLine snaps a line to its enclosing node"
```

---

## Task 5: `Service.ListFiles`

**Files:**
- Modify: `internal/codegraph/codegraph.go`
- Modify: `internal/codegraph/service.go`
- Modify: `internal/codegraph/service_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/codegraph/service_test.go` (needs `"os"` in imports, added in Task 2):

```go
func TestServiceListFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatalf("seed a.txt: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatalf("mkdir src: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src", "b.java"), []byte("class B {}"), 0o644); err != nil {
		t.Fatalf("seed b.java: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".git", "HEAD"), []byte("ref: refs/heads/main"), 0o644); err != nil {
		t.Fatalf("seed .git/HEAD: %v", err)
	}

	svc := NewService(nilStore{}, fakeAnalyzer{result: sampleResult()})
	if err := svc.SetProjectPath(filepath.Join(t.TempDir(), "index.db")); err != nil {
		t.Fatalf("SetProjectPath: %v", err)
	}
	if err := svc.OpenProject(context.Background(), dir); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	entries, err := svc.ListFiles(context.Background())
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}

	byPath := make(map[string]FileEntry, len(entries))
	for _, e := range entries {
		byPath[e.Path] = e
	}
	if e, ok := byPath["a.txt"]; !ok || e.IsDir {
		t.Fatalf("expected file entry a.txt, got %+v ok=%v", e, ok)
	}
	if e, ok := byPath["src"]; !ok || !e.IsDir {
		t.Fatalf("expected dir entry src, got %+v ok=%v", e, ok)
	}
	if e, ok := byPath[filepath.ToSlash(filepath.Join("src", "b.java"))]; !ok || e.IsDir {
		t.Fatalf("expected file entry src/b.java, got %+v ok=%v", e, ok)
	}
	if _, ok := byPath[".git"]; ok {
		t.Fatal(".git should be skipped")
	}
}

func TestServiceListFilesBeforeOpenProjectFails(t *testing.T) {
	svc := NewService(nilStore{}, fakeAnalyzer{})
	if _, err := svc.ListFiles(context.Background()); !errors.Is(err, ErrNoActiveProject) {
		t.Fatalf("expected ErrNoActiveProject, got %v", err)
	}
}
```

- [ ] **Step 2: Run to confirm failure**

```bash
go test ./internal/codegraph/... -run TestServiceListFiles -v
```

Expected: FAIL — compile error, `ListFiles`/`FileEntry` don't exist.

- [ ] **Step 3: Add `FileEntry`**

In `internal/codegraph/codegraph.go`, add near `SymbolMatch`:

```go
// FileEntry is one file or directory in the active project, relative to its
// root. Deliberately flat (not a nested tree) — simpler to produce here and
// just as easy for a frontend to build a tree UI from a sorted flat list.
type FileEntry struct {
	Path  string `json:"path"` // relative to project root, forward-slash separated
	IsDir bool   `json:"isDir"`
}
```

- [ ] **Step 4: Implement `ListFiles`**

In `internal/codegraph/service.go`, add below `GetApiFlow`:

```go
// ListFiles walks the active project's root and returns every file and
// directory (except .git) as a flat, path-sorted list. Symlinked entries are
// listed but not followed further — WalkDir does not descend into a symlink
// target, matching the same "don't silently trust symlinks" stance resolvePath
// enforces for individual reads/writes.
func (s *Service) ListFiles(ctx context.Context) ([]FileEntry, error) {
	_, root, err := s.activeProject()
	if err != nil {
		return nil, err
	}

	var entries []FileEntry
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		entries = append(entries, FileEntry{Path: filepath.ToSlash(rel), IsDir: d.IsDir()})
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("codegraph: list files: %w", walkErr)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return entries, nil
}
```

Add `"io/fs"`, `"path/filepath"`, and `"sort"` to `internal/codegraph/service.go`'s imports (currently `"context"`, `"fmt"`, `"sync"`, plus `"os"`/`"path/filepath"` added in Task 2 for `SetProjectPath`'s `MkdirAll` — `"path/filepath"` will already be there).

- [ ] **Step 5: Run the tests**

```bash
go test ./internal/codegraph/... -run TestServiceListFiles -v
```

Expected: both pass.

- [ ] **Step 6: Commit**

```bash
git add internal/codegraph/codegraph.go internal/codegraph/service.go internal/codegraph/service_test.go
git commit -m "feat(codegraph): Service.ListFiles"
```

---

## Task 6: `Service.ReadFile` and `Service.WriteFile`

**Files:**
- Modify: `internal/codegraph/service.go`
- Modify: `internal/codegraph/service_test.go`

- [ ] **Step 1: Write the failing tests**

```go
func TestServiceReadWriteFileRoundTrip(t *testing.T) {
	dir := t.TempDir()
	svc := NewService(nilStore{}, fakeAnalyzer{result: sampleResult()})
	if err := svc.SetProjectPath(filepath.Join(t.TempDir(), "index.db")); err != nil {
		t.Fatalf("SetProjectPath: %v", err)
	}
	if err := svc.OpenProject(context.Background(), dir); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	if err := svc.WriteFile(context.Background(), "notes.md", "# hello"); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := svc.ReadFile(context.Background(), "notes.md")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if got != "# hello" {
		t.Fatalf("ReadFile = %q, want %q", got, "# hello")
	}
}

func TestServiceReadFileRejectsPathOutsideProject(t *testing.T) {
	dir := t.TempDir()
	svc := NewService(nilStore{}, fakeAnalyzer{result: sampleResult()})
	if err := svc.SetProjectPath(filepath.Join(t.TempDir(), "index.db")); err != nil {
		t.Fatalf("SetProjectPath: %v", err)
	}
	if err := svc.OpenProject(context.Background(), dir); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	if _, err := svc.ReadFile(context.Background(), "../../etc/passwd"); !errors.Is(err, ErrPathOutsideProject) {
		t.Fatalf("ReadFile = %v, want ErrPathOutsideProject", err)
	}
}

func TestServiceWriteFileRejectsPathOutsideProject(t *testing.T) {
	dir := t.TempDir()
	svc := NewService(nilStore{}, fakeAnalyzer{result: sampleResult()})
	if err := svc.SetProjectPath(filepath.Join(t.TempDir(), "index.db")); err != nil {
		t.Fatalf("SetProjectPath: %v", err)
	}
	if err := svc.OpenProject(context.Background(), dir); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	if err := svc.WriteFile(context.Background(), "../../etc/passwd", "pwned"); !errors.Is(err, ErrPathOutsideProject) {
		t.Fatalf("WriteFile = %v, want ErrPathOutsideProject", err)
	}
}
```

- [ ] **Step 2: Run to confirm failure**

```bash
go test ./internal/codegraph/... -run TestServiceReadWriteFile -v
go test ./internal/codegraph/... -run TestServiceReadFileRejects -v
go test ./internal/codegraph/... -run TestServiceWriteFileRejects -v
```

Expected: FAIL — compile error, `ReadFile`/`WriteFile` don't exist on `Service` yet.

- [ ] **Step 3: Implement**

In `internal/codegraph/service.go`, add below `ListFiles`:

```go
func (s *Service) ReadFile(ctx context.Context, relativePath string) (string, error) {
	_, root, err := s.activeProject()
	if err != nil {
		return "", err
	}
	resolved, err := resolvePath(root, relativePath)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return "", fmt.Errorf("codegraph: read file: %w", err)
	}
	return string(data), nil
}

// WriteFile re-validates relativePath against the *current* project root
// (not whatever was active when the file was opened for editing) — the
// active project can change between a UI open and a Ctrl+S save.
func (s *Service) WriteFile(ctx context.Context, relativePath, content string) error {
	_, root, err := s.activeProject()
	if err != nil {
		return err
	}
	resolved, err := resolvePath(root, relativePath)
	if err != nil {
		return err
	}
	if err := os.WriteFile(resolved, []byte(content), 0o644); err != nil {
		return fmt.Errorf("codegraph: write file: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Run the tests**

```bash
go test ./internal/codegraph/... -run "TestServiceReadWriteFile|TestServiceReadFileRejects|TestServiceWriteFileRejects" -v
```

Expected: all 3 pass.

- [ ] **Step 5: Full package regression**

```bash
go test ./internal/codegraph/... -race
```

- [ ] **Step 6: Commit**

```bash
git add internal/codegraph/service.go internal/codegraph/service_test.go
git commit -m "feat(codegraph): Service.ReadFile and Service.WriteFile"
```

---

## Task 7: `Service.GetCodeSnippet`

**Files:**
- Modify: `internal/codegraph/codegraph.go`
- Modify: `internal/codegraph/service.go`
- Modify: `internal/codegraph/service_test.go`

- [ ] **Step 1: Write the failing tests**

```go
func TestServiceGetCodeSnippetSnapsToEnclosingNode(t *testing.T) {
	dir := t.TempDir()
	javaSrc := "package com.acme;\n\nclass OrderService {\n    void create() {\n        // line 5\n        // line 6\n    }\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "OrderService.java"), []byte(javaSrc), 0o644); err != nil {
		t.Fatalf("seed source: %v", err)
	}
	result := AnalysisResult{
		Nodes: []Node{
			{ID: "method1", Label: "com.acme.OrderService.create", Kind: "method", Layer: "service", FilePath: "OrderService.java", Line: 4, EndLine: 7},
		},
	}
	svc := NewService(nilStore{}, fakeAnalyzer{result: result})
	if err := svc.SetProjectPath(filepath.Join(t.TempDir(), "index.db")); err != nil {
		t.Fatalf("SetProjectPath: %v", err)
	}
	if err := svc.OpenProject(context.Background(), dir); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	// Ask for line 5 (mid-method) — the real answer should snap to the whole
	// method's 4-7 range, not just line 5.
	snippet, err := svc.GetCodeSnippet(context.Background(), "OrderService.java", 5, 5)
	if err != nil {
		t.Fatalf("GetCodeSnippet: %v", err)
	}
	if snippet.Line != 4 || snippet.EndLine != 7 {
		t.Fatalf("snippet bounds = %d-%d, want 4-7 (snapped to enclosing method)", snippet.Line, snippet.EndLine)
	}
	if snippet.FQN != "com.acme.OrderService.create" {
		t.Fatalf("snippet.FQN = %q, want com.acme.OrderService.create", snippet.FQN)
	}
	if snippet.FilePath != "OrderService.java" {
		t.Fatalf("snippet.FilePath = %q, want OrderService.java", snippet.FilePath)
	}
	if snippet.Language != "java" {
		t.Fatalf("snippet.Language = %q, want java", snippet.Language)
	}
	wantSource := "    void create() {\n        // line 5\n        // line 6\n    }"
	if snippet.Source != wantSource {
		t.Fatalf("snippet.Source = %q, want %q", snippet.Source, wantSource)
	}
}

func TestServiceGetCodeSnippetFallsBackToInputWhenNoNodeMatches(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("line1\nline2\nline3\n"), 0o644); err != nil {
		t.Fatalf("seed source: %v", err)
	}
	svc := NewService(nilStore{}, fakeAnalyzer{result: sampleResult()})
	if err := svc.SetProjectPath(filepath.Join(t.TempDir(), "index.db")); err != nil {
		t.Fatalf("SetProjectPath: %v", err)
	}
	if err := svc.OpenProject(context.Background(), dir); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	// README.md is never in the index (only .java files are analyzed), so no
	// node covers it — GetCodeSnippet must fall back to the input range as-is.
	snippet, err := svc.GetCodeSnippet(context.Background(), "README.md", 1, 2)
	if err != nil {
		t.Fatalf("GetCodeSnippet: %v", err)
	}
	if snippet.Line != 1 || snippet.EndLine != 2 {
		t.Fatalf("snippet bounds = %d-%d, want 1-2 (input, unchanged)", snippet.Line, snippet.EndLine)
	}
	if snippet.FQN != "" {
		t.Fatalf("snippet.FQN = %q, want empty (no node matched)", snippet.FQN)
	}
	if snippet.Language != "markdown" {
		t.Fatalf("snippet.Language = %q, want markdown", snippet.Language)
	}
	if snippet.Source != "line1\nline2" {
		t.Fatalf("snippet.Source = %q, want %q", snippet.Source, "line1\nline2")
	}
}

func TestServiceGetCodeSnippetRejectsPathOutsideProject(t *testing.T) {
	dir := t.TempDir()
	svc := NewService(nilStore{}, fakeAnalyzer{result: sampleResult()})
	if err := svc.SetProjectPath(filepath.Join(t.TempDir(), "index.db")); err != nil {
		t.Fatalf("SetProjectPath: %v", err)
	}
	if err := svc.OpenProject(context.Background(), dir); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	if _, err := svc.GetCodeSnippet(context.Background(), "../../etc/passwd", 1, 1); !errors.Is(err, ErrPathOutsideProject) {
		t.Fatalf("GetCodeSnippet = %v, want ErrPathOutsideProject", err)
	}
}
```

- [ ] **Step 2: Run to confirm failure**

```bash
go test ./internal/codegraph/... -run TestServiceGetCodeSnippet -v
```

Expected: FAIL — compile error, `GetCodeSnippet`/`CodeSnippet` don't exist yet.

- [ ] **Step 3: Add `CodeSnippet`**

In `internal/codegraph/codegraph.go`, add near `FileEntry`:

```go
// CodeSnippet is the result of GetCodeSnippet: the real source text for a
// method/class, plus the boundaries actually used (which may differ from
// the requested line/endLine — see GetCodeSnippet's doc comment).
type CodeSnippet struct {
	FilePath string `json:"filePath"`
	Line     int    `json:"line"`
	EndLine  int    `json:"endLine"`
	Source   string `json:"source"`
	Language string `json:"language"` // "java" | "json" | "yaml" | "markdown" | "" (unknown extension)
	FQN      string `json:"fqn"`      // empty if the snippet didn't match exactly one indexed symbol
}
```

- [ ] **Step 4: Implement**

In `internal/codegraph/service.go`, add below `WriteFile`:

```go
// GetCodeSnippet returns the real source text for filePath, snapped to the
// enclosing method/class's actual boundaries when the index has one covering
// line — the caller does not need to already know a method's exact line
// range, e.g. clicking anywhere inside a graph node's body should still show
// the whole method. Falls back to the input line/endLine verbatim when
// nothing in the index covers that line (a non-Java file, or an unindexed
// location) — still useful for arbitrary text files' File editing preview.
func (s *Service) GetCodeSnippet(ctx context.Context, filePath string, line, endLine int) (CodeSnippet, error) {
	store, root, err := s.activeProject()
	if err != nil {
		return CodeSnippet{}, err
	}
	resolved, err := resolvePath(root, filePath)
	if err != nil {
		return CodeSnippet{}, err
	}

	useLine, useEndLine, fqn := line, endLine, ""
	if node, ok, err := store.FindNodeAtLine(ctx, filePath, line); err != nil {
		return CodeSnippet{}, err
	} else if ok {
		useLine, useEndLine, fqn = node.Line, node.EndLine, node.Label
	}

	data, err := os.ReadFile(resolved)
	if err != nil {
		return CodeSnippet{}, fmt.Errorf("codegraph: read file: %w", err)
	}
	lines := strings.Split(string(data), "\n")
	if useLine < 1 || useEndLine > len(lines) || useLine > useEndLine {
		return CodeSnippet{}, fmt.Errorf("codegraph: line range %d-%d out of bounds for %q (%d lines)", useLine, useEndLine, filePath, len(lines))
	}
	source := strings.Join(lines[useLine-1:useEndLine], "\n")

	return CodeSnippet{
		FilePath: filePath,
		Line:     useLine,
		EndLine:  useEndLine,
		Source:   source,
		Language: languageForPath(filePath),
		FQN:      fqn,
	}, nil
}

func languageForPath(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".java":
		return "java"
	case ".json":
		return "json"
	case ".yaml", ".yml":
		return "yaml"
	case ".md":
		return "markdown"
	default:
		return ""
	}
}
```

Add `"strings"` to `internal/codegraph/service.go`'s imports.

- [ ] **Step 5: Run the tests**

```bash
go test ./internal/codegraph/... -run TestServiceGetCodeSnippet -v
```

Expected: all 3 pass.

- [ ] **Step 6: Full package regression**

```bash
go test ./internal/codegraph/... -race
```

- [ ] **Step 7: Commit**

```bash
git add internal/codegraph/codegraph.go internal/codegraph/service.go internal/codegraph/service_test.go
git commit -m "feat(codegraph): Service.GetCodeSnippet"
```

---

## Task 8: Capability grants

**Files:**
- Modify: `internal/app/container.go`

- [ ] **Step 1: Add the grants**

In `internal/app/container.go`, add next to the existing `builtin.codegraph` grants (lines 166-167):

```go
		{SubjectID: "builtin.codegraph", Capability: "codegraph:index", Scope: "project:new"},
		{SubjectID: "builtin.codegraph", Capability: "codegraph:read", Scope: "project:active"},
		{SubjectID: "builtin.codegraph", Capability: "codegraph:fs", Scope: "project:active"},
		{SubjectID: "builtin.codegraph", Capability: "codegraph:fs", Scope: "file:*"},
```

(`scopeMatches` in `internal/permissions/policy.go:36-46` already supports the `*` suffix as a prefix match — same pattern as `builtin.ssh`'s `Scope: "profile:*"` grant a few lines above this one.)

- [ ] **Step 2: Build**

```bash
go build ./...
```

Expected: succeeds (no behavior depends on this yet — Task 9 is the first caller).

- [ ] **Step 3: Commit**

```bash
git add internal/app/container.go
git commit -m "feat(app): grant codegraph:fs capability for file editing"
```

---

## Task 9: Bridge methods

**Files:**
- Modify: `internal/bridge/codegraph.go`
- Modify: `internal/bridge/codegraph_test.go`

- [ ] **Step 1: Write the failing tests**

Add to `internal/bridge/codegraph_test.go`:

```go
func TestCodeGraphListReadWriteFile(t *testing.T) {
	s := newTestService(t)
	s.container.CodeGraph = codegraph.NewService(codegraph.NewNilStore(), fakeCodeGraphAnalyzer{result: codegraph.AnalysisResult{}})
	s.container.ConfigDir = t.TempDir()
	dir := t.TempDir()

	if err := s.CodeGraphOpenProject(dir); err != nil {
		t.Fatalf("CodeGraphOpenProject: %v", err)
	}

	if err := s.CodeGraphWriteFile("notes.md", "# hello"); err != nil {
		t.Fatalf("CodeGraphWriteFile: %v", err)
	}
	got, err := s.CodeGraphReadFile("notes.md")
	if err != nil {
		t.Fatalf("CodeGraphReadFile: %v", err)
	}
	if got != "# hello" {
		t.Fatalf("CodeGraphReadFile = %q, want %q", got, "# hello")
	}

	entries, err := s.CodeGraphListFiles()
	if err != nil {
		t.Fatalf("CodeGraphListFiles: %v", err)
	}
	if len(entries) != 1 || entries[0].Path != "notes.md" {
		t.Fatalf("unexpected entries: %+v", entries)
	}
}

func TestCodeGraphGetCodeSnippet(t *testing.T) {
	s := newTestService(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Foo.java"), []byte("class Foo {\n    void bar() {\n    }\n}\n"), 0o644); err != nil {
		t.Fatalf("seed source: %v", err)
	}
	result := codegraph.AnalysisResult{
		Nodes: []codegraph.Node{
			{ID: "m1", Label: "com.acme.Foo.bar", Kind: "method", Layer: "unknown", FilePath: "Foo.java", Line: 2, EndLine: 3},
		},
	}
	s.container.CodeGraph = codegraph.NewService(codegraph.NewNilStore(), fakeCodeGraphAnalyzer{result: result})
	s.container.ConfigDir = t.TempDir()

	if err := s.CodeGraphOpenProject(dir); err != nil {
		t.Fatalf("CodeGraphOpenProject: %v", err)
	}

	snippet, err := s.CodeGraphGetCodeSnippet("Foo.java", 2, 2)
	if err != nil {
		t.Fatalf("CodeGraphGetCodeSnippet: %v", err)
	}
	if snippet.FQN != "com.acme.Foo.bar" {
		t.Fatalf("snippet.FQN = %q, want com.acme.Foo.bar", snippet.FQN)
	}
}
```

Add `"os"` and `"path/filepath"` to `internal/bridge/codegraph_test.go`'s imports (currently `"context"`, `"errors"`, `"testing"`, `"devkit/internal/codegraph"`).

- [ ] **Step 2: Run to confirm failure**

```bash
go test ./internal/bridge/... -run "TestCodeGraphListReadWriteFile|TestCodeGraphGetCodeSnippet" -v
```

Expected: FAIL — compile error, the 4 new `AppService` methods don't exist yet.

- [ ] **Step 3: Implement**

In `internal/bridge/codegraph.go`, add below `CodeGraphSearchSymbol`:

```go
func (s *AppService) CodeGraphListFiles() ([]codegraph.FileEntry, error) {
	result, err := s.call(context.Background(), "builtin.codegraph", "codegraph:fs", "project:active", map[string]any{
		"action": "list_files",
	}, func(ctx context.Context) (any, error) {
		return s.container.CodeGraph.ListFiles(ctx)
	})
	if err != nil {
		return nil, err
	}
	return result.([]codegraph.FileEntry), nil
}

func (s *AppService) CodeGraphReadFile(relativePath string) (string, error) {
	result, err := s.call(context.Background(), "builtin.codegraph", "codegraph:fs", "file:"+relativePath, map[string]any{
		"action":        "read_file",
		"relative_path": relativePath,
	}, func(ctx context.Context) (any, error) {
		return s.container.CodeGraph.ReadFile(ctx, relativePath)
	})
	if err != nil {
		return "", err
	}
	return result.(string), nil
}

func (s *AppService) CodeGraphWriteFile(relativePath, content string) error {
	_, err := s.call(context.Background(), "builtin.codegraph", "codegraph:fs", "file:"+relativePath, map[string]any{
		"action":        "write_file",
		"relative_path": relativePath,
	}, func(ctx context.Context) (any, error) {
		return nil, s.container.CodeGraph.WriteFile(ctx, relativePath, content)
	})
	return err
}

func (s *AppService) CodeGraphGetCodeSnippet(filePath string, line, endLine int) (codegraph.CodeSnippet, error) {
	result, err := s.call(context.Background(), "builtin.codegraph", "codegraph:fs", "file:"+filePath, map[string]any{
		"action":    "get_code_snippet",
		"file_path": filePath,
		"line":      line,
		"end_line":  endLine,
	}, func(ctx context.Context) (any, error) {
		return s.container.CodeGraph.GetCodeSnippet(ctx, filePath, line, endLine)
	})
	if err != nil {
		return codegraph.CodeSnippet{}, err
	}
	return result.(codegraph.CodeSnippet), nil
}
```

- [ ] **Step 4: Run the tests**

```bash
go test ./internal/bridge/... -run "TestCodeGraph" -v
```

Expected: all pass, including the pre-existing `TestCodeGraphOpenProjectThenListEndpoints`/`TestCodeGraphOpenProjectTwiceReusesSameStore`/`TestCodeGraphListEndpointsBeforeOpenProjectFails` from the path-wiring plan.

- [ ] **Step 5: Commit**

```bash
git add internal/bridge/codegraph.go internal/bridge/codegraph_test.go
git commit -m "feat(codegraph): bridge methods for ListFiles/ReadFile/WriteFile/GetCodeSnippet"
```

---

## Task 10: MCP tools — `listFiles`, `readFile`, `getCodeSnippet` (not `writeFile`)

**Files:**
- Modify: `internal/mcp/server.go`
- Modify: `internal/mcp/tools_codegraph.go`
- Modify: `internal/mcp/tools_codegraph_test.go`

Per the design doc (`codegraph.mdx:287-292`): `WriteFile` stays human-only, deliberately not exposed here — editing source directly through an agent is a materially bigger risk than the reads already exposed (`getApiFlow`/`searchSymbol`).

- [ ] **Step 1: Write the failing test**

Add to `internal/mcp/tools_codegraph_test.go`:

```go
func TestToolCodeGraphReadFile(t *testing.T) {
	deps := Deps{
		Caller:    &fakeCaller{},
		CodeGraph: fakeCodeGraphAPI{fileContent: "package com.acme;"},
	}
	s := NewServer(deps)
	_, v, err := s.toolCodeGraphReadFile(context.Background(), nil, codeGraphReadFileInput{RelativePath: "Foo.java"})
	if err != nil {
		t.Fatalf("toolCodeGraphReadFile: %v", err)
	}
	content, ok := v.(string)
	if !ok || content != "package com.acme;" {
		t.Fatalf("unexpected result: %#v", v)
	}
}
```

Extend `fakeCodeGraphAPI` in the same file with the 3 new fields/methods it now needs to satisfy the extended interface:

```go
type fakeCodeGraphAPI struct {
	endpoints   []codegraph.Endpoint
	flow        codegraph.ApiFlow
	matches     []codegraph.SymbolMatch
	files       []codegraph.FileEntry
	fileContent string
	snippet     codegraph.CodeSnippet
	err         error
}

func (f fakeCodeGraphAPI) ListEndpoints(ctx context.Context) ([]codegraph.Endpoint, error) {
	return f.endpoints, f.err
}
func (f fakeCodeGraphAPI) GetApiFlow(ctx context.Context, id string) (codegraph.ApiFlow, error) {
	return f.flow, f.err
}
func (f fakeCodeGraphAPI) SearchSymbol(ctx context.Context, q string) ([]codegraph.SymbolMatch, error) {
	return f.matches, f.err
}
func (f fakeCodeGraphAPI) ListFiles(ctx context.Context) ([]codegraph.FileEntry, error) {
	return f.files, f.err
}
func (f fakeCodeGraphAPI) ReadFile(ctx context.Context, relativePath string) (string, error) {
	return f.fileContent, f.err
}
func (f fakeCodeGraphAPI) GetCodeSnippet(ctx context.Context, filePath string, line, endLine int) (codegraph.CodeSnippet, error) {
	return f.snippet, f.err
}
```

- [ ] **Step 2: Run to confirm failure**

```bash
go test ./internal/mcp/... -run TestToolCodeGraphReadFile -v
```

Expected: FAIL — compile error, `toolCodeGraphReadFile`/`codeGraphReadFileInput` don't exist, and `CodeGraphAPI` doesn't require the 3 new methods yet.

- [ ] **Step 3: Extend the interface**

In `internal/mcp/server.go`, extend `CodeGraphAPI` (from Task 1's fixed version):

```go
type CodeGraphAPI interface {
	ListEndpoints(ctx context.Context) ([]codegraph.Endpoint, error)
	GetApiFlow(ctx context.Context, endpointID string) (codegraph.ApiFlow, error)
	SearchSymbol(ctx context.Context, query string) ([]codegraph.SymbolMatch, error)
	ListFiles(ctx context.Context) ([]codegraph.FileEntry, error)
	ReadFile(ctx context.Context, relativePath string) (string, error)
	GetCodeSnippet(ctx context.Context, filePath string, line, endLine int) (codegraph.CodeSnippet, error)
}
```

- [ ] **Step 4: Register the 3 new tools**

In `internal/mcp/tools_codegraph.go`, add to `registerCodeGraph`:

```go
	sdk.AddTool(s.mcp, &sdk.Tool{
		Name:        "codegraph.listFiles",
		Description: "List every file and directory in the active CodeGraph project (not just .java)",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolCodeGraphListFiles)

	sdk.AddTool(s.mcp, &sdk.Tool{
		Name:        "codegraph.readFile",
		Description: "Read the full contents of one file in the active CodeGraph project",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolCodeGraphReadFile)

	sdk.AddTool(s.mcp, &sdk.Tool{
		Name:        "codegraph.getCodeSnippet",
		Description: "Get the real source for a method/class, snapped to its actual boundaries from the index",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolCodeGraphGetCodeSnippet)
```

And the handlers, matching the existing file's style:

```go
func (s *Server) toolCodeGraphListFiles(ctx context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.codegraph", "codegraph:fs", "project:active", meta("list_files", nil), func(context.Context) (any, error) {
		return s.deps.CodeGraph.ListFiles(ctx)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, v, nil
}

type codeGraphReadFileInput struct {
	RelativePath string `json:"relativePath"`
}

func (s *Server) toolCodeGraphReadFile(ctx context.Context, _ *sdk.CallToolRequest, in codeGraphReadFileInput) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.codegraph", "codegraph:fs", "file:"+in.RelativePath, meta("read_file", map[string]any{"relative_path": in.RelativePath}), func(context.Context) (any, error) {
		return s.deps.CodeGraph.ReadFile(ctx, in.RelativePath)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, v, nil
}

type codeGraphGetCodeSnippetInput struct {
	FilePath string `json:"filePath"`
	Line     int    `json:"line"`
	EndLine  int    `json:"endLine"`
}

func (s *Server) toolCodeGraphGetCodeSnippet(ctx context.Context, _ *sdk.CallToolRequest, in codeGraphGetCodeSnippetInput) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.codegraph", "codegraph:fs", "file:"+in.FilePath, meta("get_code_snippet", map[string]any{"file_path": in.FilePath, "line": in.Line, "end_line": in.EndLine}), func(context.Context) (any, error) {
		return s.deps.CodeGraph.GetCodeSnippet(ctx, in.FilePath, in.Line, in.EndLine)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, v, nil
}
```

- [ ] **Step 5: Run the tests**

```bash
go test ./internal/mcp/... -run TestToolCodeGraph -v
```

Expected: all pass, including the pre-existing `TestToolCodeGraphListEndpoints`.

- [ ] **Step 6: Commit**

```bash
git add internal/mcp/server.go internal/mcp/tools_codegraph.go internal/mcp/tools_codegraph_test.go
git commit -m "feat(codegraph): expose listFiles/readFile/getCodeSnippet via MCP (writeFile stays human-only)"
```

---

## Task 11: Full regression

**Files:** none (verification only)

- [ ] **Step 1: Run the repo's documented standard check**

```bash
go test ./internal/... ./cmd/... -count=1
go build ./...
```

Expected: everything passes (per `AGENTS.md`).

- [ ] **Step 2: Run the full suite with `-race`**

```bash
go test ./internal/... ./cmd/... -race -count=1
```

Expected: passes, same as every prior CodeGraph plan in this sequence has verified.

- [ ] **Step 3: Note what's still not done**

No commit for this task — it's a checkpoint, not a change. Confirm out loud (in the plan's own execution notes, or to whoever is reviewing) that the file-panel UI (frontend `CodeGraphView.tsx` additions: file tree, Code editor wiring, Ctrl+S handler) is intentionally **not** part of this plan — same split as Core Go vs. its own frontend module plan. That's the natural next plan in the sequence once this one lands.

---

## What this plan does NOT do

No file-tree UI, no Code editor wiring, no Ctrl+S handling on the frontend — Go-side only, matching this plan's stated scope. No `.gitignore`-aware filtering in `ListFiles` (only `.git` itself is skipped — a deliberate, simple default; a project with a huge `node_modules` or `target/` directory will list it all, acceptable for v1, revisit only if a real project makes this genuinely slow). No preserving a file's original permission bits on `WriteFile` (always writes `0o644`) — acceptable since this module never touches executable scripts by design (Java/config/doc files only). No symlink-following inside `ListFiles`'s walk (a symlinked directory shows up as a non-dir-looking entry per `WalkDir`'s default behavior and isn't recursed into) — consistent with `resolvePath` treating symlinks as a boundary to check, not something to transparently follow.
