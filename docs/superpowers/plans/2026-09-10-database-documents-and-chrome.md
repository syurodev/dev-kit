# Database Documents & Chrome Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn the Database module from one screen with five view-modes into a multi-document workspace where tables, consoles, the ERD, activity, history and saved queries each open as a workbench tab, and the module's own header disappears.

**Architecture:** The workbench already owns a tab strip, per-module tab partitions, preview tabs and `params` plumbing — Notes proves the whole chain. Database opts in with `tabs: true`, declares one view per document type, and opens tabs with `params`. `tabKey` dedupes on `moduleId+viewId+params`, so opening the same table twice focuses the existing tab for free. The module's console state stops being a single `sql`/`queryResult` pair and becomes a record keyed by console id, so several consoles can be open at once. `DatabaseHeader` is deleted: the profile identity it restated is already in the rail's selected row, and its two buttons move to the title bar's module-action zone, which exists and today has no callers.

**Tech Stack:** React 19, Zustand, `bun test`, existing `workbenchTabs` store, existing `moduleActions` store.

**Out of scope — a separate plan:** the result grid itself (TanStack Table v9, `table-fixed` + `colgroup`, column resize/reorder via dnd-kit, consolidating the per-cell `ContextMenu`). This plan leaves `QueryConsole`'s grid untouched apart from moving its state behind a console id.

---

## File Structure

**Modified**
- `frontend/src/modules/database/manifest.ts` — `tabs: true`; one view per document type
- `frontend/src/modules/database/databaseStore.ts` — console state becomes `consoles: Record<string, ConsoleState>`; `activeTab`/`setActiveTab` deleted
- `frontend/src/modules/database/DatabaseView.tsx` — becomes the console document; stops switching views
- `frontend/src/modules/database/components/QueryConsole.tsx` — takes a `consoleId`, reads its own slice
- `frontend/src/modules/database/components/DatabaseSidebar.tsx` — table rows open a data tab; adds tooltips
- `frontend/src/modules/database/components/ExportModal.tsx`, `SaveQueryModal.tsx`, `CellInspector.tsx` — read the active console's slice

**Created**
- `frontend/src/modules/database/TableDataView.tsx` — the data document for one table
- `frontend/src/modules/database/useDatabaseModuleActions.ts` — registers title-bar actions
- `frontend/src/modules/database/databaseStore.test.ts` — console-slice tests
- `frontend/src/modules/database/documents.test.ts` — tab-opening tests

**Deleted**
- `frontend/src/modules/database/components/DatabaseHeader.tsx`

---

## Task 1: Table rows get a tooltip

Three tables in the sample schema render as `backup_his_medical_recor…` and are indistinguishable. `tableNodes` sets no `title`, so there is not even a tooltip. This is independent of everything else and ships on its own.

**Files:**
- Modify: `frontend/src/modules/database/components/DatabaseSidebar.tsx`

- [ ] **Step 1: Add the title to the table node**

In `tableNodes`, inside the returned object (it currently has `id`, `label`, `icon`, `expandable`, `expanded`, `onToggle`, `selected`, `onSelect`, `onActivate`, `children`, `emptyLabel`, `meta`, `trailing`, `wrap`), add `title` immediately after `label`:

```tsx
        label: tableName,
        // Names are truncated at the rail's width, and a schema of
        // backup_his_medical_records_* tables truncates to the same string
        // several times over. The tooltip is the only thing telling them apart
        // without expanding each one.
        title: `${schemaName}.${tableName}`,
```

- [ ] **Step 2: Typecheck**

Run: `cd frontend && npx tsc --noEmit`
Expected: no output (clean).

- [ ] **Step 3: Commit**

```bash
git add frontend/src/modules/database/components/DatabaseSidebar.tsx
git commit -m "fix(database): name the table a truncated row belongs to"
```

---

## Task 2: Console state becomes per-console

`databaseStore` holds one `sql`, one `queryResult`, one `queryError`. Several consoles cannot share those. This task splits them out with no UI change yet — the module keeps working through a single console id.

**Files:**
- Modify: `frontend/src/modules/database/databaseStore.ts`
- Test: `frontend/src/modules/database/databaseStore.test.ts` (create)

- [ ] **Step 1: Write the failing test**

Create `frontend/src/modules/database/databaseStore.test.ts`:

```ts
import { beforeEach, describe, expect, test } from "bun:test";
import { DEFAULT_CONSOLE_ID, useDatabaseStore } from "./databaseStore";

beforeEach(() => {
  useDatabaseStore.setState({ consoles: {} });
});

describe("console slices", () => {
  test("a console that was never opened reads as empty, not undefined", () => {
    const console = useDatabaseStore.getState().consoleState("never-opened");
    expect(console.sql).toBe("");
    expect(console.queryResult).toBeNull();
    expect(console.runningQuery).toBe(false);
  });

  test("two consoles hold their own SQL", () => {
    const { setSql } = useDatabaseStore.getState();
    setSql("a", "SELECT 1;");
    setSql("b", "SELECT 2;");

    const state = useDatabaseStore.getState();
    expect(state.consoleState("a").sql).toBe("SELECT 1;");
    expect(state.consoleState("b").sql).toBe("SELECT 2;");
  });

  test("closing a console drops its slice", () => {
    const { setSql, closeConsole } = useDatabaseStore.getState();
    setSql("a", "SELECT 1;");
    closeConsole("a");
    expect(useDatabaseStore.getState().consoles["a"]).toBeUndefined();
  });

  // A result belongs to the console that asked for it. Without this, a slow
  // query in one tab lands in whichever console happens to be on screen.
  test("a result lands in the console that ran it", () => {
    useDatabaseStore.setState((s) => ({
      consoles: {
        ...s.consoles,
        a: { ...s.consoleState("a"), queryResult: { columns: ["n"], rows: [["1"]], rowCount: 1, truncated: false } },
      },
    }));
    expect(useDatabaseStore.getState().consoleState("b").queryResult).toBeNull();
    expect(useDatabaseStore.getState().consoleState("a").queryResult?.rowCount).toBe(1);
  });
});
```

- [ ] **Step 2: Run it to make sure it fails**

Run: `cd frontend && bun test src/modules/database/databaseStore.test.ts`
Expected: FAIL — `DEFAULT_CONSOLE_ID` is not exported and `consoleState` is not a function.

- [ ] **Step 3: Add the console slice type and defaults**

In `frontend/src/modules/database/databaseStore.ts`, above `export type DatabaseStoreState`, add:

