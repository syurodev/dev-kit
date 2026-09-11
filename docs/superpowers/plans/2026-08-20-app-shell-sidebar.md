# App Shell Sidebar Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the `useSidebarContent` shell primitive (register/show split, mirrors `SplitPanel`), the generic `ui_state` persistence subsystem it needs for its toggle, and wire `CodeGraphView` as the first real consumer.

**Architecture:** Go: a new `internal/uistate` package (mirrors `internal/preferences`'s shape exactly) backed by a new `ui_state` table (mirrors the real `app_preferences` migration exactly — `key TEXT PRIMARY KEY, value TEXT NOT NULL`, no extra columns), exposed over the bridge as `UIStateGet`/`UIStateSet` under subject `builtin.uistate`. Frontend: a Zustand `persist` storage adapter over that bridge (first use of `persist` middleware in this codebase), a new shell primitive at `components/ui/sidebar-shell.tsx` (deliberately not `sidebar.tsx` — that name is taken by the vendored shadcn component `NotesDashboard` already uses privately), a shared `usePanelWidthsStore` so `SidebarSurface` and the existing `SplitPanelSlot` can read each other's live width for the dynamic resize clamp, and `CodeGraphView` split into a stateful view + a stateless `CodeGraphSidebar` pushed through the new hook.

**Tech Stack:** Go, SQLite, Wails v3 bridge, React 19, TypeScript, Zustand (`persist` middleware, new to this codebase), Tailwind v4, `framer-motion` (already used by `SplitPanelSlot`, mirrored here), `bun test`, Go `testing`.

**Spec:** `docs/superpowers/specs/2026-08-20-app-shell-sidebar-design.md` — read it first; this plan implements it task-by-task and does not repeat its reasoning.

---

## File structure

```
internal/uistate/
  uistate.go            Service + Repository interface
  memory.go              MemoryRepository
  sqlrepository.go       SQLRepository (table ui_state)
  uistate_test.go
internal/bridge/
  uistate.go              UIStateGet / UIStateSet
  uistate_test.go
internal/storage/sqlite.go        (modify: m16 migration, TableUIState const)
internal/app/container.go         (modify: UIState field + wiring)
main.go                           (modify: SQL uistate wiring, MinWidth/MinHeight)
frontend/src/bridge/uistate.ts    (new: bridge wrapper + uiStateStorage adapter)
frontend/src/stores/
  sidebarShell.ts                 (new: useSidebarOpenStore, persisted)
  panelWidths.ts                  (new: usePanelWidthsStore, shared by both resizable panels)
frontend/src/components/ui/
  sidebar-shell.tsx                (new: SidebarProvider, useSidebarContent, SidebarModuleIdContext)
  split-panel.tsx                  (modify: use the extracted resize hook)
frontend/src/workbench/
  useResizableWidth.ts              (new: shared pointer-resize hook)
  Workbench.tsx                     (modify: mount SidebarProvider/SidebarSurface, toggle button, Context wrap)
frontend/src/modules/codegraph/
  CodeGraphSidebar.tsx               (new: extracted presentational component)
  CodeGraphView.tsx                  (modify: push CodeGraphSidebar via useSidebarContent)
```

---

## Task 1: `internal/uistate` package (TDD)

**Files:**
- Create: `internal/uistate/uistate.go`
- Create: `internal/uistate/memory.go`
- Test: `internal/uistate/uistate_test.go`

Mirrors `internal/preferences/{preferences,memory,preferences_test}.go` exactly, generic instead of theme-typed — no validation, no default-value logic (unlike `preferences.Theme()`'s dark-default fallback): `ui_state` is a raw key-value store, callers own their own defaults.

- [ ] **Step 1: Write the failing tests**

```go
// internal/uistate/uistate_test.go
package uistate

import "testing"

func TestGetMissingKeyReturnsNotFound(t *testing.T) {
	service := NewService(NewMemoryRepository())
	_, found, err := service.Get("sidebar")
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatal("found = true, want false for an unset key")
	}
}

func TestSetGetRoundTrip(t *testing.T) {
	service := NewService(NewMemoryRepository())
	if err := service.Set("sidebar", `{"open":true}`); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, found, err := service.Get("sidebar")
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("found = false, want true")
	}
	if got != `{"open":true}` {
		t.Fatalf("value = %q, want %q", got, `{"open":true}`)
	}
}

func TestSetOverwritesExistingKey(t *testing.T) {
	service := NewService(NewMemoryRepository())
	if err := service.Set("sidebar", "first"); err != nil {
		t.Fatal(err)
	}
	if err := service.Set("sidebar", "second"); err != nil {
		t.Fatal(err)
	}
	got, _, err := service.Get("sidebar")
	if err != nil {
		t.Fatal(err)
	}
	if got != "second" {
		t.Fatalf("value = %q, want %q", got, "second")
	}
}

func TestKeysAreIndependent(t *testing.T) {
	service := NewService(NewMemoryRepository())
	if err := service.Set("sidebar", "a"); err != nil {
		t.Fatal(err)
	}
	if err := service.Set("other-key", "b"); err != nil {
		t.Fatal(err)
	}
	got, _, err := service.Get("sidebar")
	if err != nil {
		t.Fatal(err)
	}
	if got != "a" {
		t.Fatalf("sidebar = %q, want %q (must not leak from other-key)", got, "a")
	}
}
```

- [ ] **Step 2: Run to confirm the tests fail**

Run: `cd /Users/syuro/Workspace/PERSONAL/dev-kit/dev-kit-app && go test ./internal/uistate/...`
Expected: FAIL — package `internal/uistate` does not exist.

- [ ] **Step 3: Implement `Service` and `Repository`**

```go
// internal/uistate/uistate.go
// Package uistate owns generic, non-secret UI preferences that need to
// survive an app restart — a toggle, a panel width, a window bound. One
// key-value table, not a table per feature; see
// docs/superpowers/specs/2026-08-20-app-shell-sidebar-design.md.
package uistate

type Repository interface {
	Get(key string) (value string, found bool, err error)
	Set(key, value string) error
}

type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) Get(key string) (string, bool, error) {
	return s.repo.Get(key)
}

func (s *Service) Set(key, value string) error {
	return s.repo.Set(key, value)
}
```

- [ ] **Step 4: Implement `MemoryRepository`**

```go
// internal/uistate/memory.go
package uistate

import "sync"

type MemoryRepository struct {
	mu     sync.RWMutex
	values map[string]string
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{values: make(map[string]string)}
}

func (r *MemoryRepository) Get(key string) (string, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	value, found := r.values[key]
	return value, found, nil
}

func (r *MemoryRepository) Set(key, value string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.values[key] = value
	return nil
}
```

- [ ] **Step 5: Run the tests**

Run: `cd /Users/syuro/Workspace/PERSONAL/dev-kit/dev-kit-app && go test ./internal/uistate/...`
Expected: PASS, 4 tests.

- [ ] **Step 6: Commit**

```bash
git add internal/uistate/
git commit -m "feat(uistate): add generic UI preference service"
```

---

## Task 2: `ui_state` table migration

**Files:**
- Modify: `internal/storage/sqlite.go`

- [ ] **Step 1: Add the table constant**

In the `const (...)` block that has `TableAppPreferences = "app_preferences"` (`internal/storage/sqlite.go` around line 32), add directly after it:

```go
	TableUIState            = "ui_state"
```

- [ ] **Step 2: Add migration `m16`**

Directly after the `m15` migration block (the last one, ending with `runner, err := NewMigrationRunner(...)`), insert — this is `m14` ("create app preferences") with the table name and version changed, nothing else:

```go
	m16 := Migration{
		Version: 16,
		Name:    "create ui_state",
		Up: func(ctx context.Context) error {
			_, err := db.ExecContext(ctx, fmt.Sprintf(
				`CREATE TABLE IF NOT EXISTS %s (
					key TEXT PRIMARY KEY,
					value TEXT NOT NULL
				)`, TableUIState,
			))
			return err
		},
	}
```

- [ ] **Step 3: Register it in the runner**

Change:
```go
	runner, err := NewMigrationRunner(&sqlMigrationStore{db: db}, []Migration{m1, m2, m3, m4, m5, m6, m7, m8, m9, m10, m11, m12, m13, m14, m15})
```
to:
```go
	runner, err := NewMigrationRunner(&sqlMigrationStore{db: db}, []Migration{m1, m2, m3, m4, m5, m6, m7, m8, m9, m10, m11, m12, m13, m14, m15, m16})
```

- [ ] **Step 4: Write the migration test**

Mirror whatever pattern `internal/storage/migrations_test.go` already uses for asserting a table gets created (read that file first — do not guess its helper names). Add one test asserting that after running all migrations against a fresh in-memory DB, `SELECT 1 FROM ui_state LIMIT 0` succeeds (table exists, right columns).

- [ ] **Step 5: Run it**

Run: `cd /Users/syuro/Workspace/PERSONAL/dev-kit/dev-kit-app && go test ./internal/storage/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/storage/sqlite.go internal/storage/migrations_test.go
git commit -m "feat(storage): add ui_state table migration"
```

---

## Task 3: Wire `uistate` into the container

**Files:**
- Modify: `internal/app/container.go`
- Modify: `main.go`

- [ ] **Step 1: Add the field**

In `Container` struct (`internal/app/container.go`), directly after the `Preferences  *preferences.Service` line, add:

```go
	UIState      *uistate.Service
```

Add the import `"devkit/internal/uistate"` alongside the existing `"devkit/internal/preferences"` import.

- [ ] **Step 2: Wire the in-memory default**

In `NewContainerWithStores`'s returned struct literal, directly after `Preferences:  preferences.NewService(preferences.NewMemoryRepository()),`, add:

```go
		UIState:      uistate.NewService(uistate.NewMemoryRepository()),
```

- [ ] **Step 3: Add the permission grants**

`ui_state` values are dynamic per-key, so the scope is a wildcard, matching the existing `{SubjectID: "builtin.codegraph", Capability: "codegraph:fs", Scope: "file:*"}` pattern in the same grants list. Add, near the `builtin.settings` grants:

```go
		{SubjectID: "builtin.uistate", Capability: "ui:read", Scope: "key:*"},
		{SubjectID: "builtin.uistate", Capability: "ui:write", Scope: "key:*"},
```

- [ ] **Step 4: Wire the real SQL-backed service in `main.go`**

Directly after `container.Preferences = preferences.NewService(preferences.NewSQLRepository(db))` (`main.go:82`), add:

```go
	container.UIState = uistate.NewService(uistate.NewSQLRepository(db))
```

Add the `"devkit/internal/uistate"` import to `main.go`.

- [ ] **Step 5: Implement `SQLRepository`**

```go
// internal/uistate/sqlrepository.go
package uistate

import (
	"context"
	"database/sql"
)

type SQLRepository struct {
	db *sql.DB
}

func NewSQLRepository(db *sql.DB) *SQLRepository {
	return &SQLRepository{db: db}
}

func (r *SQLRepository) Get(key string) (string, bool, error) {
	var value string
	err := r.db.QueryRowContext(context.Background(),
		"SELECT value FROM ui_state WHERE key = ?", key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return value, true, nil
}

func (r *SQLRepository) Set(key, value string) error {
	_, err := r.db.ExecContext(context.Background(),
		`INSERT INTO ui_state (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}
```

- [ ] **Step 6: Build and run the full Go test suite**

Run: `cd /Users/syuro/Workspace/PERSONAL/dev-kit/dev-kit-app && go build ./... && go test ./...`
Expected: builds clean, all tests pass (including the new `uistate` package and the existing `internal/app` tests, which will now construct a `Container` with a non-nil `UIState` field).

- [ ] **Step 7: Commit**

```bash
git add internal/app/container.go internal/uistate/sqlrepository.go main.go
git commit -m "feat(uistate): wire SQL-backed service into the container"
```

---

## Task 4: Bridge — `UIStateGet` / `UIStateSet`

**Files:**
- Create: `internal/bridge/uistate.go`
- Test: `internal/bridge/uistate_test.go`

Mirrors `internal/bridge/preferences.go`'s `s.call(...)` shape. No native-apply/rollback step — `ui_state` has no native side effect to keep in sync, unlike theme.

`UIStateGet` returns `""` when the key was never set. This is safe specifically because every caller on the frontend side (Task 6) stores `JSON.stringify(value)` — `JSON.stringify` never produces an empty string for any value Zustand's `persist` middleware will ever pass it (`"false"`, `"null"`, `"0"` are all non-empty) — so an empty return is unambiguous. If a future `ui_state` consumer ever wants to legitimately store the empty string, this collapses; call it out then, not now.

- [ ] **Step 1: Write the failing test**

```go
// internal/bridge/uistate_test.go
package bridge

import (
	"testing"

	"devkit/internal/app"
)

func TestUIStateSetGetRoundTrip(t *testing.T) {
	service := NewAppService(app.NewContainer())
	if err := service.UIStateSet("sidebar", `{"open":true}`); err != nil {
		t.Fatalf("UIStateSet: %v", err)
	}
	got, err := service.UIStateGet("sidebar")
	if err != nil {
		t.Fatalf("UIStateGet: %v", err)
	}
	if got != `{"open":true}` {
		t.Fatalf("value = %q, want %q", got, `{"open":true}`)
	}
}

func TestUIStateGetMissingKeyReturnsEmptyString(t *testing.T) {
	service := NewAppService(app.NewContainer())
	got, err := service.UIStateGet("never-set")
	if err != nil {
		t.Fatalf("UIStateGet: %v", err)
	}
	if got != "" {
		t.Fatalf("value = %q, want empty string for an unset key", got)
	}
}
```

- [ ] **Step 2: Run to confirm it fails**

Run: `cd /Users/syuro/Workspace/PERSONAL/dev-kit/dev-kit-app && go test ./internal/bridge/... -run TestUIState`
Expected: FAIL — `UIStateGet`/`UIStateSet` undefined.

- [ ] **Step 3: Implement**

```go
// internal/bridge/uistate.go
package bridge

import (
	"context"
	"errors"
)

var errUIStateUnavailable = errors.New("ui state is not available")

func (s *AppService) UIStateGet(key string) (string, error) {
	result, err := s.call(context.Background(), "builtin.uistate", "ui:read", "key:"+key, map[string]any{
		"action": "get",
		"key":    key,
	}, func(context.Context) (any, error) {
		if s.container.UIState == nil {
			return nil, errUIStateUnavailable
		}
		value, _, err := s.container.UIState.Get(key)
		return value, err
	})
	if err != nil {
		return "", err
	}
	return result.(string), nil
}

func (s *AppService) UIStateSet(key, value string) error {
	_, err := s.call(context.Background(), "builtin.uistate", "ui:write", "key:"+key, map[string]any{
		"action": "set",
		"key":    key,
	}, func(context.Context) (any, error) {
		if s.container.UIState == nil {
			return nil, errUIStateUnavailable
		}
		return nil, s.container.UIState.Set(key, value)
	})
	return err
}
```

- [ ] **Step 4: Run the tests**

Run: `cd /Users/syuro/Workspace/PERSONAL/dev-kit/dev-kit-app && go test ./internal/bridge/... -run TestUIState`
Expected: PASS, 2 tests.

- [ ] **Step 5: Run the full suite once more**

Run: `cd /Users/syuro/Workspace/PERSONAL/dev-kit/dev-kit-app && go test ./...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/bridge/uistate.go internal/bridge/uistate_test.go
git commit -m "feat(uistate): expose UIStateGet/UIStateSet on the bridge"
```

---

## Task 5: Regenerate bindings, add the frontend bridge wrapper + storage adapter

**Files:**
- Create: `frontend/src/bridge/uistate.ts`
- Modify: generated `frontend/bindings/` (via codegen, not hand-edited)

- [ ] **Step 1: Regenerate bindings**

Run: `cd /Users/syuro/Workspace/PERSONAL/dev-kit/dev-kit-app && task generate:bindings`

- [ ] **Step 2: Confirm the new methods exist**

Run: `cd frontend && grep -n "UIStateGet\|UIStateSet" bindings/devkit/internal/bridge/appservice.ts`
Expected: both present, matching the shape of `PreferencesGetTheme`/`PreferencesSetTheme` in the same file.

- [ ] **Step 3: Write the bridge wrapper and storage adapter**

```ts
// frontend/src/bridge/uistate.ts
import { AppService } from "../../bindings/devkit/internal/bridge";
import { callBridge } from "./client";
import type { PersistStorage, StorageValue } from "zustand/middleware";

export function getUIState(key: string): Promise<string> {
  return callBridge(() => AppService.UIStateGet(key));
}

export function setUIState(key: string, value: string): Promise<void> {
  return callBridge(() => AppService.UIStateSet(key, value));
}

function debounce<Args extends unknown[]>(
  fn: (...args: Args) => void,
  delayMs: number,
): (...args: Args) => void {
  let timer: ReturnType<typeof setTimeout> | undefined;
  return (...args: Args) => {
    clearTimeout(timer);
    timer = setTimeout(() => fn(...args), delayMs);
  };
}

/**
 * One implementation of Zustand's PersistStorage interface, shared by every
 * store that needs to survive an app restart — see
 * docs/superpowers/specs/2026-08-20-app-shell-sidebar-design.md § ui_state.
 * getItem never debounces (read once at store init); setItem does, so a
 * resize drag doesn't hammer the bridge on every pixel.
 */
export const uiStateStorage: PersistStorage<unknown> = {
  getItem: async (key) => {
    const raw = await getUIState(key);
    if (!raw) return null;
    return JSON.parse(raw) as StorageValue<unknown>;
  },
  setItem: debounce((key: string, value: StorageValue<unknown>) => {
    void setUIState(key, JSON.stringify(value));
  }, 400),
  removeItem: (key) => {
    void setUIState(key, "");
  },
};
```

No `lodash` import — a shared debounce this small (5 lines) doesn't earn pulling in a dependency's module just for this, even though `lodash` is already installed; the earlier spec sketch used it, this is the smaller, equally-correct version.

- [ ] **Step 4: Typecheck**

Run: `cd frontend && bunx tsc --noEmit`
Expected: no errors. If `PersistStorage`'s `setItem`/`getItem` signatures don't match exactly what's written above, read `node_modules/zustand/middleware.d.ts` for the real signature and fix the adapter to match it — do not guess a second time.

- [ ] **Step 5: Commit**

```bash
git add frontend/bindings frontend/src/bridge/uistate.ts
git commit -m "feat(uistate): frontend bridge wrapper and Zustand persist adapter"
```

---

## Task 6: Native window minimum size

**Files:**
- Modify: `main.go`

- [ ] **Step 1: Add `MinWidth`/`MinHeight`**

In the `application.WebviewWindowOptions{...}` literal (`main.go`, around line 316), directly after `Height: 760,`, add:

```go
		// Floor derived from Sidebar (240px min) + its gutter (4px) + the
		// content area's own hard floor (480px) + SplitPanel's gutter (4px) +
		// SplitPanel (320px min), both open at once, plus the outer mx-1/mb-1
		// margins (8px) — see docs/superpowers/specs/2026-08-20-app-shell-
		// sidebar-design.md § Resize. No floor existed before this.
		MinWidth:  1080,
		MinHeight: 700,
```

Check the real field names on `application.WebviewWindowOptions` first (`grep -n "MinWidth\|MinHeight" $(go env GOPATH)/pkg/mod/github.com/wailsapp/wails/v3*/pkg/application/*.go` or equivalent for the vendored/module path this repo uses) — do not assume the Wails v3 beta API names these fields exactly this way without checking.

- [ ] **Step 2: Build**

Run: `cd /Users/syuro/Workspace/PERSONAL/dev-kit/dev-kit-app && go build ./...`
Expected: succeeds.

- [ ] **Step 3: Manual check**

Run the desktop app, try to resize the window below ~1080×700. Expected: the OS refuses to shrink it further.

- [ ] **Step 4: Commit**

```bash
git add main.go
git commit -m "feat: set a native window minimum size, derived from sidebar+panel floors"
```

---

## Task 7: Shared resize hook, extracted from `SplitPanelSlot`

**Files:**
- Create: `frontend/src/workbench/useResizableWidth.ts`
- Create: `frontend/src/stores/panelWidths.ts`
- Modify: `frontend/src/components/ui/split-panel.tsx`

Extracts the real pointer-resize logic already in `SplitPanelSlot` (`document.addEventListener("pointermove"/"pointerup")`, manual clamp, cursor/user-select styling) into one hook, and moves both panels' widths into one small store so `SidebarSurface` (Task 8/10) and `SplitPanelSlot` can each compute the other's live width for the dynamic clamp the spec requires. No behavior change for `SplitPanelSlot` in this task — same constants, same feel, verified by hand before moving on.

- [ ] **Step 1: Write the shared width store**

```ts
// frontend/src/stores/panelWidths.ts
import { create } from "zustand";

type PanelWidthsState = {
  sidebarWidth: number;
  splitPanelWidth: number;
  setSidebarWidth: (width: number) => void;
  setSplitPanelWidth: (width: number) => void;
};

/**
 * Shared only because the two panels' resize clamps depend on each other's
 * live width (Sidebar's max shrinks while SplitPanel is open wide, and vice
 * versa) — see docs/superpowers/specs/2026-08-20-app-shell-sidebar-
 * design.md § Resize. Zustand, not Context: same reasoning as
 * workbench/dock/dock-store.ts — two sibling components need read/write
 * access without every consumer re-rendering on every drag frame.
 */
export const usePanelWidthsStore = create<PanelWidthsState>((set) => ({
  sidebarWidth: 320,
  splitPanelWidth: 420,
  setSidebarWidth: (width) => set({ sidebarWidth: width }),
  setSplitPanelWidth: (width) => set({ splitPanelWidth: width }),
}));
```

- [ ] **Step 2: Extract the resize hook**

```ts
// frontend/src/workbench/useResizableWidth.ts
import { useEffect, useRef, useState } from "react";

/**
 * Pointer-drag resize with min/max clamping, animated via the caller's own
 * framer-motion element. Extracted from components/ui/split-panel.tsx's
 * original inline implementation — SidebarSurface (workbench/) is the
 * second consumer that made sharing it worth doing.
 */
export function useResizableWidth(opts: {
  width: number;
  onWidthChange: (width: number) => void;
  min: number;
  max: number;
  /** +1: dragging right grows the panel (Sidebar). -1: dragging left grows it (SplitPanel). */
  sign: 1 | -1;
}) {
  const { width, onWidthChange, min, max, sign } = opts;
  const [isResizing, setIsResizing] = useState(false);
  const resizeStart = useRef<{ x: number; width: number } | null>(null);

  useEffect(() => {
    if (!isResizing) return;
    function onMove(e: PointerEvent) {
      if (!resizeStart.current) return;
      const delta = sign * (e.clientX - resizeStart.current.x);
      onWidthChange(Math.min(max, Math.max(min, resizeStart.current.width + delta)));
    }
    function onUp() {
      resizeStart.current = null;
      setIsResizing(false);
    }
    document.addEventListener("pointermove", onMove);
    document.addEventListener("pointerup", onUp);
    document.body.style.cursor = "col-resize";
    document.body.style.userSelect = "none";
    return () => {
      document.removeEventListener("pointermove", onMove);
      document.removeEventListener("pointerup", onUp);
      document.body.style.cursor = "";
      document.body.style.userSelect = "";
    };
  }, [isResizing, min, max, sign, onWidthChange]);

  function onPointerDown(e: React.PointerEvent) {
    e.preventDefault();
    resizeStart.current = { x: e.clientX, width };
    setIsResizing(true);
  }

  return { isResizing, onPointerDown };
}
```

Note the sign flip from the original: `SplitPanelSlot`'s inline version computed `delta = resizeStart.current.x - e.clientX` (dragging *left* grows it, since it's on the right edge of the screen). `sign: -1` for `SplitPanelSlot` reproduces that exactly; `sign: 1` is for `SidebarSurface`, which is a mirror image (on the left edge, dragging *right* grows it).

- [ ] **Step 3: Refactor `SplitPanelSlot` to use both**

In `components/ui/split-panel.tsx`:
- Replace the local `const [width, setWidth] = React.useState(...)` with `usePanelWidthsStore((s) => [s.splitPanelWidth, s.setSplitPanelWidth])` (or two separate selector calls — either is fine, just don't subscribe to the whole store object).
- Replace the inline `isResizing`/`resizeStart`/`onMove`/`onUp`/`onResizePointerDown` block with a single call: `const { isResizing, onPointerDown } = useResizableWidth({ width, onWidthChange: setWidth, min: MIN_PANEL_WIDTH, max: MAX_PANEL_WIDTH, sign: -1 })`.
- Rename the gutter's `onPointerDown={onResizePointerDown}` to `onPointerDown={onPointerDown}`.
- Everything else (the `wasOpenRef` closed→open sync effect, the framer-motion `animate={{ width: ... }}`, the gutter JSX, the close button) is unchanged.

- [ ] **Step 4: Typecheck and manually verify no regression**

Run: `cd frontend && bunx tsc --noEmit`
Then run the app, open Notes, open the note-details `SplitPanel`, drag-resize it. Expected: identical feel to before this task — same min/max, same cursor, same animation. This is a pure refactor; if anything visibly changed, it's a bug in this task, not a feature of it.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/workbench/useResizableWidth.ts frontend/src/stores/panelWidths.ts frontend/src/components/ui/split-panel.tsx
git commit -m "refactor(split-panel): extract shared resize hook and panel-width store"
```

---

## Task 8: `useSidebarOpenStore` — persisted global toggle

**Files:**
- Create: `frontend/src/stores/sidebarShell.ts`

- [ ] **Step 1: Write the store**

```ts
// frontend/src/stores/sidebarShell.ts
import { create } from "zustand";
import { persist } from "zustand/middleware";
import { uiStateStorage } from "@/bridge/uistate";

type SidebarOpenState = {
  open: boolean;
  setOpen: (open: boolean) => void;
};

/**
 * Global, not per-module — closing the sidebar in one module closes it
 * everywhere until the user reopens it (docs/superpowers/specs/2026-08-20-
 * app-shell-sidebar-design.md). Default false (closed) matches the Figma
 * App Shell component's own default variant, Panel=Closed, Sidebar=Closed.
 */
export const useSidebarOpenStore = create<SidebarOpenState>()(
  persist(
    (set) => ({
      open: false,
      setOpen: (open) => set({ open }),
    }),
    { name: "sidebar", storage: uiStateStorage },
  ),
);
```

- [ ] **Step 2: Typecheck**

Run: `cd frontend && bunx tsc --noEmit`
Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/stores/sidebarShell.ts
git commit -m "feat(sidebar): persisted global open/closed store"
```

---

## Task 9: `useSidebarContent` primitive

**Files:**
- Create: `frontend/src/components/ui/sidebar-shell.tsx`

Named `sidebar-shell.tsx`, not `sidebar.tsx` — that file already exists and is the vendored shadcn component `NotesDashboard` uses privately (untouched by this plan, per the spec's explicit non-goal). Two files named `Sidebar` in this codebase would be a standing trap; this name says plainly which one is the shell primitive.

Implements the register/show split from the spec: modules register content continuously (keyed by module id, so a hidden-but-still-mounted module can't clobber the active one's content); the shell decides visibility independently via `useSidebarOpenStore` (Task 8).

- [ ] **Step 1: Write the primitive**

```tsx
// frontend/src/components/ui/sidebar-shell.tsx
import * as React from "react";
import { LazyMotion, domAnimation, m } from "framer-motion";
import { useResizableWidth } from "@/workbench/useResizableWidth";
import { usePanelWidthsStore } from "@/stores/panelWidths";
import { useSidebarOpenStore } from "@/stores/sidebarShell";
import { cn } from "@/lib/utils";

export const SidebarModuleIdContext = React.createContext<string | null>(null);

type SidebarShellContextValue = {
  content: Map<string, React.ReactNode>;
  setContent: (moduleId: string, node: React.ReactNode | null) => void;
};

const SidebarShellContext = React.createContext<SidebarShellContextValue | null>(null);

export function SidebarProvider({ children }: { children: React.ReactNode }) {
  const [content, setContentState] = React.useState<Map<string, React.ReactNode>>(new Map());

  const setContent = React.useCallback((moduleId: string, node: React.ReactNode | null) => {
    setContentState((prev) => {
      const next = new Map(prev);
      if (node === null) {
        next.delete(moduleId);
      } else {
        next.set(moduleId, node);
      }
      return next;
    });
  }, []);

  const value = React.useMemo(() => ({ content, setContent }), [content, setContent]);

  return <SidebarShellContext.Provider value={value}>{children}</SidebarShellContext.Provider>;
}

/**
 * Registers `content` under the calling module's id (read from
 * SidebarModuleIdContext, provided once per mounted module view — see
 * Workbench.tsx). Registration is independent of whether the sidebar is
 * currently shown; that's controlled globally by useSidebarOpenStore, not by
 * this hook. Pass null to unregister explicitly (or just unmount — the
 * effect cleanup does the same thing).
 */
export function useSidebarContent(content: React.ReactNode | null) {
  const moduleId = React.useContext(SidebarModuleIdContext);
  const ctx = React.useContext(SidebarShellContext);
  if (!ctx) {
    throw new Error("useSidebarContent must be used within a SidebarProvider");
  }
  const { setContent } = ctx;

  React.useEffect(() => {
    if (!moduleId) return;
    setContent(moduleId, content);
    return () => setContent(moduleId, null);
  }, [moduleId, content, setContent]);
}

const MIN_SIDEBAR_WIDTH = 240;
const MAX_SIDEBAR_WIDTH = 480;
const CONTENT_MIN_WIDTH = 480;
const GAP_WIDTH = 4; // matches components/ui/split-panel.tsx's GAP_WIDTH — one gutter width, everywhere.

/**
 * The rendered column. Flex sibling to the left of WorkbenchViews (see
 * Workbench.tsx) — mirrors SplitPanelSlot's box-in-box shape (own
 * rounded-xl/border/bg-card, not a shared card with an internal divider).
 */
export function SidebarSurface({ activeModuleId }: { activeModuleId: string | null }) {
  const ctx = React.useContext(SidebarShellContext);
  if (!ctx) {
    throw new Error("SidebarSurface must be used within a SidebarProvider");
  }
  const open = useSidebarOpenStore((s) => s.open);
  const sidebarWidth = usePanelWidthsStore((s) => s.sidebarWidth);
  const setSidebarWidth = usePanelWidthsStore((s) => s.setSidebarWidth);
  const splitPanelWidth = usePanelWidthsStore((s) => s.splitPanelWidth);

  // windowWidth intentionally re-read on resize only, not on every render —
  // this clamp only matters while actively dragging, and useResizableWidth
  // already re-derives min/max on every pointermove via these props.
  const [windowWidth, setWindowWidth] = React.useState(() => window.innerWidth);
  React.useEffect(() => {
    const onResize = () => setWindowWidth(window.innerWidth);
    window.addEventListener("resize", onResize);
    return () => window.removeEventListener("resize", onResize);
  }, []);

  const max = Math.min(
    MAX_SIDEBAR_WIDTH,
    windowWidth - splitPanelWidth - CONTENT_MIN_WIDTH - GAP_WIDTH * 2,
  );

  const { isResizing, onPointerDown } = useResizableWidth({
    width: sidebarWidth,
    onWidthChange: setSidebarWidth,
    min: MIN_SIDEBAR_WIDTH,
    max,
    sign: 1,
  });

  const content = activeModuleId ? ctx.content.get(activeModuleId) : null;

  return (
    <LazyMotion features={domAnimation}>
      <m.div
        initial={{ width: 0 }}
        animate={{ width: open ? sidebarWidth + GAP_WIDTH : 0 }}
        transition={isResizing ? { duration: 0 } : { duration: 0.22, ease: "easeInOut" }}
        className="flex min-h-0 shrink-0 overflow-hidden"
      >
        <div
          className={cn(
            "relative flex min-h-0 flex-1 flex-col overflow-hidden rounded-xl border border-border bg-card",
          )}
        >
          {content}
        </div>
        {open ? (
          <div
            role="separator"
            aria-orientation="vertical"
            aria-label="Resize sidebar"
            onPointerDown={onPointerDown}
            style={{ width: GAP_WIDTH }}
            className="group flex shrink-0 cursor-col-resize items-center justify-center"
          >
            <div
              className={cn(
                "h-full w-px bg-transparent transition-colors",
                isResizing ? "bg-ring" : "group-hover:bg-border",
              )}
            />
          </div>
        ) : null}
      </m.div>
    </LazyMotion>
  );
}
```

The gutter sits on the sidebar's *right* edge (opposite of `SplitPanelSlot`'s, which sits on its content's *left* edge) — correct, since Sidebar is the left-hand panel and the draggable edge is always the one facing the shared content area in the middle.

- [ ] **Step 2: Typecheck**

Run: `cd frontend && bunx tsc --noEmit`
Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/ui/sidebar-shell.tsx
git commit -m "feat(sidebar): useSidebarContent primitive and SidebarSurface"
```

---

## Task 10: Mount the primitive in `Workbench.tsx`

**Files:**
- Modify: `frontend/src/workbench/Workbench.tsx`

- [ ] **Step 1: Provide `SidebarModuleIdContext` per mounted module**

In `WorkbenchViews`'s per-tab render block:

```tsx
<div
  key={tab.id}
  className={tab.id === activeTabId ? "contents" : "hidden"}
>
  <Suspense fallback={...}>
    <TabView params={tab.params} />
  </Suspense>
</div>
```

wrap `<TabView params={tab.params} />` (leave the `Suspense` fallback outside the wrap, so it doesn't need a module id):

```tsx
<Suspense fallback={...}>
  <SidebarModuleIdContext.Provider value={tab.moduleId}>
    <TabView params={tab.params} />
  </SidebarModuleIdContext.Provider>
</Suspense>
```

Import `SidebarModuleIdContext` from `@/components/ui/sidebar-shell`.

- [ ] **Step 2: Wrap `Workbench` in `SidebarProvider`**

`Workbench`'s returned JSX is already wrapped in `<SplitPanelProvider>`. Nest `SidebarProvider` inside it (order doesn't matter — they're independent contexts, but nesting Sidebar inside SplitPanel keeps the two side-content providers adjacent in the tree, easier to find later):

```tsx
<SplitPanelProvider>
  <SidebarProvider>
    <div className="relative flex h-svh w-full flex-col overflow-hidden bg-transparent">
      {/* ...unchanged... */}
    </div>
  </SidebarProvider>
</SplitPanelProvider>
```

- [ ] **Step 3: Render `SidebarSurface` as the first child of `content-row`**

Change:
```tsx
<div className="mx-1 mb-1 flex min-h-0 flex-1">
  <WorkbenchViews ... />
  <SplitPanelSlot />
</div>
```
to:
```tsx
<div className="mx-1 mb-1 flex min-h-0 flex-1">
  <SidebarSurface activeModuleId={activeModule?.id ?? null} />
  <WorkbenchViews ... />
  <SplitPanelSlot />
</div>
```

`activeModule` is already computed in `Workbench()` (`const activeModule = availableModules.find(...) ?? availableModules[0]`) — reuse it, don't recompute.

- [ ] **Step 4: Add the toggle button**

In the header's right-hand icon cluster (`Workbench.tsx`, the `<div className="flex min-w-0 items-center justify-end gap-2">` block that currently renders the Search and Settings `Tooltip`+`Button` pairs), add a third one **after** Settings — pragmatic placement per the spec's explicit non-goal (the redesigned "right after traffic lights" position waits on the separate title-bar rewrite):

```tsx
<Tooltip>
  <TooltipTrigger asChild>
    <Button
      variant="ghost"
      size="icon-xs"
      aria-label="Toggle sidebar"
      aria-pressed={sidebarOpen}
      className={cn(sidebarOpen && "bg-accent text-foreground", "shrink-0 [--wails-draggable:no-drag]")}
      onClick={() => setSidebarOpen(!sidebarOpen)}
    >
      <PanelLeft className="size-3.5" />
    </Button>
  </TooltipTrigger>
  <TooltipContent side="bottom">Toggle sidebar</TooltipContent>
</Tooltip>
```

Add `PanelLeft` to the existing `import { Search, Settings as SettingsIcon } from "lucide-react";` line. Add, near the other store selectors in `Workbench()`:

```tsx
const sidebarOpen = useSidebarOpenStore((s) => s.open);
const setSidebarOpen = useSidebarOpenStore((s) => s.setOpen);
```

with `import { useSidebarOpenStore } from "../stores/sidebarShell";`.

- [ ] **Step 5: Typecheck and build**

Run: `cd frontend && bunx tsc --noEmit && bun run build:dev`
Expected: both succeed.

- [ ] **Step 6: Manual verification**

Run the desktop app. Toggle button appears after Settings, clicking it animates a 320px column open/closed on the left (empty — no module has registered content yet, so it's just the chrome). Resize it by dragging its right edge; it clamps at 240–480. Restart the app; the last open/closed state is remembered (confirms the `ui_state` round trip end to end).

- [ ] **Step 7: Commit**

```bash
git add frontend/src/workbench/Workbench.tsx
git commit -m "feat(sidebar): mount SidebarProvider/SidebarSurface and the titlebar toggle"
```

---

## Task 11: `CodeGraphSidebar` — extract the presentational component

**Files:**
- Create: `frontend/src/modules/codegraph/CodeGraphSidebar.tsx`

Moves the JSX currently inline in `CodeGraphView.tsx` (the project-open row, status/error text, the Endpoints/Files toggle, the endpoint list, the `FileTree`) into a new file, unchanged as markup — every prop below already exists as state or a handler in the current `CodeGraphView.tsx`; this task only relocates JSX and wires it through props.

- [ ] **Step 1: Write the component**

```tsx
// frontend/src/modules/codegraph/CodeGraphSidebar.tsx
import { Button } from "@/components/ui/button";
import { FileTree } from "./FileTree";
import type { CodeGraphEndpoint, CodeGraphFileEntry } from "@/bridge/codegraph";

export type CodeGraphSidebarProps = {
  projectPath: string;
  busy: boolean;
  status: string;
  error: string;
  endpoints: CodeGraphEndpoint[];
  activeTab: "endpoints" | "files";
  onTabChange: (tab: "endpoints" | "files") => void;
  files: CodeGraphFileEntry[];
  selectedFile: string | null;
  onOpenProject: () => void;
  onReindex: () => void;
  onSelectEndpoint: (endpoint: CodeGraphEndpoint) => void;
  onSelectFile: (path: string) => void;
};

export function CodeGraphSidebar({
  projectPath,
  busy,
  status,
  error,
  endpoints,
  activeTab,
  onTabChange,
  files,
  selectedFile,
  onOpenProject,
  onReindex,
  onSelectEndpoint,
  onSelectFile,
}: CodeGraphSidebarProps) {
  return (
    <div className="flex h-full flex-col gap-4 overflow-hidden p-4">
      <div className="flex items-center gap-2">
        <Button onClick={onOpenProject} disabled={busy} size="sm">
          Open Java project…
        </Button>
        {projectPath && (
          <Button onClick={onReindex} disabled={busy} variant="secondary" size="sm">
            Re-index
          </Button>
        )}
      </div>
      {projectPath && <span className="text-sm text-muted-foreground truncate">{projectPath}</span>}

      {status && <p className="text-sm text-muted-foreground">{status}</p>}
      {error && <p className="text-sm text-destructive">{error}</p>}

      <div className="flex items-center gap-2 border-b pb-2">
        <Button variant={activeTab === "endpoints" ? "secondary" : "ghost"} size="sm" onClick={() => onTabChange("endpoints")}>
          Endpoints
        </Button>
        <Button variant={activeTab === "files" ? "secondary" : "ghost"} size="sm" onClick={() => onTabChange("files")}>
          Files
        </Button>
      </div>

      {activeTab === "endpoints" ? (
        <ul className="flex-1 overflow-y-auto space-y-1">
          {endpoints.map((ep) => (
            <li key={ep.id}>
              <button
                className="w-full text-left rounded px-2 py-1 hover:bg-surface-raised transition-colors"
                onClick={() => onSelectEndpoint(ep)}
              >
                <span className="font-mono text-xs font-semibold uppercase text-status-info mr-1.5">{ep.httpMethod}</span>
                <span className="text-sm">{ep.path}</span>
              </button>
            </li>
          ))}
          {endpoints.length === 0 && !busy && (
            <li className="text-sm text-muted-foreground py-2">No endpoints yet — open a project to index it.</li>
          )}
        </ul>
      ) : (
        <div className="flex-1 overflow-hidden">
          <FileTree entries={files} selectedPath={selectedFile} onSelectFile={onSelectFile} />
        </div>
      )}
    </div>
  );
}
```

This is `CodeGraphView.tsx`'s current lines 172–194 (project row, status/error, tab toggle) and the endpoint `<ul>`/`<FileTree>` bodies from lines 198–213 and 237–239, unchanged as JSX — only the source of each value changed from a local variable/closure to a prop.

- [ ] **Step 2: Typecheck**

Run: `cd frontend && bunx tsc --noEmit`
Expected: this file typechecks in isolation; `CodeGraphView.tsx` will show errors until Task 12 updates it — that's expected at this point, don't chase it yet.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/modules/codegraph/CodeGraphSidebar.tsx
git commit -m "feat(codegraph): extract CodeGraphSidebar as a presentational component"
```

---

## Task 12: Wire `CodeGraphView` to push `CodeGraphSidebar` through the primitive

**Files:**
- Modify: `frontend/src/modules/codegraph/CodeGraphView.tsx`

- [ ] **Step 1: Replace the returned JSX**

Every piece of state (`projectPath`, `endpoints`, `subgraph`, `rootId`, `status`, `error`, `busy`, `activeTab`, `files`, `selectedFile`, `fileContent`, `fileDirty`, `fileError`) and every handler (`pickAndOpenProject`, `reindex`, `viewFlow`, `openFile`, `saveCurrentFile`) in `CodeGraphView.tsx` stays exactly as it is today — nothing about the data layer changes in this task. Only the return statement changes:

```tsx
import { useSidebarContent } from "@/components/ui/sidebar-shell";
import { CodeGraphSidebar } from "./CodeGraphSidebar";
// ...remove the FileTree import — CodeGraphSidebar owns it now.

export function CodeGraphView() {
  // ...all existing state and handlers, unchanged...

  useSidebarContent(
    <CodeGraphSidebar
      projectPath={projectPath}
      busy={busy}
      status={status}
      error={error}
      endpoints={endpoints}
      activeTab={activeTab}
      onTabChange={setActiveTab}
      files={files}
      selectedFile={selectedFile}
      onOpenProject={pickAndOpenProject}
      onReindex={() => reindex(projectPath)}
      onSelectEndpoint={viewFlow}
      onSelectFile={openFile}
    />,
  );

  return (
    <div className="flex h-full flex-col overflow-hidden">
      {activeTab === "endpoints" ? (
        subgraph && rootId ? (
          <div className="flex flex-1 flex-col overflow-hidden p-4">
            <span className="px-1 pb-2 font-mono text-[11px] text-text-tertiary">
              {subgraph.nodes.length} nodes · {subgraph.edges.length} edges
            </span>
            <div className="flex-1 overflow-hidden">
              <FlowCanvas key={rootId} rootId={rootId} nodes={subgraph.nodes} edges={subgraph.edges} />
            </div>
          </div>
        ) : (
          <p className="p-4 text-sm text-muted-foreground">Select an endpoint to see its flow.</p>
        )
      ) : selectedFile ? (
        <>
          <div className="flex items-center justify-between border-b px-2 py-1 text-sm text-muted-foreground">
            <span className="truncate font-mono">{selectedFile}</span>
            {fileDirty && <span className="text-status-warning">unsaved</span>}
          </div>
          <div className="flex-1 overflow-hidden">
            <CodeEditor
              value={fileContent}
              onChange={(v) => {
                setFileContent(v);
                setFileDirty(true);
              }}
              extensions={editorExtensions}
            />
          </div>
        </>
      ) : (
        <p className="p-4 text-sm text-muted-foreground">Select a file to edit it. Ctrl/Cmd+S saves.</p>
      )}
      {fileError && <p className="text-sm text-destructive">{fileError}</p>}
    </div>
  );
}
```

- [ ] **Step 2: Typecheck**

Run: `cd frontend && bunx tsc --noEmit`
Expected: no errors, including in `CodeGraphSidebar.tsx` (Task 11's expected errors are gone now that this file's props match).

- [ ] **Step 3: Build**

Run: `cd frontend && bun run build:dev`
Expected: succeeds.

- [ ] **Step 4: Manual verification against Figma `150:46`**

Run the desktop app, open the sidebar toggle, open a real Java project via CodeGraph. Confirm:
- "Open Java project…" / "Re-index" / project path row appears in the shell sidebar column, not inline in the content area.
- Endpoints/Files toggle and the corresponding list/tree render in the sidebar.
- Clicking an endpoint still renders the flow canvas in the main content area; clicking a file still opens it in the code editor there.
- Switching to another module (e.g. Notes via Dock) and back to CodeGraph: sidebar content reappears correctly (proves the module-id-keyed map in Task 9 picks the right entry, not a stale one from whichever module was mounted last).
- Closing the sidebar via the toggle hides the column; CodeGraph's main content (flow canvas / editor) is unaffected — they're independent trees.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/modules/codegraph/CodeGraphView.tsx
git commit -m "feat(codegraph): push CodeGraphSidebar through useSidebarContent"
```

---

## Task 13: Full verification pass

**Files:** none — verification only.

- [ ] **Step 1: Go**

Run: `cd /Users/syuro/Workspace/PERSONAL/dev-kit/dev-kit-app && go build ./... && go test ./...`
Expected: builds clean, all tests pass.

- [ ] **Step 2: Frontend**

Run: `cd frontend && bun test && bunx tsc --noEmit && bun run build`
Expected: all pass, including `check-performance-budget.mjs` — this feature adds `zustand/middleware`'s `persist` (small, and `zustand` core is already a dependency) and `framer-motion` (already a dependency, already used by `SplitPanelSlot`) to the *initial* bundle this time, since `Workbench.tsx` itself (not a lazy module chunk) now imports `sidebar-shell.tsx`. Confirm the budget still passes; if it doesn't, that's real signal to look at, not something to raise the budget past.

- [ ] **Step 3: Doc fix**

Apply the one-line correction from the spec's Decisions section to `base-doc/content/projects/dev-kit/sidebar.mdx` § Resize: replace the claim that Sidebar "dùng lại react-resizable-panels (đã có trong frontend/package.json, cùng lib SplitPanel dùng)" with a note that it mirrors `SplitPanelSlot`'s real hand-rolled pointer-resize implementation instead (the library is an unused dependency, not what `SplitPanel` actually uses).

- [ ] **Step 4: Commit the doc fix**

```bash
git -C base-doc add content/projects/dev-kit/sidebar.mdx
git -C base-doc commit -m "docs: correct sidebar.mdx's resize implementation claim against real split-panel.tsx"
```

(`base-doc` is checked separately from `dev-kit-app` — confirm with `git -C base-doc status` first if unsure whether it's the same repository.)

---

## Deliberate limitations

- **No eviction policy for sidebar content beyond what `WorkbenchViews` already enforces** (`MAX_MOUNTED_MODULES = 3`, 2-minute grace). This is an explicit reading of the spec's "hide, don't destroy" language as "the same policy that already exists for main content," not a new unbounded-retention mechanism — see the spec's Architecture §5 for the reasoning. Revisit only if that shared cap turns out wrong for sidebar specifically.
- **`CodeGraphSidebar`'s search box is not built.** The Figma mock shows one; `searchCodeGraphSymbol` exists on the bridge, unused. Out of scope per the spec.
- **`VirtualList`/`VirtualTree` are not built.** No consumer needs them at CodeGraph's current list sizes.
- **The toggle sits at the end of the current title bar's right-hand cluster**, not "right after traffic lights." Moves when the separate title-bar redesign (`title-bar.mdx`) lands — tracked there, not here.

## Self-review notes

- Every architecture piece from the spec has a task: `ui_state` (Tasks 1–6), `useSidebarContent`/register-show split (Task 9), resize + cross-clamp (Tasks 7, 9), toggle + persistence (Tasks 8, 10), vault-gating (free, confirmed in spec, no task needed), performance/mount lifecycle (confirmed as inherited from `WorkbenchViews`, no task needed), `CodeGraphView` consumer (Tasks 11–12).
- Types threaded through later tasks (`CodeGraphSidebarProps`, `SidebarModuleIdContext`, `usePanelWidthsStore`, `useSidebarOpenStore`) are each defined in the task that introduces them and used identically afterward.
- Every Go step mirrors a real, already-read file (`preferences.go`/`memory.go`/`sqlrepository.go`/`preferences_test.go`, the `app_preferences` migration, `container.go`'s exact wiring lines, `bridge/preferences.go`'s `s.call` shape) — nothing in Task 1–4 is invented shape, only renamed/generalized.
- The two doc/code corrections the spec found (`react-resizable-panels` claim, window min-width open question) are both closed out: the first via Task 13 Step 3, the second via Task 6's derived numbers.