```ts
/**
 * One query console's own state. Several consoles are open at once, so none of
 * this can live as a single field on the store: a result carries the id of the
 * console that asked for it, and a slow query landing after you switched tabs
 * has to find its way back to the tab that ran it.
 */
export type ConsoleState = {
  sql: string;
  queryResult: QueryResult | null;
  runningQuery: boolean;
  queryStartTime: number | null;
  queryError: string;
  queryStatus: string;
  queryDurationMs: number | null;
  queryRunId: number;
  /** Percent of the console's height given to the editor. */
  editorSize: number;
};

/** The console a module opens with, and the one every legacy caller means. */
export const DEFAULT_CONSOLE_ID = "console-1";

export const EMPTY_CONSOLE: ConsoleState = Object.freeze({
  sql: "",
  queryResult: null,
  runningQuery: false,
  queryStartTime: null,
  queryError: "",
  queryStatus: "",
  queryDurationMs: null,
  queryRunId: 0,
  editorSize: 50,
}) as ConsoleState;
```

- [ ] **Step 4: Replace the flat console fields on the state type**

In `DatabaseStoreState`, delete these lines:

```ts
  // Query console state
  sql: string;
  queryResult: QueryResult | null;
  runningQuery: boolean;
  queryStartTime: number | null;
  queryError: string;
  queryStatus: string;
  queryDurationMs: number | null;
  queryRunId: number;
```

and also delete `consoleEditorSize: number;` from the folder-group block. In their place add:

```ts
  /** Every open console's state, keyed by the tab's console id. */
  consoles: Record<string, ConsoleState>;
```

- [ ] **Step 5: Replace the console actions on the state type**

In `DatabaseStoreState`, change these signatures:

```ts
  setSql: (consoleId: string, sql: string) => void;
  setConsoleEditorSize: (consoleId: string, size: number) => void;
  executeQuery: (consoleId: string) => Promise<void>;
  cancelQuery: (consoleId: string) => void;
  clearQueryResult: (consoleId: string) => void;
  clearQueryError: (consoleId: string) => void;
  /** Reads a console, defaulting to EMPTY_CONSOLE so callers never branch. */
  consoleState: (consoleId: string) => ConsoleState;
  /** Drops a console's slice when its tab closes. */
  closeConsole: (consoleId: string) => void;
```

- [ ] **Step 6: Implement the readers and writers**

In the store body, replace the initial values `sql: ""`, `queryResult: null`, `runningQuery: false`, `queryStartTime: null`, `queryError: ""`, `queryStatus: ""`, `queryDurationMs: null`, `queryRunId: 0` and `consoleEditorSize: 50` with:

```ts
  consoles: {},
```

Then add these actions next to the other query actions:

```ts
  consoleState: (consoleId) => get().consoles[consoleId] ?? EMPTY_CONSOLE,

  closeConsole: (consoleId) =>
    set((state) => {
      if (!(consoleId in state.consoles)) {
        return state;
      }
      const { [consoleId]: _closed, ...rest } = state.consoles;
      return { consoles: rest };
    }),

  setSql: (consoleId, sql) => patchConsole(set, consoleId, { sql }),

  setConsoleEditorSize: (consoleId, size) =>
    patchConsole(set, consoleId, { editorSize: size }),

  clearQueryResult: (consoleId) =>
    patchConsole(set, consoleId, { queryResult: null, queryStatus: "", queryDurationMs: null }),

  clearQueryError: (consoleId) => patchConsole(set, consoleId, { queryError: "" }),
```

And above the store's `create(` call, add the helper:

```ts
/**
 * Merges a patch into one console, creating it from EMPTY_CONSOLE if this is
 * the first thing that touched it. Every console writer goes through here so a
 * missing slice is never a branch at the call site.
 */
function patchConsole(
  set: (fn: (state: DatabaseStoreState) => Partial<DatabaseStoreState>) => void,
  consoleId: string,
  patch: Partial<ConsoleState>,
): void {
  set((state) => ({
    consoles: {
      ...state.consoles,
      [consoleId]: { ...(state.consoles[consoleId] ?? EMPTY_CONSOLE), ...patch },
    },
  }));
}
```

- [ ] **Step 7: Run the test**

Run: `cd frontend && bun test src/modules/database/databaseStore.test.ts`
Expected: PASS, 4 tests.

- [ ] **Step 8: Commit**

```bash
git add frontend/src/modules/database/databaseStore.ts frontend/src/modules/database/databaseStore.test.ts
git commit -m "refactor(database): console state keyed by console id"
```

---

## Task 3: Route the remaining writers through a console id

`executeQuery` still writes the flat fields deleted in Task 2, so the store does not typecheck yet. Two other writers do too, and neither is a query — Task 2's implementer found them:

- `setSelectedProfileId` cleared the one result when you switched connection. With several consoles, every console's result is now stale, not just one.
- `refreshSchema`'s catch writes `queryError`. That is a schema failure landing in a console's error slot — the same bug shape as the `tableColumnsError` fix earlier in this codebase, and worse now, because with N consoles there is no "the" console to put it in.

**Files:**
- Modify: `frontend/src/modules/database/databaseStore.ts` — `setSelectedProfileId`, `refreshSchema`, `executeQuery`, `cancelQuery`
- Test: `frontend/src/modules/database/databaseStore.test.ts`

- [ ] **Step 1: Write the failing test**

Append to `frontend/src/modules/database/databaseStore.test.ts`:

```ts
// smoothness 5, "stale response": queryRunId is what stops a slow first run
// overwriting a faster second one. Per console now, so two consoles running at
// once cannot invalidate each other.
describe("run tokens", () => {
  test("each console counts its own runs", () => {
    const { setSql } = useDatabaseStore.getState();
    setSql("a", "SELECT 1;");
    setSql("b", "SELECT 2;");

    useDatabaseStore.setState((s) => ({
      consoles: {
        ...s.consoles,
        a: { ...s.consoleState("a"), queryRunId: 7 },
      },
    }));

    expect(useDatabaseStore.getState().consoleState("a").queryRunId).toBe(7);
    expect(useDatabaseStore.getState().consoleState("b").queryRunId).toBe(0);
  });
});
```

- [ ] **Step 2: Run it to make sure it fails**

Run: `cd frontend && bun test src/modules/database/databaseStore.test.ts`
Expected: FAIL — the module does not compile, because `executeQuery` still assigns `sql`, `queryRunId` and friends at the top level.

- [ ] **Step 3: Clear every console's result when the connection changes**

In `setSelectedProfileId`, remove `queryResult: null`, `queryError: ""`, `queryStatus: ""` and `queryDurationMs: null` from the `set({...})`, and add after it:

```ts
    // Every console's result came from the connection you just left, so none of
    // them describe anything any more. The SQL stays: the same query against a
    // different database is usually why you switched.
    set((state) => ({
      consoles: Object.fromEntries(
        Object.entries(state.consoles).map(([id, console]) => [
          id,
          { ...console, queryResult: null, queryError: "", queryStatus: "", queryDurationMs: null },
        ]),
      ),
    }));
```

- [ ] **Step 4: Give a schema failure its own field**

Add to `DatabaseStoreState`, next to `indexingSchema`:

```ts
  /**
   * Why the last schema index failed. Its own field rather than a console's
   * queryError: re-indexing belongs to the connection, and with several
   * consoles open there is no console it could honestly belong to.
   */
  schemaError: string;
```

Initial value `schemaError: ""`. In `refreshSchema`, replace the two `set` calls that touch it:

```ts
  refreshSchema: async (profileId: string) => {
    set({ indexingSchema: true, schemaError: "" });
    try {
      const snap = await indexSchema(profileId);
      set({ schemaSnapshot: snap, indexingSchema: false });
      const schemas = Object.keys(snap.namespace || {});
      if (schemas.length > 0 && !get().selectedSchema) {
        set({ selectedSchema: schemas[0] });
      }
    } catch (e) {
      set({ indexingSchema: false, schemaError: errorMessage(e) });
    }
  },
```

- [ ] **Step 5: Surface it**

`schemaError` with no reader is the swallowed failure again, just in a new field. Re-indexing is triggered from the title bar (Task 9), so its failure is transient and belongs in a toast rather than a permanent surface. In `frontend/src/modules/database/DatabaseView.tsx`, add:

```tsx
  const schemaError = useDatabaseStore((s) => s.schemaError);
  useEffect(() => {
    if (schemaError) {
      toast.error("Could not index the schema", { description: schemaError });
    }
  }, [schemaError]);
```

with `import { toast } from "sonner";`. The Toaster is mounted app-wide in `App.tsx`, and `modules/notes` uses the same call shape.

Note for the implementer: `DatabaseView.tsx` is rewritten wholesale in Task 7. Add this now anyway — leaving `schemaError` unread between here and there is exactly the silent failure this task exists to end, and Task 7's replacement text already carries it.

- [ ] **Step 6: Rewrite executeQuery**

Replace the whole `executeQuery` action with:

```ts
  executeQuery: async (consoleId) => {
    const { selectedProfileId } = get();
    const console = get().consoleState(consoleId);
    if (!selectedProfileId || !console.sql.trim()) {
      return;
    }

    const runId = console.queryRunId + 1;
    patchConsole(set, consoleId, {
      queryRunId: runId,
      runningQuery: true,
      queryError: "",
      queryStatus: "",
      queryStartTime: Date.now(),
    });

    try {
      const call = runQuery(selectedProfileId, console.sql);
      pendingQueries.set(consoleId, call);
      const result = await call;
      // A newer run in THIS console has already started; its answer wins.
      if (get().consoleState(consoleId).queryRunId !== runId) {
        return;
      }
      patchConsole(set, consoleId, {
        queryResult: result,
        runningQuery: false,
        queryDurationMs: Date.now() - (get().consoleState(consoleId).queryStartTime ?? Date.now()),
        queryStatus: `${result.rowCount} row${result.rowCount === 1 ? "" : "s"}`,
      });
    } catch (e) {
      if (isCancelError(e)) {
        // A cancel is something the user asked for; it leaves the previous
        // result alone.
        patchConsole(set, consoleId, { runningQuery: false });
        return;
      }
      if (get().consoleState(consoleId).queryRunId !== runId) {
        return;
      }
      patchConsole(set, consoleId, { queryError: errorMessage(e), runningQuery: false });
    } finally {
      pendingQueries.delete(consoleId);
    }
  },

  cancelQuery: (consoleId) => {
    pendingQueries.get(consoleId)?.cancel();
  },
```

Above the store's `create(` call, next to `patchConsole`, add:

```ts
/**
 * The in-flight call per console, so Cancel reaches the right one. Outside the
 * store because a CancellableCall is not state — nothing renders from it, and
 * putting it in the store would make every console re-render when any query
 * starts.
 */
const pendingQueries = new Map<string, ReturnType<typeof runQuery>>();
```

- [ ] **Step 7: Run the tests**

Run: `cd frontend && bun test src/modules/database/databaseStore.test.ts`
Expected: PASS, 5 tests — the four from Task 2 plus the run-token one above.

- [ ] **Step 8: Confirm the store itself now typechecks**

Run: `cd frontend && npx tsc --noEmit 2>&1 | grep databaseStore`
Expected: no output. Errors elsewhere (`QueryConsole.tsx`, `ExportModal.tsx`, `DatabaseSidebar.tsx`) are Tasks 4-5 and 8.

- [ ] **Step 9: Commit**

```bash
git add frontend/src/modules/database
git commit -m "refactor(database): per-console runs, and a schema failure of its own"
```

---

## Task 4: QueryConsole takes a console id

**Files:**
- Modify: `frontend/src/modules/database/components/QueryConsole.tsx`

- [ ] **Step 1: Give the component its prop**

Change the signature and the store reads at the top of `QueryConsole`:

```tsx
export function QueryConsole({ consoleId }: { consoleId: string }) {
  // One subscription per console: the slice is a single object, so a run in
  // another console changes nothing this component reads.
  const console = useDatabaseStore((s) => s.consoles[consoleId] ?? EMPTY_CONSOLE);
  const setSql = useDatabaseStore((s) => s.setSql);
  const executeQuery = useDatabaseStore((s) => s.executeQuery);
  const cancelQuery = useDatabaseStore((s) => s.cancelQuery);
  const setConsoleEditorSize = useDatabaseStore((s) => s.setConsoleEditorSize);
```

Import `EMPTY_CONSOLE` alongside `useDatabaseStore`.

- [ ] **Step 2: Rewrite every use of the old flat fields**

Inside the component, replace each bare reference with the slice: `sql` → `console.sql`, `queryResult` → `console.queryResult`, `runningQuery` → `console.runningQuery`, `queryError` → `console.queryError`, `queryStatus` → `console.queryStatus`, `queryDurationMs` → `console.queryDurationMs`, `consoleEditorSize` → `console.editorSize`.

Every call gains the id: `setSql(consoleId, value)`, `executeQuery(consoleId)`, `cancelQuery(consoleId)`, `setConsoleEditorSize(consoleId, size)`.

- [ ] **Step 3: Typecheck**

Run: `cd frontend && npx tsc --noEmit`
Expected: errors only in `DatabaseView.tsx`, `ExportModal.tsx`, `SaveQueryModal.tsx`, `CellInspector.tsx` — the remaining callers, fixed in Task 5.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/modules/database/components/QueryConsole.tsx
git commit -m "refactor(database): QueryConsole reads one console slice"
```

---

## Task 5: The modals follow the active console

`ExportModal` and `SaveQueryModal` act on "the" result and "the" SQL. With several consoles, they act on the one whose tab is active.

**Files:**
- Modify: `frontend/src/modules/database/databaseStore.ts`
- Modify: `frontend/src/modules/database/components/ExportModal.tsx`
- Modify: `frontend/src/modules/database/components/SaveQueryModal.tsx`

- [ ] **Step 1: Track which console the modals mean**

Add to `DatabaseStoreState`:

```ts
  /**
   * The console a modal acts on. Set when a console opens a modal, because
   * "export this result" is about the console that asked, not about whichever
   * tab wins a race to render.
   */
  modalConsoleId: string;
```

Initial value `modalConsoleId: ""`, and change the two openers to record it:

```ts
  setShowExportModal: (show) => set({ showExportModal: show }),
  setShowSaveQueryModal: (show) => set({ showSaveQueryModal: show }),
  openExportModal: (consoleId) => set({ showExportModal: true, modalConsoleId: consoleId }),
  openSaveQueryModal: (consoleId) => set({ showSaveQueryModal: true, modalConsoleId: consoleId }),
```

Declare `openExportModal: (consoleId: string) => void;` and `openSaveQueryModal: (consoleId: string) => void;` on the state type.

- [ ] **Step 2: Read the recorded console in both modals**

In `ExportModal.tsx` and `SaveQueryModal.tsx`, replace the `queryResult` / `sql` reads with:

```tsx
  const modalConsoleId = useDatabaseStore((s) => s.modalConsoleId);
  const console = useDatabaseStore((s) => s.consoles[modalConsoleId] ?? EMPTY_CONSOLE);
```

and use `console.queryResult` / `console.sql`.

- [ ] **Step 3: Point QueryConsole's buttons at the new openers**

In `QueryConsole.tsx`, replace `setShowExportModal(true)` with `openExportModal(consoleId)` and `setShowSaveQueryModal(true)` with `openSaveQueryModal(consoleId)`.

- [ ] **Step 4: Typecheck**

Run: `cd frontend && npx tsc --noEmit`
Expected: errors only in `DatabaseView.tsx`.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/modules/database/databaseStore.ts frontend/src/modules/database/components/ExportModal.tsx frontend/src/modules/database/components/SaveQueryModal.tsx frontend/src/modules/database/components/QueryConsole.tsx
git commit -m "refactor(database): modals act on the console that opened them"
```

---

## Task 6: Declare the document views

**Files:**
- Modify: `frontend/src/modules/database/manifest.ts`
- Create: `frontend/src/modules/database/TableDataView.tsx`

- [ ] **Step 1: Create the table document**

Create `frontend/src/modules/database/TableDataView.tsx`:

```tsx
import { useEffect } from "react";
import { useDatabaseStore } from "./databaseStore";
import { QueryConsole } from "./components/QueryConsole";

/**
 * One table's data, as its own document.
 *
 * Backed by a console rather than a second grid: the backend's only read path
 * is RunQuery, the result shape is the same, and a table tab is a console whose
 * SQL was written for you. Giving it its own console id is what keeps that SQL
 * — and its result — from colliding with a console you typed in yourself.
 */
export function TableDataView({ params }: { params?: Record<string, unknown> }) {
  const schema = typeof params?.schema === "string" ? params.schema : "";
  const table = typeof params?.table === "string" ? params.table : "";
  const consoleId = `table:${schema}.${table}`;

  const setSql = useDatabaseStore((s) => s.setSql);
  const executeQuery = useDatabaseStore((s) => s.executeQuery);
  const hasSql = useDatabaseStore((s) => Boolean(s.consoles[consoleId]?.sql));

  // Seeded once. Re-seeding on every mount would throw away a WHERE the user
  // added, and this view remounts whenever its tab leaves the mount window.
  useEffect(() => {
    if (hasSql || !table) {
      return;
    }
    setSql(consoleId, `SELECT * FROM ${table} LIMIT 200;`);
    void executeQuery(consoleId);
  }, [consoleId, hasSql, table, setSql, executeQuery]);

  return <QueryConsole consoleId={consoleId} />;
}

export default TableDataView;
```

- [ ] **Step 2: Declare every document type in the manifest**

Replace the `views` array in `frontend/src/modules/database/manifest.ts`:

```tsx
  // A tab per document: a console, a table's data, the ERD, and the three
  // read-only views. views[0] is the module's home — what a Dock click opens.
  tabs: true,
  views: [
    {
      id: "database.console",
      title: "Console",
      ...lazyModuleView(() =>
        import("./DatabaseView").then(({ DatabaseView }) => ({ default: DatabaseView })),
      ),
    },
    {
      id: "database.table",
      title: "Table",
      ...lazyModuleView(() =>
        import("./TableDataView").then(({ TableDataView }) => ({ default: TableDataView })),
      ),
    },
    {
      id: "database.erd",
      title: "ERD diagram",
      ...lazyModuleView(() =>
        import("./components/ERDView").then(({ ERDView }) => ({ default: ERDView })),
      ),
    },
    {
      id: "database.activity",
      title: "Live activity",
      ...lazyModuleView(() =>
        import("./components/LiveActivityView").then(({ LiveActivityView }) => ({ default: LiveActivityView })),
      ),
    },
    {
      id: "database.history",
      title: "Query history",
      ...lazyModuleView(() =>
        import("./components/QueryHistoryView").then(({ QueryHistoryView }) => ({ default: QueryHistoryView })),
      ),
    },
    {
      id: "database.saved",
      title: "Saved queries",
      ...lazyModuleView(() =>
        import("./components/SavedQueriesView").then(({ SavedQueriesView }) => ({ default: SavedQueriesView })),
      ),
    },
  ],
```

- [ ] **Step 3: Update the Go manifest to match**

The frontend and backend manifests are compared at runtime by `compareModuleParity`, and a mismatch hides the module from the Dock. In `internal/app/app.go`, change the `database` entry's `Views`:

```go
			Views:        []string{"database.console", "database.table", "database.erd", "database.activity", "database.history", "database.saved"},
```

- [ ] **Step 4: Verify parity**

Run: `cd frontend && npx tsc --noEmit && cd .. && go build ./internal/...`
Expected: both clean.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/modules/database/manifest.ts frontend/src/modules/database/TableDataView.tsx internal/app/app.go
git commit -m "feat(database): declare a view per document type"
```

---

## Task 7: DatabaseView becomes the console document

`DatabaseView` currently switches between five views and renders the header. It becomes one console.

**Files:**
- Modify: `frontend/src/modules/database/DatabaseView.tsx`
- Delete: `frontend/src/modules/database/components/DatabaseHeader.tsx`

- [ ] **Step 1: Rewrite DatabaseView**

Replace the whole file with:

```tsx
import { useEffect, useMemo } from "react";
import { toast } from "sonner";
import { useShallow } from "zustand/react/shallow";
import { useSidebarContent } from "@/components/ui/sidebar-shell";
import { DEFAULT_CONSOLE_ID, useDatabaseStore } from "./databaseStore";
import { DatabaseSidebar } from "./components/DatabaseSidebar";
import { DatabaseBanners } from "./components/DatabaseBanners";
import { QueryConsole } from "./components/QueryConsole";
import { NewConnectionDialog } from "./components/NewConnectionDialog";
import { ConnectionOptionsDialog } from "./components/ConnectionOptionsDialog";
import { ExportModal } from "./components/ExportModal";
import { SaveQueryModal } from "./components/SaveQueryModal";
import { CellInspector } from "./components/CellInspector";
import { TLSRefusedState, VaultLockedState } from "./components/DatabaseStates";
import { isInsecureTLSError, isVaultLockedError } from "./errorStates";
import { useDatabaseModuleActions } from "./useDatabaseModuleActions";

/**
 * One query console, as a document.
 *
 * The module's header is gone: the profile name, engine and env it restated are
 * all on the rail's selected row already, and its two buttons act on the
 * connection rather than on this tab, so they live in the title bar (see
 * useDatabaseModuleActions). What is left is the console itself.
 */
export function DatabaseView({ params }: { params?: Record<string, unknown> }) {
  const consoleId = typeof params?.consoleId === "string" ? params.consoleId : DEFAULT_CONSOLE_ID;

  const { fetchProfiles, profileError, profiles, selectedProfileId, setShowConnectionOptionsDialog } =
    useDatabaseStore(
      useShallow((s) => ({
        fetchProfiles: s.fetchProfiles,
        profileError: s.profileError,
        profiles: s.profiles,
        selectedProfileId: s.selectedProfileId,
        setShowConnectionOptionsDialog: s.setShowConnectionOptionsDialog,
      })),
    );
  const queryError = useDatabaseStore((s) => s.consoles[consoleId]?.queryError ?? "");

  useEffect(() => {
    void fetchProfiles();
  }, [fetchProfiles]);

  // Carried over from Task 3: re-indexing is a title-bar action on the
  // connection, so its failure is transient and belongs in a toast rather than
  // in a console's error slot.
  const schemaError = useDatabaseStore((s) => s.schemaError);
  useEffect(() => {
    if (schemaError) {
      toast.error("Could not index the schema", { description: schemaError });
    }
  }, [schemaError]);

  const sidebar = useMemo(() => <DatabaseSidebar />, []);
  useSidebarContent(sidebar);
  useDatabaseModuleActions();

  const blocking = profileError || queryError;
  const profile = profiles.find((p) => p.id === selectedProfileId);

  if (isVaultLockedError(profileError)) {
    return <VaultLockedState />;
  }
  if (isInsecureTLSError(blocking)) {
    return (
      <TLSRefusedState
        target={profile ? `${profile.host}:${profile.port}` : "this host"}
        onEditConnection={() => setShowConnectionOptionsDialog(true)}
      />
    );
  }

  return (
    <div className="flex h-full w-full flex-col overflow-hidden text-text-primary">
      <DatabaseBanners />
      <QueryConsole consoleId={consoleId} />
      <NewConnectionDialog />
      <ConnectionOptionsDialog />
      <ExportModal />
      <SaveQueryModal />
      <CellInspector />
    </div>
  );
}

export default DatabaseView;
```

- [ ] **Step 2: Delete the header**

```bash
git rm frontend/src/modules/database/components/DatabaseHeader.tsx
```

- [ ] **Step 3: Remove the module's own tab state**

In `databaseStore.ts`, delete `activeTab: DatabaseTab;` and `setActiveTab: (tab: DatabaseTab) => void;` from the state type, `activeTab: "console",` from the initial state, and the `setActiveTab` action. Keep the `DatabaseTab` type export only if something still imports it; if `npx tsc --noEmit` says nothing does, delete it too.

Three call sites set it and must go: the "Focus in ERD" context-menu item in `DatabaseSidebar.tsx` becomes an `openTab` in Task 8, and the two in `QueryHistoryView.tsx` / `SavedQueriesView.tsx` that switch to the console become `openTab` calls in Task 8 as well. Leave them broken for now.

- [ ] **Step 4: Typecheck**

Run: `cd frontend && npx tsc --noEmit`
Expected: errors only at the three `setActiveTab` call sites named above.

- [ ] **Step 5: Commit**

```bash
git add -A frontend/src/modules/database internal/app/app.go
git commit -m "feat(database): the console is a document, the header is gone"
```

---

## Task 8: Open documents from the rail and the views

**Files:**
- Modify: `frontend/src/modules/database/components/DatabaseSidebar.tsx`
- Modify: `frontend/src/modules/database/components/QueryHistoryView.tsx`
- Modify: `frontend/src/modules/database/components/SavedQueriesView.tsx`
- Test: `frontend/src/modules/database/documents.test.ts` (create)

- [ ] **Step 1: Write the failing test**

Create `frontend/src/modules/database/documents.test.ts`:

```ts
import { beforeEach, describe, expect, test } from "bun:test";
import { moduleTabs, useWorkbenchTabsStore } from "@/stores/workbenchTabs";
import { openTableTab } from "./documents";

beforeEach(() => {
  useWorkbenchTabsStore.setState({ activeModuleId: "", modules: {} });
});

describe("table documents", () => {
  test("opening a table opens one tab titled after it", () => {
    openTableTab("chiro", "ba_user");
    const tabs = moduleTabs(useWorkbenchTabsStore.getState().modules, "database");
    expect(tabs.openTabs).toHaveLength(1);
    expect(tabs.openTabs[0].title).toBe("ba_user");
  });

  // tabKey dedupes on moduleId+viewId+params, so this is the store's promise
  // rather than ours — the test is here because losing it would silently
  // duplicate a tab on every click.
  test("opening the same table twice focuses the first tab", () => {
    openTableTab("chiro", "ba_user");
    openTableTab("chiro", "ba_user");
    const tabs = moduleTabs(useWorkbenchTabsStore.getState().modules, "database");
    expect(tabs.openTabs).toHaveLength(1);
  });

  test("the same table name in two schemas is two tabs", () => {
    openTableTab("chiro", "users");
    openTableTab("billing", "users");
    const tabs = moduleTabs(useWorkbenchTabsStore.getState().modules, "database");
    expect(tabs.openTabs).toHaveLength(2);
  });
});
```

- [ ] **Step 2: Run it to make sure it fails**

Run: `cd frontend && bun test src/modules/database/documents.test.ts`
Expected: FAIL — cannot resolve `./documents`.

- [ ] **Step 3: Write the openers**

Create `frontend/src/modules/database/documents.ts`:

```ts
import { Table2, Network, Activity, History, Bookmark, Terminal } from "lucide-react";
import { getModule } from "@/modules/registry";
import { useWorkbenchTabsStore } from "@/stores/workbenchTabs";

/**
 * Opening a Database document.
 *
 * Every one of these is a tab, which is what removed the module's own five-way
 * view switch: a mode you can only be in one of is a worse fit than a document
 * you can have several of. `preview` is the shared slot a single click uses —
 * a second click on something else replaces it, so browsing the rail does not
 * leave a trail of tabs behind.
 */
function open(viewId: string, title: string, icon: typeof Table2, params?: Record<string, unknown>, preview = true) {
  // Start the chunk before React needs it: the tab appears in the same frame
  // either way, but the view lands sooner.
  void getModule("database")?.views.find((v) => v.id === viewId)?.preload?.().catch(() => undefined);
  useWorkbenchTabsStore.getState().openTab({ moduleId: "database", viewId, title, icon, params, preview });
}

export function openTableTab(schema: string, table: string, preview = true): void {
  open("database.table", table, Table2, { schema, table }, preview);
}

export function openConsoleTab(consoleId: string, title = "Console"): void {
  open("database.console", title, Terminal, { consoleId }, false);
}

export function openErdTab(): void {
  open("database.erd", "ERD diagram", Network, undefined, false);
}

export function openActivityTab(): void {
  open("database.activity", "Live activity", Activity, undefined, false);
}

export function openHistoryTab(): void {
  open("database.history", "Query history", History, undefined, false);
}

export function openSavedTab(): void {
  open("database.saved", "Saved queries", Bookmark, undefined, false);
}
```

- [ ] **Step 4: Run the test**

Run: `cd frontend && bun test src/modules/database/documents.test.ts`
Expected: PASS, 3 tests.

- [ ] **Step 5: Wire the rail**

In `DatabaseSidebar.tsx`'s `tableNodes`, replace `onActivate: () => handleInsertTable(tableName)` with:

```tsx
        // Single click selects and expands; double click opens the data. That
        // is the DataGrip gesture, and it is why opening a table no longer
        // overwrites whatever SQL a console had in it.
        onActivate: () => openTableTab(schemaName, tableName, false),
```

and change the context menu's first item from "Select 50 rows" to:

```tsx
              <ContextMenuItem onSelect={() => openTableTab(schemaName, tableName, false)}>
                Open data
                <ContextMenuShortcut>Double-click</ContextMenuShortcut>
              </ContextMenuItem>
```

Replace the "Focus in ERD" item's body with:

```tsx
                onSelect={() => {
                  setSelectedSchema(schemaName);
                  setSelectedTable(tableName);
                  openErdTab();
                }}
```

Import the openers: `import { openErdTab, openTableTab } from "../../documents";`

- [ ] **Step 6: Give the three read-only views an entry point**

Deleting the header removed the only way to reach Live activity, Query history
and Saved queries. They belong to the connection, so they hang off its context
menu in the rail. In `DatabaseSidebar.tsx`'s `profileNodes`, inside the existing
`<ContextMenuContent>` for a connection, add above the destructive delete item:

```tsx
            <ContextMenuSeparator />
            <ContextMenuItem onSelect={() => openErdTab()}>
              <Network />
              ERD diagram
            </ContextMenuItem>
            <ContextMenuItem onSelect={() => openActivityTab()}>
              <Activity />
              Live activity
            </ContextMenuItem>
            <ContextMenuItem onSelect={() => openHistoryTab()}>
              <History />
              Query history
            </ContextMenuItem>
            <ContextMenuItem onSelect={() => openSavedTab()}>
              <Bookmark />
              Saved queries
            </ContextMenuItem>
```

Extend the import to `import { openActivityTab, openErdTab, openHistoryTab, openSavedTab, openTableTab } from "../../documents";` and add `Activity`, `Bookmark`, `History`, `Network` to the `lucide-react` import.

- [ ] **Step 7: Wire the two views that jumped to the console**

In `QueryHistoryView.tsx` and `SavedQueriesView.tsx`, replace each `setActiveTab("console")` with an opener that also carries the SQL. Both already have the entry's `sql` in scope:

```tsx
  const consoleId = `console-${entry.id}`;
  setSql(consoleId, entry.sql);
  openConsoleTab(consoleId, entry.name ?? "Console");
```

For `QueryHistoryView` the entry has no `name`, so pass nothing and let it default to "Console".

- [ ] **Step 8: Typecheck and test**

Run: `cd frontend && npx tsc --noEmit && bun test`
Expected: tsc clean, all tests pass.

- [ ] **Step 9: Commit**

```bash
git add frontend/src/modules/database
git commit -m "feat(database): open tables, the ERD and saved queries as documents"
```

---

## Task 9: Connection actions move to the title bar

**Files:**
- Create: `frontend/src/modules/database/useDatabaseModuleActions.ts`

- [ ] **Step 1: Write the hook**

Create `frontend/src/modules/database/useDatabaseModuleActions.ts`:

```ts
import { RefreshCw, Settings2 } from "lucide-react";
import { useModuleAction } from "@/stores/moduleActions";
import { useDatabaseStore } from "./databaseStore";

/**
 * The Database module's title-bar actions.
 *
 * The rule these follow, and the reason the old header's buttons are here
 * rather than in a tab: the title bar acts on the CONNECTION, the content
 * toolbar acts on the document in the active tab. Options and re-index are
 * true of the connection no matter which tab is open; Run and Explain are not.
 *
 * "Test connection" is deliberately not here. The rail listing a schema is
 * already proof of a live connection, and a button whose whole output is a
 * chip that disappears after four seconds does not earn a permanent seat.
 */
export function useDatabaseModuleActions(): void {
  const selectedProfileId = useDatabaseStore((s) => s.selectedProfileId);
  const indexingSchema = useDatabaseStore((s) => s.indexingSchema);
  const refreshSchema = useDatabaseStore((s) => s.refreshSchema);
  const setShowConnectionOptionsDialog = useDatabaseStore((s) => s.setShowConnectionOptionsDialog);

  useModuleAction(
    selectedProfileId
      ? {
          id: "database.reindex",
          icon: RefreshCw,
          label: indexingSchema ? "Re-indexing schema…" : "Re-index schema",
          isActive: indexingSchema,
          onClick: () => void refreshSchema(selectedProfileId),
        }
      : null,
  );

  useModuleAction(
    selectedProfileId
      ? {
          id: "database.options",
          icon: Settings2,
          label: "Connection options",
          onClick: () => setShowConnectionOptionsDialog(true),
        }
      : null,
  );
}
```

- [ ] **Step 2: Typecheck**

Run: `cd frontend && npx tsc --noEmit`
Expected: clean. `DatabaseView` already calls the hook (Task 7).

- [ ] **Step 3: Verify in the running app**

The Wails dev server is already running. In the Browser pane, open `http://127.0.0.1:9245/` — the module needs the bridge, so confirm instead from the desktop window: select a connection and check two icon buttons appear in the title bar left of the sidebar toggle, with tooltips "Re-index schema" and "Connection options".

- [ ] **Step 4: Commit**

```bash
git add frontend/src/modules/database/useDatabaseModuleActions.ts
git commit -m "feat(database): connection actions live in the title bar"
```

---

## Task 10: Views fetch only while their tab is on screen

`activeTab` was not only a view switch. Three views used it to gate a TanStack
Query: `enabled: Boolean(selectedProfileId) && activeTab === "activity"` and the
same in `QueryHistoryView` and `SavedQueriesView`. Deleting it in Task 7 leaves
those queries permanently enabled — Live activity polls a production database
forever from a tab nobody is looking at. Tabs do not replace this gate on their
own: a module's inactive tabs stay mounted so their state survives switching.

**Files:**
- Create: `frontend/src/modules/database/useIsActiveDocument.ts`
- Modify: `frontend/src/modules/database/components/LiveActivityView.tsx:66`
- Modify: `frontend/src/modules/database/components/QueryHistoryView.tsx:48`
- Modify: `frontend/src/modules/database/components/SavedQueriesView.tsx:56`
- Test: `frontend/src/modules/database/useIsActiveDocument.test.ts` (create)

- [ ] **Step 1: Write the failing test**

Create `frontend/src/modules/database/useIsActiveDocument.test.ts`:

```ts
import { beforeEach, expect, test } from "bun:test";
import { useWorkbenchTabsStore } from "@/stores/workbenchTabs";
import { isActiveDocument } from "./useIsActiveDocument";
import { openActivityTab, openHistoryTab } from "./documents";

beforeEach(() => {
  useWorkbenchTabsStore.setState({ activeModuleId: "", modules: {} });
});

test("only the active tab's view reports active", () => {
  openActivityTab();
  openHistoryTab(); // opening second makes it active

  const state = useWorkbenchTabsStore.getState();
  expect(isActiveDocument(state, "database.history")).toBe(true);
  expect(isActiveDocument(state, "database.activity")).toBe(false);
});

// The gate has to be false with nothing open, or a view mounted for one frame
// during teardown fires a request on its way out.
test("nothing open means nothing active", () => {
  const state = useWorkbenchTabsStore.getState();
  expect(isActiveDocument(state, "database.activity")).toBe(false);
});
```

- [ ] **Step 2: Run it to make sure it fails**

Run: `cd frontend && bun test src/modules/database/useIsActiveDocument.test.ts`
Expected: FAIL — cannot resolve `./useIsActiveDocument`.

- [ ] **Step 3: Export the tabs state type**

`WorkbenchTabsState` is declared at `frontend/src/stores/workbenchTabs.ts:81` but
not exported, and the hook below takes it as a parameter so the pure half can be
tested without a React render. Add the keyword:

```ts
export type WorkbenchTabsState = {
```

- [ ] **Step 4: Write the hook**

Create `frontend/src/modules/database/useIsActiveDocument.ts`:

```ts
import { moduleTabs, useWorkbenchTabsStore, type WorkbenchTabsState } from "@/stores/workbenchTabs";

/**
 * Whether this module's active tab is the given view.
 *
 * Live activity polls, and history and saved queries refetch. Being mounted is
 * not permission to do either: a module's inactive tabs stay mounted so their
 * state survives a switch, so without this a background Live activity tab keeps
 * querying a production server indefinitely. This is what `activeTab` used to
 * gate before the module's views became documents.
 */
export function isActiveDocument(state: WorkbenchTabsState, viewId: string): boolean {
  const tabs = moduleTabs(state.modules, "database");
  const active = tabs.openTabs.find((tab) => tab.id === tabs.activeTabId);
  return active?.viewId === viewId;
}

export function useIsActiveDocument(viewId: string): boolean {
  return useWorkbenchTabsStore((state) => isActiveDocument(state, viewId));
}
```

- [ ] **Step 5: Run the test**

Run: `cd frontend && bun test src/modules/database/useIsActiveDocument.test.ts`
Expected: PASS, 2 tests.

- [ ] **Step 6: Replace the three gates**

In `LiveActivityView.tsx`, drop `activeTab` from the store selector and add:

```tsx
  const isActive = useIsActiveDocument("database.activity");
```

then change the query option to `enabled: Boolean(selectedProfileId) && isActive,`.

Do the same in `QueryHistoryView.tsx` with `"database.history"` and in `SavedQueriesView.tsx` with `"database.saved"`, dropping `activeTab` from each selector.

- [ ] **Step 7: Run the checks**

Run: `cd frontend && npx tsc --noEmit && bun test`
Expected: tsc clean — this is the point where the last `activeTab` reference is gone — and all tests pass.

- [ ] **Step 8: Commit**

```bash
git add frontend/src/stores/workbenchTabs.ts frontend/src/modules/database
git commit -m "fix(database): a background tab must not keep polling"
```

---

## Task 11: Hand the gutter to the shell

With the header gone, no Database surface draws a full-bleed divider, so the shell can own the padding.

**Files:**
- Modify: `frontend/src/modules/database/manifest.ts`
- Modify: `frontend/src/modules/database/components/QueryConsole.tsx:215`
- Modify: `frontend/src/modules/database/components/LiveActivityView.tsx:141`
- Modify: `frontend/src/modules/database/components/QueryHistoryView.tsx`
- Modify: `frontend/src/modules/database/components/SavedQueriesView.tsx`

- [ ] **Step 1: Ask the shell for padding**

In `manifest.ts`, add next to `tabs: true`:

```tsx
  // The shell's p-4. Every Database view drew its own gutter and they had
  // drifted — 24px in the console, 12px in the three read-only views — so
  // switching tabs moved the content. Nothing here draws a full-bleed divider
  // any more, which is the case the bleed default exists for.
  padded: true,
```

- [ ] **Step 2: Remove the module's own gutters**

`QueryConsole.tsx:215` — drop the padding, keep the layout:

```tsx
    <div className="flex h-full flex-1 flex-col overflow-hidden" onKeyDown={handleKeyDown}>
```

`LiveActivityView.tsx:141`, and the identical line in `QueryHistoryView.tsx` and `SavedQueriesView.tsx` — drop `px-3` from the toolbar row:

```tsx
      <div className="min-h-12 py-2 border-b border-border bg-surface-raised/30 flex flex-wrap items-center justify-between gap-3 shrink-0">
```

- [ ] **Step 3: Verify the grid still bounds its own height**

`padded: true` also gives the shell an `overflow-auto` container, and the result grid virtualises against a bounded height. In the desktop window, run a query that returns the 200-row cap and confirm the grid scrolls inside its own box rather than growing the page. If the page grows, add `min-h-0` to the `<section>` chain in `Workbench.tsx:219-225` rather than reverting `padded`.

- [ ] **Step 4: Run the checks**

Run: `cd frontend && npx tsc --noEmit && bun test && bun run check:performance`
Expected: tsc clean, tests pass, budget under 1 MiB JS / 280 KiB CSS.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/modules/database
git commit -m "feat(database): one gutter, drawn by the shell"
```

---

## Task 12: Close a console's slice when its tab closes

Console slices accumulate: `closeConsole` exists (Task 2) and nothing calls it.

**Files:**
- Modify: `frontend/src/modules/database/DatabaseView.tsx`
- Test: `frontend/src/modules/database/databaseStore.test.ts`

- [ ] **Step 1: Write the failing test**

Append to `databaseStore.test.ts`:

```ts
// A console that is never cleaned up keeps its result alive for the life of the
// process — 200 rows x 74 columns per closed tab.
test("closeConsole is what frees a result", () => {
  const { setSql, closeConsole } = useDatabaseStore.getState();
  setSql("gone", "SELECT 1;");
  expect(useDatabaseStore.getState().consoles["gone"]).toBeDefined();
  closeConsole("gone");
  expect(useDatabaseStore.getState().consoles["gone"]).toBeUndefined();
});
```

- [ ] **Step 2: Run it**

Run: `cd frontend && bun test src/modules/database/databaseStore.test.ts`
Expected: PASS — `closeConsole` was written in Task 2. This test pins it so the cleanup below cannot be removed silently.

- [ ] **Step 3: Write the shared cleanup hook**

A table document owns a console too, and its tab's params are `{schema, table}`
— not `{consoleId}` — so a check that looks for `params.consoleId` would never
find a table tab and would free its slice the moment the tab scrolled out of the
mount window. The hook derives the id the same way each document does.

Create `frontend/src/modules/database/useConsoleLifetime.ts`:

```ts
import { useEffect } from "react";
import { moduleTabs, useWorkbenchTabsStore } from "@/stores/workbenchTabs";
import { DEFAULT_CONSOLE_ID, useDatabaseStore } from "./databaseStore";

/**
 * The console id a Database tab owns.
 *
 * A console tab carries it directly; a table tab is identified by what it shows,
 * so its console id is derived from that. Both live in one function because the
 * cleanup below has to compute the same id from a tab that the document computed
 * for itself, and two copies of that rule would drift into a leak.
 */
export function consoleIdOfTab(viewId: string, params?: Record<string, unknown>): string | null {
  if (viewId === "database.console") {
    return typeof params?.consoleId === "string" ? params.consoleId : DEFAULT_CONSOLE_ID;
  }
  if (viewId === "database.table") {
    return `table:${String(params?.schema ?? "")}.${String(params?.table ?? "")}`;
  }
  return null;
}

/**
 * Frees a console's slice once its tab is really gone.
 *
 * Unmounting is not closing: WorkbenchViews unmounts a module left alone long
 * enough, and that console has to come back with its SQL and its result. So the
 * slice is dropped only when no open tab still owns the id.
 */
export function useConsoleLifetime(consoleId: string): void {
  const closeConsole = useDatabaseStore((s) => s.closeConsole);
  useEffect(() => {
    return () => {
      const open = moduleTabs(useWorkbenchTabsStore.getState().modules, "database").openTabs;
      const stillOpen = open.some((tab) => consoleIdOfTab(tab.viewId, tab.params) === consoleId);
      if (!stillOpen) {
        closeConsole(consoleId);
      }
    };
  }, [consoleId, closeConsole]);
}
```

- [ ] **Step 4: Call it from both documents**

In `DatabaseView.tsx` and in `TableDataView.tsx`, add below the existing hooks:

```tsx
  useConsoleLifetime(consoleId);
```

importing it from `./useConsoleLifetime` and `../useConsoleLifetime` respectively — `TableDataView.tsx` sits at the module root, so both are `./useConsoleLifetime`.

Then replace `TableDataView`'s inline id with the shared one so the two cannot drift:

```tsx
  const consoleId = consoleIdOfTab("database.table", params) ?? "";
```

- [ ] **Step 5: Run the checks**

Run: `cd frontend && npx tsc --noEmit && bun test`
Expected: clean, all pass.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/modules/database
git commit -m "fix(database): free a console's result when its tab closes"
```

---

## Task 13: Full verification

- [ ] **Step 1: Frontend**

Run: `cd frontend && npx tsc --noEmit && bun test && bun run build`
Expected: tsc clean, all tests pass, build succeeds and the performance budget passes.

- [ ] **Step 2: Backend parity**

Run: `cd .. && go test ./internal/app/... ./internal/modules/... ./internal/bridge/... -count=1`
Expected: all ok. This is what catches a `database.*` view id declared on one side and not the other.

- [ ] **Step 3: Cross-platform**

Run: `for os in darwin linux windows; do GOOS=$os GOARCH=amd64 go vet ./internal/app/ ./internal/modules/; done`
Expected: no output. Scope it to the changed packages — anything importing Wails fails on cross-compile with CGO off, `GOOS=darwin` included.

- [ ] **Step 4: In the app**

In the desktop window: open a table by double-click, open a second table, open a console, open the ERD. Confirm four tabs, that switching between them preserves each console's SQL and result, that closing a table tab does not disturb the others, and that no "Module metadata is unavailable or out of sync" banner appears.

- [ ] **Step 5: Commit any fixes**

```bash
git add -A
git commit -m "fix(database): verification fallout"
```
