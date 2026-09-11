# Database Table Browser Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make opening a table show its data the way DataGrip does — a full-height grid with WHERE and ORDER BY bars and keyset paging — and give the console the same grid underneath.

**Architecture:** One `ResultGrid` component serves both documents, built on a real `<table>` with `table-fixed` + `<colgroup>` so columns can be sized, and one context menu for the whole grid instead of one per cell. `TableDataView` stops being a console with pre-filled SQL: it composes its own statement from a WHERE box, an ORDER BY, and a keyset cursor, and has no SQL editor at all. Paging is keyset where the table has a usable key and OFFSET where it does not, because views and heap tables have none.

**Tech Stack:** React 19, Zustand, `@tanstack/react-virtual` (already used), `@dnd-kit/*` (new), `bun test`.

**No `@tanstack/react-table`.** It was recommended earlier, before WHERE / ORDER BY / paging were settled as server-side. With the server doing all three, its sorted, filtered and paginated row models are all unused, and what is left — column order, column sizes, `flexRender` — is three arrays and a `map`. Sorting the 200 rows a capped query returned would also be a lie: they are not the first 200 of the sort the user asked for.

---

## Amendments — 2026-09-11, during implementation

The plan was written before the code. Six things it assumed turned out not to
hold. Each is corrected in place below; this list is the record of what changed
and why.

1. **A1 — `hasNext` cannot come from the probe row alone (Task 7).** The plan
   asks for `pageSize + 1` rows so the extra one answers "is there a next
   page". But `internal/database/database.go` caps every result at
   `DefaultMaxRows = 200` with no per-query override, so `LIMIT 201` comes
   back as 200 rows with `truncated: true` — the probe row is eaten by the
   server before it reaches the store. Left as written, `hasNext` would be
   `false` on every page, Next would be permanently disabled and the table
   would never page past row 200. `truncated` is the server saying the same
   thing the probe row would have, so `hasNext` reads both.

2. **A2 — the page size is a constant, not the literal 200 (Tasks 7-8).**
   `TableToolbar` computed its row range from a hardcoded `200` and
   `onOpenInConsole` passed a hardcoded `pageSize: 200`, in a plan whose only
   page-size constant lives in the store. Two of the three would have gone on
   claiming 200 after the third changed. `BROWSE_PAGE_SIZE` is exported and
   passed in.

3. **A3 — a page load needs a stale-answer guard (Tasks 6-7).** `executeQuery`
   already guards every console result with a `queryRunId`, because a second
   run started before the first answered must not be overwritten by it.
   `loadBrowsePage` had no such guard, so clicking Next twice quickly, or
   sorting while a page was in flight, could land the older answer last and
   show a page that is not the one the toolbar says. A data browser showing
   the wrong rows silently is the failure this plan exists to avoid, so
   `BrowseState` gains a `runId` and the superseded call is cancelled.

4. **A4 — `rowAsJSON` and `columnValues` move, they are not deleted (Task 2).**
   Task 2 offered to delete them "if nothing else in the file uses them". Two
   things do: `components/rowAsJSON.test.ts` imports both, and the grid needs
   exactly what they do. The grid's inlined `Object.fromEntries(result.columns
   .map(…))` is also not the same function — `rowAsJSON` maps over the ROW and
   falls back to `col_${i}` for an empty name, which is what its tests pin. So
   both move to `grid/rowText.ts`, the test moves with them, and the grid calls
   them instead of reimplementing one of them slightly differently.

5. **A5 — Task 11 replaces a test, it does not only append one.**
   `useConsoleLifetime.test.ts` already asserts `consoleIdOfTab("database.
   table", …) === "table:chiro.ba_user"`. Task 11 changes that branch to
   return null, so appending the new test leaves the suite asserting both
   answers. The old test is replaced.

6. **A6 — the drag needs an activation distance (Task 10).** dnd-kit's pointer
   sensor starts a drag on pointerdown by default. The header cell is also the
   sort button and carries the resize handle, so without a distance threshold
   clicking a header would begin a drag instead of sorting, and a resize drag
   would reorder the column. A `PointerSensor` with `activationConstraint:
   { distance: 4 }` keeps a click a click, and the resize handle stops the
   event reaching the sensor.

---

## File Structure

**Created**
- `frontend/src/modules/database/grid/ResultGrid.tsx` — the shared grid
- `frontend/src/modules/database/grid/gridColumns.ts` — column order and width state, pure
- `frontend/src/modules/database/grid/gridColumns.test.ts`
- `frontend/src/modules/database/grid/rowText.ts` — copy-a-row / copy-a-column text (A4, moved out of `QueryConsole.tsx`)
- `frontend/src/modules/database/grid/rowText.test.ts` (A4, moved from `components/rowAsJSON.test.ts`)
- `frontend/src/modules/database/sql/identifier.ts` — engine-correct quoting
- `frontend/src/modules/database/sql/identifier.test.ts`
- `frontend/src/modules/database/sql/browseQuery.ts` — the statement a table tab runs
- `frontend/src/modules/database/sql/browseQuery.test.ts`
- `frontend/src/modules/database/tableKey.ts` — which columns page a table
- `frontend/src/modules/database/tableKey.test.ts`
- `frontend/src/modules/database/components/TableToolbar.tsx`

**Modified**
- `frontend/src/modules/database/TableDataView.tsx` — data-first, no editor
- `frontend/src/modules/database/components/QueryConsole.tsx` — renders `ResultGrid`
- `frontend/src/modules/database/databaseStore.ts` — browse state per table
- `frontend/package.json` — `@dnd-kit/*`

**Deleted**
- `frontend/src/modules/database/components/rowAsJSON.test.ts` — moved to `grid/rowText.test.ts` (A4)

---

## Phase 1 — The shared grid

Shippable on its own: the console gets sizable columns, a row-number gutter and one context menu instead of 2,220.

### Task 1: Column state, as two arrays

**Files:**
- Create: `frontend/src/modules/database/grid/gridColumns.ts`
- Test: `frontend/src/modules/database/grid/gridColumns.test.ts`

- [ ] **Step 1: Write the failing test**

Create `frontend/src/modules/database/grid/gridColumns.test.ts`:

```ts
import { describe, expect, test } from "bun:test";
import { DEFAULT_COLUMN_WIDTH_PX, orderedColumns, resizeColumn, reorderColumn } from "./gridColumns";

describe("column order", () => {
  // Column state is keyed by name, but a result can repeat a name —
  // SELECT a.id, b.id — so everything here works on positions.
  test("two columns sharing a name stay distinct", () => {
    const ordered = orderedColumns(["id", "id"], []);
    expect(ordered).toEqual([0, 1]);
  });

  test("a saved order survives a query returning the same columns", () => {
    expect(orderedColumns(["a", "b", "c"], [2, 0, 1])).toEqual([2, 0, 1]);
  });

  // A different query returns different columns. A stale order would show
  // them in an order the user never chose, or drop some entirely.
  test("a saved order is dropped when the column count changes", () => {
    expect(orderedColumns(["a", "b"], [2, 0, 1])).toEqual([0, 1]);
  });

  test("moving a column left shifts the rest right", () => {
    expect(reorderColumn([0, 1, 2, 3], 3, 1)).toEqual([0, 3, 1, 2]);
  });

  test("moving a column onto itself changes nothing", () => {
    expect(reorderColumn([0, 1, 2], 1, 1)).toEqual([0, 1, 2]);
  });
});

describe("column width", () => {
  test("an unsized column gets the default", () => {
    expect(resizeColumn([], 2, 0)[2]).toBe(DEFAULT_COLUMN_WIDTH_PX);
  });

  // Dragging left past the minimum must stop at it, not invert the column.
  test("a column cannot be dragged narrower than its minimum", () => {
    const widths = resizeColumn([200, 200], 0, -1000);
    expect(widths[0]).toBeGreaterThan(0);
    expect(widths[0]).toBeLessThan(200);
  });

  test("resizing one column leaves the others alone", () => {
    const widths = resizeColumn([200, 200, 200], 1, 40);
    expect(widths[0]).toBe(200);
    expect(widths[1]).toBe(240);
    expect(widths[2]).toBe(200);
  });
});
```

- [ ] **Step 2: Run it to make sure it fails**

Run: `cd frontend && bun test src/modules/database/grid/gridColumns.test.ts`
Expected: FAIL — cannot resolve `./gridColumns`.

- [ ] **Step 3: Write it**

Create `frontend/src/modules/database/grid/gridColumns.ts`:

```ts
/**
 * A result grid's column order and widths.
 *
 * Positions, not names: a result can repeat a column name — `SELECT a.id,
 * b.id` — and a map keyed by name would collapse the two. Kept pure so the
 * arithmetic can be tested without a DOM, the way tabs/tabWidths is.
 */

/** What a column is given before anyone drags it. */
export const DEFAULT_COLUMN_WIDTH_PX = 180;

/**
 * The narrowest a column may be dragged: enough for the sort caret and a
 * character or two, so a column dragged to nothing can still be dragged back.
 */
export const MIN_COLUMN_WIDTH_PX = 48;

/**
 * The display order for `columns`, or the natural order when the saved one
 * does not describe this result.
 *
 * A console runs a different query every time, and an order saved against the
 * last one would show these columns in an arrangement nobody chose. Length is
 * the cheap check that catches it.
 */
export function orderedColumns(columns: readonly string[], saved: readonly number[]): number[] {
  const natural = columns.map((_, index) => index);
  if (saved.length !== columns.length) {
    return natural;
  }
  const seen = new Set(saved);
  if (seen.size !== saved.length || saved.some((index) => index < 0 || index >= columns.length)) {
    return natural;
  }
  return [...saved];
}

/** Moves the column at `from` to `to`, keeping every other column's order. */
export function reorderColumn(order: readonly number[], from: number, to: number): number[] {
  if (from === to || from < 0 || to < 0 || from >= order.length || to >= order.length) {
    return [...order];
  }
  const next = [...order];
  const [moved] = next.splice(from, 1);
  next.splice(to, 0, moved);
  return next;
}

/** Applies a drag of `delta` px to one column, leaving the rest untouched. */
export function resizeColumn(
  widths: readonly number[],
  index: number,
  delta: number,
): number[] {
  const next = widths.length > 0 ? [...widths] : [];
  while (next.length <= index) {
    next.push(DEFAULT_COLUMN_WIDTH_PX);
  }
  next[index] = Math.max(MIN_COLUMN_WIDTH_PX, (next[index] ?? DEFAULT_COLUMN_WIDTH_PX) + delta);
  return next;
}
```

- [ ] **Step 4: Run the test**

Run: `cd frontend && bun test src/modules/database/grid/gridColumns.test.ts`
Expected: PASS, 8 tests.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/modules/database/grid
git commit -m "feat(database): column order and widths, as positions"
```

---

### Task 2: One context menu for the whole grid

`QueryConsole` wraps every `<th>` and every `<td>` in its own Radix `ContextMenu`. Thirty visible rows of a 74-column table is 2,220 menu roots, each with its own state and context.

**Files:**
- Create: `frontend/src/modules/database/grid/ResultGrid.tsx`
- Modify: `frontend/src/modules/database/components/QueryConsole.tsx`

- [ ] **Step 1: Write the grid**

Create `frontend/src/modules/database/grid/ResultGrid.tsx`. This is the whole component; the console's inline table is replaced by it.

```tsx
import { useRef, useState } from "react";
import { Braces, Columns3, Copy, List } from "lucide-react";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuLabel,
  ContextMenuTrigger,
} from "@/components/ui/context-menu";
import { useVirtualRows } from "@/components/ui/virtual-rows";
import { cn } from "@/lib/utils";
import { copyText } from "@/bridge/clipboard";
import type { QueryResult } from "@/bridge/database";
import {
  DEFAULT_COLUMN_WIDTH_PX,
  MIN_COLUMN_WIDTH_PX,
  orderedColumns,
  resizeColumn,
} from "./gridColumns";
// A4: moved here from QueryConsole rather than reimplemented. rowAsJSON maps
// over the ROW and names an empty column col_${i}; an inlined
// Object.fromEntries over `columns` is a different function with its own bugs.
import { columnValues, rowAsJSON } from "./rowText";

const ROW_HEIGHT = 36;

/** Which cell the pointer was over when the menu opened. */
type Target = { row: number; column: number } | null;

/**
 * The result grid, shared by the console and a table's data.
 *
 * One context menu for the grid, not one per cell. The console mounted a Radix
 * root on every `<th>` and `<td>`, which is 2,220 of them on thirty visible
 * rows of a 74-column table; the menu now reads which cell the pointer was
 * over from a data attribute when it opens.
 *
 * `table-fixed` with an explicit `<colgroup>` is what makes columns sizable at
 * all: under `table-layout: auto` the browser sizes columns from content and a
 * width on a cell is a suggestion. It also stops the columns shifting as
 * virtualised rows come and go, which they did.
 */
export function ResultGrid({
  result,
  widths,
  columnOrder,
  onResize,
  onInspectCell,
  sort,
  onSort,
}: {
  result: QueryResult;
  widths: readonly number[];
  columnOrder: readonly number[];
  onResize: (index: number, delta: number) => void;
  onInspectCell: (cell: { columnName: string; value: string }) => void;
  /** Server-side sort, when the caller has one. Absent in the console. */
  sort?: { column: number; direction: "asc" | "desc" } | null;
  onSort?: (column: number) => void;
  /**
   * Accepts a new column order. Wired in Phase 3; absent in the console, whose
   * columns change with every query, so an order saved against the last one
   * describes columns this one may not have.
   */
  onReorder?: (order: number[]) => void;
}) {
  const rows = useVirtualRows(result.rows.length, ROW_HEIGHT);
  const order = orderedColumns(result.columns, columnOrder);
  const [target, setTarget] = useState<Target>(null);

  const widthOf = (index: number) => widths[index] ?? DEFAULT_COLUMN_WIDTH_PX;
  const cell =
    target && result.rows[target.row]
      ? {
          value: result.rows[target.row][target.column] ?? "",
          name: result.columns[target.column] || `col_${target.column}`,
          row: result.rows[target.row],
        }
      : null;

  return (
    <ContextMenu>
      <ContextMenuTrigger asChild>
        <div
          ref={rows.scrollRef}
          className="min-h-0 flex-1 overflow-auto scrollbar-gutter-stable rounded-md border border-border-subtle bg-surface-well"
          onContextMenu={(event) => {
            const el = (event.target as HTMLElement).closest("[data-cell]");
            const raw = el?.getAttribute("data-cell");
            const [row, column] = raw ? raw.split(":").map(Number) : [NaN, NaN];
            setTarget(Number.isNaN(row) ? null : { row, column });
          }}
        >
          <table
            className="w-full table-fixed border-collapse text-left text-code-12"
            aria-label="Query results"
          >
            <colgroup>
              {/* The gutter. Its width is the row-number column's, fixed. */}
              <col style={{ width: 56 }} />
              {order.map((index) => (
                <col key={index} style={{ width: widthOf(index) }} />
              ))}
            </colgroup>
            <thead className="sticky top-0 z-10 bg-surface-raised">
              <tr>
                <th className="h-10 border-r border-b border-border-subtle px-3 text-code-11 text-text-tertiary">
                  #
                </th>
                {order.map((index) => (
                  <HeaderCell
                    key={`${result.columns[index]}-${index}`}
                    label={result.columns[index]}
                    index={index}
                    sorted={sort?.column === index ? sort.direction : null}
                    onSort={onSort}
                    onResize={onResize}
                  />
                ))}
              </tr>
            </thead>
            <tbody>
              {rows.padTop > 0 && <tr style={{ height: rows.padTop }} aria-hidden />}
              {rows.indices.map((rowIndex) => (
                <tr key={rowIndex} className="group transition-colors hover:bg-surface-raised/60">
                  {/* The row number is the row's own, not the window's: with
                      virtualised rows a counter over the rendered ones would
                      restart every time you scrolled. */}
                  <td className="h-9 border-r border-b border-border-subtle px-3 text-code-11 text-text-tertiary tabular-nums">
                    {rowIndex + 1}
                  </td>
                  {order.map((columnIndex) => {
                    const value = result.rows[rowIndex][columnIndex] ?? "";
                    return (
                      <td
                        key={columnIndex}
                        data-cell={`${rowIndex}:${columnIndex}`}
                        className={cn(
                          "h-9 cursor-pointer truncate border-r border-b border-border-subtle px-4 select-text last:border-r-0 hover:text-text-primary",
                          columnIndex === order[0] ? "text-text-primary" : "text-text-secondary",
                        )}
                        onClick={() =>
                          onInspectCell({
                            columnName: result.columns[columnIndex] || `col_${columnIndex}`,
                            value,
                          })
                        }
                        title="Click to inspect cell"
                      >
                        {value === "" ? (
                          <span className="text-code-11 text-status-warning">NULL</span>
                        ) : (
                          value
                        )}
                      </td>
                    );
                  })}
                </tr>
              ))}
              {rows.padBottom > 0 && <tr style={{ height: rows.padBottom }} aria-hidden />}
            </tbody>
          </table>
        </div>
      </ContextMenuTrigger>
      <ContextMenuContent>
        {cell ? (
          <>
            <ContextMenuItem onSelect={() => void copyText(cell.value)}>
              <Copy />
              Copy cell
            </ContextMenuItem>
            <ContextMenuItem
              onSelect={() => void copyText(rowAsJSON(result.columns, cell.row))}
            >
              <Braces />
              Copy row as JSON
            </ContextMenuItem>
            <ContextMenuItem onSelect={() => void copyText(cell.name)}>
              <Columns3 />
              Copy column name
            </ContextMenuItem>
            <ContextMenuItem
              onSelect={() => void copyText(columnValues(result.rows, target!.column))}
            >
              <List />
              Copy {result.rows.length} value{result.rows.length === 1 ? "" : "s"}
            </ContextMenuItem>
            {/* The count is what is loaded, not what the query would have
                returned. Offering "the column" over a capped result would hand
                someone 200 rows of an unknown total. */}
            {result.truncated ? (
              <ContextMenuLabel className="text-caption-11 font-normal text-status-warning">
                result was capped — this is not the whole column
              </ContextMenuLabel>
            ) : null}
          </>
        ) : null}
      </ContextMenuContent>
    </ContextMenu>
  );
}

/**
 * A header cell, with its own resize handle.
 *
 * The handle is a sibling of the label rather than a child of a button,
 * because a drag target inside a click target is a fight over the pointer.
 */
function HeaderCell({
  label,
  index,
  sorted,
  onSort,
  onResize,
}: {
  label: string;
  index: number;
  sorted: "asc" | "desc" | null;
  onSort?: (index: number) => void;
  onResize: (index: number, delta: number) => void;
}) {
  const dragStart = useRef<number | null>(null);

  return (
    <th className="relative h-10 border-r border-b border-border-subtle p-0 text-code-12-medium text-text-secondary last:border-r-0">
      <button
        type="button"
        className="flex h-full w-full items-center gap-1 truncate px-4 text-left disabled:cursor-default"
        disabled={!onSort}
        onClick={() => onSort?.(index)}
      >
        <span className="truncate">{label}</span>
        {sorted ? (
          <span aria-hidden className="shrink-0 text-text-tertiary">
            {sorted === "asc" ? "▲" : "▼"}
          </span>
        ) : null}
      </button>
      <span
        role="separator"
        aria-label={`Resize ${label}`}
        aria-orientation="vertical"
        className="absolute inset-y-0 right-0 w-1 cursor-col-resize hover:bg-border-default"
        onPointerDown={(event) => {
          event.preventDefault();
          dragStart.current = event.clientX;
          event.currentTarget.setPointerCapture(event.pointerId);
        }}
        onPointerMove={(event) => {
          if (dragStart.current === null) return;
          const delta = event.clientX - dragStart.current;
          if (delta === 0) return;
          dragStart.current = event.clientX;
          onResize(index, delta);
        }}
        onPointerUp={(event) => {
          dragStart.current = null;
          event.currentTarget.releasePointerCapture(event.pointerId);
        }}
      />
    </th>
  );
}

export { MIN_COLUMN_WIDTH_PX };
```

- [ ] **Step 2: Point the console at it**

In `QueryConsole.tsx`, replace the whole `<div ref={resultRows.scrollRef}>…</table></div>` block with:

```tsx
              <ResultGrid
                result={slice.queryResult}
                widths={gridWidths}
                columnOrder={[]}
                onResize={(index, delta) => setGridWidths((w) => resizeColumn(w, index, delta))}
                onInspectCell={setInspectedCell}
              />
```

and above the return:

```tsx
  // Console widths are per mount, not stored: a console runs a different query
  // every time, and a width saved against the last result describes columns
  // this one may not have.
  const [gridWidths, setGridWidths] = useState<number[]>([]);
```

Delete the now-unused `useVirtualRows` import, `resultRows`, `RESULT_ROW_HEIGHT` and the per-cell `ContextMenu` imports — grep each first; `ContextMenu` is used nowhere else in the file, so `grep -c ContextMenu` reaching 0 is the check in Step 3.

**A4: `columnValues` and `rowAsJSON` move rather than being deleted.** They are
not unused — `components/rowAsJSON.test.ts` imports both, and the grid above
calls both. Move them, with their doc comments verbatim, out of
`QueryConsole.tsx` into a new `frontend/src/modules/database/grid/rowText.ts`:

```ts
/**
 * The text behind "copy a row" and "copy a column".
 *
 * Lives beside the grid because the grid's context menu is the only thing that
 * asks for it now; it was in QueryConsole when the menu was.
 */

/** One column's values, newline separated — the shape that pastes into an IN
 *  list or a spreadsheet. A row shorter than the header becomes an empty line,
 *  which `join` already does for a hole; no guard needed. */
export function columnValues(rows: string[][], index: number): string {
  return rows.map((row) => row[index]).join("\n");
}

/**
 * A row is a string array beside a separate column list — JSON has to pair them
 * back up. Values arrive already stringified by the bridge, so this is a shape
 * change, not a re-encode.
 */
export function rowAsJSON(columns: string[], row: string[]): string {
  return JSON.stringify(
    Object.fromEntries(row.map((value, i) => [columns[i] || `col_${i}`, value])),
    null,
    2,
  );
}
```

Then move the test with them — `git mv frontend/src/modules/database/components/rowAsJSON.test.ts frontend/src/modules/database/grid/rowText.test.ts` — and change its import to `from "./rowText"`. Its assertions do not change: they are what pins the two behaviours an inlined version loses.

- [ ] **Step 3: Verify**

Run: `cd frontend && npx tsc --noEmit && bun test`
Expected: tsc clean, 0 fail.

Run: `cd frontend && grep -c ContextMenu src/modules/database/components/QueryConsole.tsx`
Expected: 0 — every menu in the result moved into the grid.

Run: `cd frontend && grep -rn "rowAsJSON\|columnValues" src/modules/database`
Expected: only `grid/rowText.ts`, `grid/rowText.test.ts` and `grid/ResultGrid.tsx` — nothing left importing them from `QueryConsole` (A4).

- [ ] **Step 4: Commit**

```bash
git add frontend/src/modules/database
git commit -m "feat(database): one grid, one context menu, sizable columns"
```

---

## Phase 2 — The table document

### Task 3: Quote an identifier the way the engine does

Building `SELECT … FROM t ORDER BY c` from names the schema handed us needs quoting, or a column named `order`, `first name` or `Größe` breaks the statement. There is no quoting helper in the codebase today.

**Files:**
- Create: `frontend/src/modules/database/sql/identifier.ts`
- Test: `frontend/src/modules/database/sql/identifier.test.ts`

- [ ] **Step 1: Write the failing test**

Create `frontend/src/modules/database/sql/identifier.test.ts`:

```ts
import { describe, expect, test } from "bun:test";
import { quoteIdentifier } from "./identifier";

describe("quoteIdentifier", () => {
  test("MySQL uses backticks, Postgres double quotes", () => {
    expect(quoteIdentifier("users", "mysql")).toBe("`users`");
    expect(quoteIdentifier("users", "postgres")).toBe('"users"');
  });

  test("MariaDB quotes as MySQL", () => {
    expect(quoteIdentifier("users", "mariadb")).toBe("`users`");
  });

  // A reserved word is the everyday case, not an exotic one.
  test("a reserved word survives", () => {
    expect(quoteIdentifier("order", "postgres")).toBe('"order"');
  });

  // The quote character inside a name is the injection this exists to stop.
  // Both dialects escape it by doubling.
  test("an embedded quote is doubled, not dropped", () => {
    expect(quoteIdentifier('a"b', "postgres")).toBe('"a""b"');
    expect(quoteIdentifier("a`b", "mysql")).toBe("`a``b`");
  });

  // A name carrying a statement terminator must come out as one identifier,
  // never as two statements.
  test("a name that looks like SQL stays a name", () => {
    const quoted = quoteIdentifier('x"; DROP TABLE users; --', "postgres");
    expect(quoted.startsWith('"')).toBe(true);
    expect(quoted.endsWith('"')).toBe(true);
    expect(quoted.slice(1, -1)).not.toContain('";');
  });

  test("an empty name is refused rather than quoted into nothing", () => {
    expect(() => quoteIdentifier("", "postgres")).toThrow();
  });
});
```

- [ ] **Step 2: Run it to make sure it fails**

Run: `cd frontend && bun test src/modules/database/sql/identifier.test.ts`
Expected: FAIL — cannot resolve `./identifier`.

- [ ] **Step 3: Write it**

Create `frontend/src/modules/database/sql/identifier.ts`:

```ts
/**
 * Quoting a table or column name into a statement.
 *
 * Every name here comes from the schema the server reported, so this is not
 * guarding against a hostile user — it is guarding against ordinary names the
 * SQL grammar cannot take bare: a reserved word, a space, a hyphen, a non-ASCII
 * letter. The doubling rule also means a name containing the quote character
 * cannot end the identifier early, which is the one case where an ordinary name
 * would otherwise become a second statement.
 */
export function quoteIdentifier(name: string, engine: string | undefined): string {
  if (!name) {
    throw new Error("cannot quote an empty identifier");
  }
  const quote = quoteCharFor(engine);
  return quote + name.split(quote).join(quote + quote) + quote;
}

/**
 * MySQL and MariaDB take backticks; everything else in this app is Postgres,
 * which takes the SQL-standard double quote.
 */
function quoteCharFor(engine: string | undefined): string {
  const normalised = (engine ?? "postgres").toLowerCase();
  return normalised === "mysql" || normalised === "mariadb" ? "`" : '"';
}
```

- [ ] **Step 4: Run the test**

Run: `cd frontend && bun test src/modules/database/sql/identifier.test.ts`
Expected: PASS, 6 tests.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/modules/database/sql
git commit -m "feat(database): quote an identifier the way the engine does"
```

---

### Task 4: Which columns can page a table

**Files:**
- Create: `frontend/src/modules/database/tableKey.ts`
- Test: `frontend/src/modules/database/tableKey.test.ts`

- [ ] **Step 1: Write the failing test**

Create `frontend/src/modules/database/tableKey.test.ts`:

```ts
import { describe, expect, test } from "bun:test";
import { pageKeyFor } from "./tableKey";

const index = (over: Partial<Parameters<typeof pageKeyFor>[0][number]>) => ({
  schema: "public",
  table: "orders",
  name: "PRIMARY",
  columns: ["id"],
  unique: true,
  primary: true,
  ...over,
});

describe("pageKeyFor", () => {
  test("the primary key pages the table", () => {
    expect(pageKeyFor([index({})], "public", "orders")).toEqual(["id"]);
  });

  test("a composite primary key pages in its own column order", () => {
    expect(pageKeyFor([index({ columns: ["tenant_id", "id"] })], "public", "orders")).toEqual([
      "tenant_id",
      "id",
    ]);
  });

  // No primary key but a unique index is still a stable order. Preferring the
  // narrowest keeps the keyset comparison short.
  test("a unique index stands in for a missing primary key", () => {
    const indexes = [
      index({ name: "by_email", columns: ["email"], primary: false }),
      index({ name: "by_pair", columns: ["a", "b"], primary: false }),
    ];
    expect(pageKeyFor(indexes, "public", "orders")).toEqual(["email"]);
  });

  // A nullable unique index is not a total order: NULLs compare as unknown, so
  // a keyset walk would skip or repeat rows. Reported as unkeyed instead.
  test("a non-unique index is not a key", () => {
    const indexes = [index({ name: "by_status", columns: ["status"], unique: false, primary: false })];
    expect(pageKeyFor(indexes, "public", "orders")).toBeNull();
  });

  // Views and heap tables. The caller falls back to OFFSET.
  test("a table with nothing unique has no key", () => {
    expect(pageKeyFor([], "public", "orders")).toBeNull();
  });

  test("another table's indexes do not page this one", () => {
    expect(pageKeyFor([index({ table: "customers" })], "public", "orders")).toBeNull();
  });
});
```

- [ ] **Step 2: Run it to make sure it fails**

Run: `cd frontend && bun test src/modules/database/tableKey.test.ts`
Expected: FAIL — cannot resolve `./tableKey`.

- [ ] **Step 3: Write it**

Create `frontend/src/modules/database/tableKey.ts`:

```ts
import type { IndexInfo } from "@/bridge/database";

/**
 * The columns a keyset walk can page a table by, or null when it has none.
 *
 * Keyset paging needs a total order: a column set that is unique and present on
 * every row, so "the row after this one" has exactly one answer. A primary key
 * is that by definition; a unique index is that in practice. Anything else —
 * a view, a heap table, a mapping table nobody keyed — gets null, and the
 * caller pages by OFFSET instead. Returning a non-unique column here would not
 * fail loudly: it would silently skip rows and repeat others at every page
 * boundary, which is the worst way for a data browser to be wrong.
 */
export function pageKeyFor(
  indexes: readonly IndexInfo[],
  schema: string,
  table: string,
): string[] | null {
  const mine = indexes.filter(
    (index) => index.schema === schema && index.table === table && index.columns.length > 0,
  );

  const primary = mine.find((index) => index.primary);
  if (primary) {
    return [...primary.columns];
  }

  // The narrowest unique index: every column in the key lengthens the row-value
  // comparison each page runs.
  const unique = mine
    .filter((index) => index.unique)
    .sort((a, b) => a.columns.length - b.columns.length)[0];
  return unique ? [...unique.columns] : null;
}
```

- [ ] **Step 4: Run the test**

Run: `cd frontend && bun test src/modules/database/tableKey.test.ts`
Expected: PASS, 6 tests.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/modules/database/tableKey.ts frontend/src/modules/database/tableKey.test.ts
git commit -m "feat(database): decide what can page a table"
```

---

### Task 5: The statement a table tab runs

**Files:**
- Create: `frontend/src/modules/database/sql/browseQuery.ts`
- Test: `frontend/src/modules/database/sql/browseQuery.test.ts`

- [ ] **Step 1: Write the failing test**

Create `frontend/src/modules/database/sql/browseQuery.test.ts`:

```ts
import { describe, expect, test } from "bun:test";
import { buildBrowseQuery } from "./browseQuery";

const base = {
  schema: "public",
  table: "orders",
  engine: "postgres",
  where: "",
  orderBy: null,
  pageKey: ["id"],
  cursor: null,
  pageSize: 200,
};

describe("buildBrowseQuery", () => {
  test("the first page orders by the key and takes one extra row", () => {
    const { sql, mode } = buildBrowseQuery(base);
    expect(mode).toBe("keyset");
    // The extra row is how "is there a next page" is answered without COUNT(*).
    expect(sql).toContain("LIMIT 201");
    expect(sql).toContain('ORDER BY "id" ASC');
    expect(sql).not.toContain("WHERE");
  });

  test("a cursor becomes a row-value comparison", () => {
    const { sql } = buildBrowseQuery({ ...base, cursor: { values: ["42"], direction: "next" } });
    expect(sql).toContain('WHERE ("id") > (\'42\')');
  });

  // Sorting by a non-unique column cannot page on that column alone: ties
  // would skip and repeat rows. The key is appended as the tiebreaker.
  test("a user sort keeps the key as a tiebreaker", () => {
    const { sql } = buildBrowseQuery({
      ...base,
      orderBy: { column: "name", direction: "desc" },
    });
    expect(sql).toContain('ORDER BY "name" DESC, "id" ASC');
  });

  test("paging back reverses the comparison", () => {
    const { sql } = buildBrowseQuery({ ...base, cursor: { values: ["42"], direction: "prev" } });
    expect(sql).toContain('WHERE ("id") < (\'42\')');
    expect(sql).toContain('ORDER BY "id" DESC');
  });

  // Without a key there is no cursor to build, so the walk is by offset.
  test("an unkeyed table pages by offset", () => {
    const { sql, mode } = buildBrowseQuery({ ...base, pageKey: null, offset: 400 });
    expect(mode).toBe("offset");
    expect(sql).toContain("LIMIT 201 OFFSET 400");
  });

  test("the user's WHERE is combined with the cursor, not replaced by it", () => {
    const { sql } = buildBrowseQuery({
      ...base,
      where: "status = 'open'",
      cursor: { values: ["42"], direction: "next" },
    });
    expect(sql).toContain("status = 'open'");
    expect(sql).toContain('("id") > (\'42\')');
    expect(sql).toContain(" AND ");
  });

  test("identifiers are quoted for the engine", () => {
    const { sql } = buildBrowseQuery({ ...base, engine: "mysql" });
    expect(sql).toContain("`orders`");
    expect(sql).toContain("`id`");
  });

  // A value with a quote in it must not end the literal early.
  test("a cursor value carrying a quote is escaped", () => {
    const { sql } = buildBrowseQuery({
      ...base,
      cursor: { values: ["o'brien"], direction: "next" },
    });
    expect(sql).toContain("'o''brien'");
  });
});
```

- [ ] **Step 2: Run it to make sure it fails**

Run: `cd frontend && bun test src/modules/database/sql/browseQuery.test.ts`
Expected: FAIL — cannot resolve `./browseQuery`.

- [ ] **Step 3: Write it**

Create `frontend/src/modules/database/sql/browseQuery.ts`:

```ts
import { quoteIdentifier } from "./identifier";

export type BrowseCursor = {
  /** The key values of the row this page starts after (or before). */
  values: readonly string[];
  direction: "next" | "prev";
};

export type BrowseQuery = {
  sql: string;
  /** How the caller must interpret the cursor it gets back. */
  mode: "keyset" | "offset";
  /** Rows to keep; the statement asks for one more to detect a next page. */
  pageSize: number;
  /** True when the rows come back in reverse and must be flipped. */
  reversed: boolean;
};

/**
 * The statement behind a table tab.
 *
 * Keyset, not OFFSET, wherever the table has a key: OFFSET makes the server
 * scan and discard every row before the page, so page 500 costs five hundred
 * pages of work. A keyset walk asks "the rows after this one" and costs the
 * same at any depth.
 *
 * One extra row is always requested. That is how the pager knows whether a next
 * page exists without a COUNT(*), which on a large table is a full scan every
 * time you open it.
 *
 * Values are inlined rather than bound because the bridge takes a statement and
 * nothing else (`DBQuery(id, sql)`). They are escaped here; identifiers go
 * through quoteIdentifier. The user's own WHERE is passed through untouched —
 * it is their SQL, the same as the console's, and the connection's read-only
 * flag is what limits it.
 */
export function buildBrowseQuery(opts: {
  schema: string;
  table: string;
  engine: string | undefined;
  where: string;
  orderBy: { column: string; direction: "asc" | "desc" } | null;
  pageKey: readonly string[] | null;
  cursor: BrowseCursor | null;
  pageSize: number;
  offset?: number;
}): BrowseQuery {
  const { schema, table, engine, where, orderBy, pageKey, cursor, pageSize } = opts;
  const q = (name: string) => quoteIdentifier(name, engine);
  const target = `${q(schema)}.${q(table)}`;

  const conditions: string[] = [];
  if (where.trim()) {
    conditions.push(`(${where.trim()})`);
  }

  const keyed = pageKey !== null && pageKey.length > 0;
  const reversed = keyed && cursor?.direction === "prev";

  if (keyed && cursor) {
    const comparison = cursor.direction === "next" ? ">" : "<";
    const columns = pageKey.map(q).join(", ");
    const values = cursor.values.map(quoteLiteral).join(", ");
    conditions.push(`(${columns}) ${comparison} (${values})`);
  }

  // The key always ends the ORDER BY. Sorting by a non-unique column alone is
  // not a total order, and a keyset walk over one skips and repeats rows at
  // every page boundary.
  const terms: string[] = [];
  if (orderBy) {
    terms.push(`${q(orderBy.column)} ${orderBy.direction.toUpperCase()}`);
  }
  if (keyed) {
    for (const column of pageKey) {
      terms.push(`${q(column)} ${reversed ? "DESC" : "ASC"}`);
    }
  }

  const parts = [`SELECT * FROM ${target}`];
  if (conditions.length > 0) {
    parts.push(`WHERE ${conditions.join(" AND ")}`);
  }
  if (terms.length > 0) {
    parts.push(`ORDER BY ${terms.join(", ")}`);
  }
  parts.push(`LIMIT ${pageSize + 1}`);
  if (!keyed && opts.offset) {
    parts.push(`OFFSET ${Math.trunc(opts.offset)}`);
  }

  return {
    sql: parts.join(" ") + ";",
    mode: keyed ? "keyset" : "offset",
    pageSize,
    reversed,
  };
}

/** A string literal for the statement: the quote doubles, as in both dialects. */
function quoteLiteral(value: string): string {
  return "'" + value.split("'").join("''") + "'";
}
```

- [ ] **Step 4: Run the test**

Run: `cd frontend && bun test src/modules/database/sql/browseQuery.test.ts`
Expected: PASS, 9 tests.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/modules/database/sql
git commit -m "feat(database): keyset browse statement, offset where there is no key"
```

---

### Task 6: Browse state per table

**Files:**
- Modify: `frontend/src/modules/database/databaseStore.ts`
- Test: `frontend/src/modules/database/browseState.test.ts` (create)

- [ ] **Step 1: Write the failing test**

Create `frontend/src/modules/database/browseState.test.ts`:

```ts
import { beforeEach, expect, test } from "bun:test";
import { EMPTY_BROWSE, useDatabaseStore } from "./databaseStore";

beforeEach(() => {
  useDatabaseStore.setState({ browse: {} });
});

test("a table never browsed reads as empty, not undefined", () => {
  const state = useDatabaseStore.getState().browseState("public.orders");
  expect(state.where).toBe("");
  expect(state.rows).toBeNull();
  expect(state.cursors).toEqual([]);
});

test("two tables hold their own filter", () => {
  const { setBrowseWhere } = useDatabaseStore.getState();
  setBrowseWhere("public.orders", "status = 'open'");
  setBrowseWhere("public.customers", "");

  expect(useDatabaseStore.getState().browseState("public.orders").where).toBe("status = 'open'");
  expect(useDatabaseStore.getState().browseState("public.customers").where).toBe("");
});

// Changing the filter invalidates the walk: the cursors describe positions in
// a result the new WHERE no longer produces.
test("changing the filter resets the page", () => {
  const { setBrowseWhere } = useDatabaseStore.getState();
  useDatabaseStore.setState((s) => ({
    browse: {
      ...s.browse,
      "public.orders": { ...s.browseState("public.orders"), cursors: [["10"], ["20"]], pageIndex: 2 },
    },
  }));

  setBrowseWhere("public.orders", "status = 'open'");

  const state = useDatabaseStore.getState().browseState("public.orders");
  expect(state.cursors).toEqual([]);
  expect(state.pageIndex).toBe(0);
});

test("changing the sort resets the page for the same reason", () => {
  const { setBrowseOrder } = useDatabaseStore.getState();
  useDatabaseStore.setState((s) => ({
    browse: {
      ...s.browse,
      "public.orders": { ...s.browseState("public.orders"), cursors: [["10"]], pageIndex: 1 },
    },
  }));

  setBrowseOrder("public.orders", { column: "name", direction: "asc" });

  expect(useDatabaseStore.getState().browseState("public.orders").pageIndex).toBe(0);
});
```

- [ ] **Step 2: Run it to make sure it fails**

Run: `cd frontend && bun test src/modules/database/browseState.test.ts`
Expected: FAIL — `EMPTY_BROWSE` is not exported and `browseState` is not a function.

- [ ] **Step 3: Add the slice**

In `databaseStore.ts`, beside `ConsoleState`, add:

```ts
/**
 * One table tab's browsing state.
 *
 * `cursors` is the key of the LAST row of every page already visited, so page
 * n starts after cursors[n-1]. Holding the trail rather than one cursor is what
 * makes the back button work: a keyset walk has no way to compute the previous
 * page's start from the current one.
 */
export type BrowseState = {
  where: string;
  orderBy: { column: string; direction: "asc" | "desc" } | null;
  rows: QueryResult | null;
  loading: boolean;
  error: string;
  /** Last-row keys of pages already seen, one per page boundary crossed. */
  cursors: string[][];
  pageIndex: number;
  /** Whether the server returned the extra row, i.e. another page exists. */
  hasNext: boolean;
  /** Column widths, kept per table because the columns are the table's own. */
  widths: number[];
  /**
   * A3: which load this state belongs to. Bumped when a page is asked for, and
   * checked when one answers — the same guard `executeQuery` puts on a console
   * result, for the same reason: Next clicked twice, or a sort applied while a
   * page is in flight, must not be overwritten by the older answer landing
   * last. A browser silently showing rows the toolbar does not describe is
   * worse than one that is slow.
   */
  runId: number;
};

export const EMPTY_BROWSE: BrowseState = Object.freeze({
  where: "",
  orderBy: null,
  rows: null,
  loading: false,
  error: "",
  cursors: [],
  pageIndex: 0,
  hasNext: false,
  widths: [],
  runId: 0,
}) as BrowseState;
```

Add to `DatabaseStoreState`:

```ts
  /** Every open table tab's browsing state, keyed "schema.table". */
  browse: Record<string, BrowseState>;
  browseState: (key: string) => BrowseState;
  setBrowseWhere: (key: string, where: string) => void;
  setBrowseOrder: (key: string, orderBy: BrowseState["orderBy"]) => void;
  setBrowseWidths: (key: string, widths: number[]) => void;
  closeBrowse: (key: string) => void;
```

- [ ] **Step 4: Implement it**

Initial value `browse: {}`. Beside `patchConsole`, add the twin:

```ts
function patchBrowse(
  set: (fn: (state: DatabaseStoreState) => Partial<DatabaseStoreState>) => void,
  key: string,
  patch: Partial<BrowseState>,
): void {
  set((state) => ({
    browse: {
      ...state.browse,
      [key]: { ...(state.browse[key] ?? EMPTY_BROWSE), ...patch },
    },
  }));
}
```

and the actions:

```ts
  browseState: (key) => get().browse[key] ?? EMPTY_BROWSE,

  // Both of these invalidate the walk: the cursors describe positions in a
  // result the new filter or sort no longer produces, and reusing them would
  // page through rows that are not there.
  setBrowseWhere: (key, where) =>
    patchBrowse(set, key, { where, cursors: [], pageIndex: 0, hasNext: false }),

  setBrowseOrder: (key, orderBy) =>
    patchBrowse(set, key, { orderBy, cursors: [], pageIndex: 0, hasNext: false }),

  setBrowseWidths: (key, widths) => patchBrowse(set, key, { widths }),

  closeBrowse: (key) =>
    set((state) => {
      if (!(key in state.browse)) {
        return state;
      }
      const { [key]: _closed, ...rest } = state.browse;
      return { browse: rest };
    }),
```

- [ ] **Step 5: Run the test**

Run: `cd frontend && bun test src/modules/database/browseState.test.ts`
Expected: PASS, 4 tests.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/modules/database
git commit -m "feat(database): browsing state per table"
```

---

### Task 7: Fetching a page

**Files:**
- Modify: `frontend/src/modules/database/databaseStore.ts`
- Test: `frontend/src/modules/database/browseState.test.ts`

- [ ] **Step 1: Write the failing test**

Append to `browseState.test.ts`:

```ts
// The statement asks for pageSize + 1 rows. The extra one answers "is there a
// next page" without a COUNT(*), and must never reach the grid.
test("the probe row sets hasNext and is not shown", () => {
  const { applyBrowsePage } = useDatabaseStore.getState();
  const rows = Array.from({ length: 201 }, (_, i) => [String(i)]);

  applyBrowsePage("public.orders", {
    result: { columns: ["id"], rows, rowCount: 201, truncated: false },
    pageSize: 200,
    reversed: false,
    pageKey: ["id"],
  });

  const state = useDatabaseStore.getState().browseState("public.orders");
  expect(state.hasNext).toBe(true);
  expect(state.rows?.rows).toHaveLength(200);
});

test("a short page is the last page", () => {
  const { applyBrowsePage } = useDatabaseStore.getState();
  applyBrowsePage("public.orders", {
    result: { columns: ["id"], rows: [["1"], ["2"]], rowCount: 2, truncated: false },
    pageSize: 200,
    reversed: false,
    pageKey: ["id"],
  });

  expect(useDatabaseStore.getState().browseState("public.orders").hasNext).toBe(false);
});

// A1. The server caps every result at DefaultMaxRows = 200 (internal/database/
// database.go) and there is no per-query override, so LIMIT 201 comes back as
// 200 rows with `truncated` set — the probe row never survives the trip. If
// hasNext read the row count alone it would be false on every full page and the
// table could not be paged past row 200 at all. `truncated` is the server
// saying exactly what the probe row would have said.
test("a page the server capped still has a next page", () => {
  const { applyBrowsePage } = useDatabaseStore.getState();
  const rows = Array.from({ length: 200 }, (_, i) => [String(i)]);

  applyBrowsePage("public.orders", {
    result: { columns: ["id"], rows, rowCount: 200, truncated: true },
    pageSize: 200,
    reversed: false,
    pageKey: ["id"],
  });

  const state = useDatabaseStore.getState().browseState("public.orders");
  expect(state.hasNext).toBe(true);
  expect(state.rows?.rows).toHaveLength(200);
});

// Exactly one page of matching rows is not a capped result: the driver ran out
// before the cap, so `truncated` is false and there is genuinely no next page.
test("a full page that the server did not cap is the last page", () => {
  const { applyBrowsePage } = useDatabaseStore.getState();
  const rows = Array.from({ length: 200 }, (_, i) => [String(i)]);

  applyBrowsePage("public.orders", {
    result: { columns: ["id"], rows, rowCount: 200, truncated: false },
    pageSize: 200,
    reversed: false,
    pageKey: ["id"],
  });

  expect(useDatabaseStore.getState().browseState("public.orders").hasNext).toBe(false);
});

// Paging back asks the server for rows before the cursor in DESC order, so
// they arrive bottom-first and have to be flipped before display.
test("a reversed page is flipped back", () => {
  const { applyBrowsePage } = useDatabaseStore.getState();
  applyBrowsePage("public.orders", {
    result: { columns: ["id"], rows: [["3"], ["2"], ["1"]], rowCount: 3, truncated: false },
    pageSize: 200,
    reversed: true,
    pageKey: ["id"],
  });

  expect(useDatabaseStore.getState().browseState("public.orders").rows?.rows).toEqual([
    ["1"],
    ["2"],
    ["3"],
  ]);
});
```

- [ ] **Step 2: Run it to make sure it fails**

Run: `cd frontend && bun test src/modules/database/browseState.test.ts`
Expected: FAIL — `applyBrowsePage` is not a function.

- [ ] **Step 3: Implement it**

Declare on the state type:

```ts
  /**
   * Turns one page's raw answer into what the grid shows: drops the probe row,
   * flips a backwards page, and records whether another page exists.
   */
  applyBrowsePage: (
    key: string,
    page: {
      result: QueryResult;
      pageSize: number;
      reversed: boolean;
      pageKey: readonly string[] | null;
    },
  ) => void;
  /** Runs the statement for the current page of a table tab. */
  loadBrowsePage: (key: string, opts: { schema: string; table: string }) => Promise<void>;
  /** Moves one page forward or back and loads it. */
  stepBrowsePage: (
    key: string,
    opts: { schema: string; table: string },
    step: 1 | -1,
  ) => Promise<void>;
```

Implement `applyBrowsePage`:

```ts
  applyBrowsePage: (key, { result, pageSize, reversed }) => {
    // A1. Two ways to learn there is more: the probe row arrived, or the server
    // ate it. DBQuery caps at DefaultMaxRows = 200 with no override, so a
    // LIMIT 201 for a 200-row page comes back as 200 rows with `truncated`
    // set and the probe row gone. Reading the count alone would leave hasNext
    // false on every full page and the table unpageable past row 200.
    const hasNext = result.rows.length > pageSize || result.truncated;
    const kept = result.rows.length > pageSize ? result.rows.slice(0, pageSize) : result.rows;
    // A backwards page was asked for in DESC order so the server could walk
    // away from the cursor; the rows arrive bottom-first.
    const rows = reversed ? [...kept].reverse() : kept;
    patchBrowse(set, key, {
      rows: { ...result, rows, rowCount: rows.length },
      hasNext: reversed ? true : hasNext,
      loading: false,
      error: "",
    });
  },
```

- [ ] **Step 4: Run the test**

Run: `cd frontend && bun test src/modules/database/browseState.test.ts`
Expected: PASS, 9 tests.

- [ ] **Step 5: Implement the loader**

```ts
  loadBrowsePage: async (key, { schema, table }) => {
    const { selectedProfileId, profiles } = get();
    if (!selectedProfileId) {
      return;
    }
    const browse = get().browseState(key);
    const engine = profiles.find((p) => p.id === selectedProfileId)?.engine;
    const pageKey = get().pageKeys[key] ?? null;
    const cursor =
      pageKey && browse.pageIndex > 0
        ? { values: browse.cursors[browse.pageIndex - 1] ?? [], direction: "next" as const }
        : null;

    const query = buildBrowseQuery({
      schema,
      table,
      engine,
      where: browse.where,
      orderBy: browse.orderBy,
      pageKey,
      cursor,
      pageSize: BROWSE_PAGE_SIZE,
      offset: pageKey ? undefined : browse.pageIndex * BROWSE_PAGE_SIZE,
    });

    // A3. The same stale-answer guard executeQuery puts on a console result.
    // Next clicked twice, or a sort applied while a page is in flight, leaves
    // two calls racing; without this the older one can land last and fill the
    // grid with rows the toolbar no longer describes.
    const runId = browse.runId + 1;
    patchBrowse(set, key, { loading: true, error: "", runId });
    // And the superseded statement is stopped rather than left to finish: the
    // page it would return is one nobody is going to look at.
    pendingBrowseQueries.get(key)?.cancel();
    try {
      const call = runQuery(selectedProfileId, query.sql);
      pendingBrowseQueries.set(key, call);
      const result = await call;
      if (get().browseState(key).runId !== runId) {
        return;
      }
      get().applyBrowsePage(key, {
        result,
        pageSize: query.pageSize,
        reversed: query.reversed,
        pageKey,
      });
      // Remember where this page ended, so the next one can start after it.
      if (pageKey && result.rows.length > 0) {
        const last = result.rows[Math.min(result.rows.length, query.pageSize) - 1];
        const keyIndexes = pageKey.map((column) => result.columns.indexOf(column));
        if (!keyIndexes.includes(-1)) {
          const trail = [...get().browseState(key).cursors];
          trail[get().browseState(key).pageIndex] = keyIndexes.map((i) => last[i]);
          patchBrowse(set, key, { cursors: trail });
        }
      }
    } catch (e) {
      // Both branches check the guard first. A cancel usually means a newer
      // load superseded this one, and clearing `loading` for the old call
      // would leave the spinner off while the new page is still coming.
      if (get().browseState(key).runId !== runId) {
        return;
      }
      if (isCancelError(e)) {
        patchBrowse(set, key, { loading: false });
        return;
      }
      patchBrowse(set, key, { loading: false, error: errorMessage(e) });
    } finally {
      if (get().browseState(key).runId === runId) {
        pendingBrowseQueries.delete(key);
      }
    }
  },

  stepBrowsePage: async (key, target, step) => {
    const browse = get().browseState(key);
    const next = browse.pageIndex + step;
    if (next < 0 || (step === 1 && !browse.hasNext)) {
      return;
    }
    patchBrowse(set, key, { pageIndex: next });
    await get().loadBrowsePage(key, target);
  },
```

Beside the other constants, add:

```ts
/**
 * Rows per page. The backend caps every result at DefaultMaxRows = 200
 * (internal/database/database.go) with no per-query override, so a page cannot
 * usefully be larger than this — and the LIMIT 201 this plan sends comes back
 * as 200 rows with `truncated` set rather than 201 rows. applyBrowsePage reads
 * `truncated` as the probe row the server ate (A1).
 *
 * A2: exported, because the toolbar numbers its rows from it and "Open in
 * console" composes a statement with it. Three copies of 200 would drift.
 */
export const BROWSE_PAGE_SIZE = 200;
```

and beside `pendingQueries`, its twin (A3):

```ts
/**
 * The in-flight page per table tab, so a superseded load can be stopped. Same
 * reasoning as pendingQueries: a CancellableCall is not state, and holding it
 * in the store would re-render every open table whenever any one of them paged.
 */
const pendingBrowseQueries = new Map<string, ReturnType<typeof runQuery>>();
```

And on `DatabaseStoreState`, beside `browse`:

```ts
  /**
   * The columns that page each table, or null where it has none. Read once per
   * table from its indexes, because a keyset walk cannot be built without it
   * and an index list is a round trip.
   */
  pageKeys: Record<string, string[] | null>;
  setPageKey: (key: string, columns: string[] | null) => void;
```

with initial value `pageKeys: {}` and:

```ts
  setPageKey: (key, columns) =>
    set((state) => ({ pageKeys: { ...state.pageKeys, [key]: columns } })),
```

- [ ] **Step 6: Verify**

Run: `cd frontend && npx tsc --noEmit && bun test`
Expected: tsc clean, 0 fail.

- [ ] **Step 7: Commit**

```bash
git add frontend/src/modules/database
git commit -m "feat(database): walk a table a page at a time"
```

---

### Task 8: The table document, data first

**Files:**
- Create: `frontend/src/modules/database/components/TableToolbar.tsx`
- Modify: `frontend/src/modules/database/TableDataView.tsx`

- [ ] **Step 1: Write the toolbar**

Create `frontend/src/modules/database/components/TableToolbar.tsx`:

```tsx
import { ChevronLeft, ChevronRight, RefreshCw, SquareArrowOutUpRight } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";

/**
 * A table tab's controls: what to filter by, what to order by, and where in the
 * table you are.
 *
 * Two boxes rather than a SQL editor. Opening a table is a request to see its
 * data, and an editor spending most of the height on a statement you did not
 * ask to write inverts that — the two things anyone actually changes while
 * browsing are the filter and the sort. Anything beyond them opens in a
 * console, which is what the last button is for.
 */
export function TableToolbar({
  where,
  onWhereChange,
  onApply,
  orderLabel,
  pageIndex,
  rowCount,
  hasNext,
  keyed,
  loading,
  onStep,
  onRefresh,
  onOpenInConsole,
}: {
  where: string;
  onWhereChange: (value: string) => void;
  onApply: () => void;
  orderLabel: string;
  pageIndex: number;
  rowCount: number;
  hasNext: boolean;
  keyed: boolean;
  loading: boolean;
  onStep: (step: 1 | -1) => void;
  onRefresh: () => void;
  onOpenInConsole: () => void;
  /**
   * A2: rows per page, from the store's BROWSE_PAGE_SIZE rather than a literal
   * 200 spelled here. The row range is the one thing in this component that
   * would go on claiming 200 after the page size changed, and it would look
   * right while being wrong.
   */
  pageSize: number;
}) {
  const first = pageIndex * pageSize + 1;
  const last = pageIndex * pageSize + rowCount;

  return (
    <div className="flex shrink-0 flex-col gap-2 pb-2">
      <div className="flex items-center gap-2">
        <span className="shrink-0 text-code-11 text-text-tertiary">WHERE</span>
        <Input
          value={where}
          onChange={(event) => onWhereChange(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === "Enter") {
              event.preventDefault();
              onApply();
            }
          }}
          placeholder="status = 'open'"
          aria-label="Filter rows"
          className="h-(--control-height-md) border-border-subtle bg-surface-well text-code-12"
        />
        <span className="shrink-0 text-code-11 text-text-tertiary">{orderLabel}</span>
      </div>

      <div className="flex items-center gap-2">
        <Button variant="ghost" size="xs" onClick={onRefresh} disabled={loading}>
          <RefreshCw className={loading ? "animate-spin" : undefined} />
          Refresh
        </Button>
        <Button variant="ghost" size="xs" onClick={onOpenInConsole}>
          <SquareArrowOutUpRight />
          Open in console
        </Button>

        <span className="flex-1" />

        {/* Row numbers, not a total: a COUNT(*) on a large table is a full scan
            every time you open it, and "of 501+" is the honest shape when the
            answer is only known one page ahead. */}
        <span className="text-code-11 text-text-tertiary tabular-nums">
          {rowCount === 0 ? "no rows" : `${first}–${last}${hasNext ? "+" : ""}`}
        </span>
        <Button
          variant="ghost"
          size="icon-xs"
          aria-label="Previous page"
          onClick={() => onStep(-1)}
          disabled={loading || pageIndex === 0}
        >
          <ChevronLeft />
        </Button>
        <Button
          variant="ghost"
          size="icon-xs"
          aria-label="Next page"
          onClick={() => onStep(1)}
          disabled={loading || !hasNext}
        >
          <ChevronRight />
        </Button>
        {!keyed ? (
          // Worth saying: without a key the walk is by offset, which the server
          // pays for page by page, and a row inserted meanwhile shifts the
          // window under you.
          <span
            className="text-code-11 text-text-tertiary"
            title="This table has no primary or unique key, so pages are counted rather than walked. Deep pages are slower, and rows inserted while you page can shift the window."
          >
            unkeyed
          </span>
        ) : null}
      </div>
    </div>
  );
}
```

- [ ] **Step 2: Rewrite the document**

Replace `frontend/src/modules/database/TableDataView.tsx` with:

```tsx
import { useEffect } from "react";
import { useShallow } from "zustand/react/shallow";
import { listIndexes } from "@/bridge/database";
import { ResultGrid } from "./grid/ResultGrid";
import { resizeColumn } from "./grid/gridColumns";
import { TableToolbar } from "./components/TableToolbar";
import { DatabaseOverlays } from "./DatabaseOverlays";
import { BROWSE_PAGE_SIZE, useDatabaseStore } from "./databaseStore";
import { useDatabaseChrome } from "./useDatabaseChrome";
import { useIsActiveDocument } from "./useIsActiveDocument";
import { openConsoleTab } from "./documents";
import { buildBrowseQuery } from "./sql/browseQuery";
import { pageKeyFor } from "./tableKey";

/**
 * One table's data, as its own document.
 *
 * No SQL editor. Opening a table is a request to see rows, and the statement
 * behind them is composed from the filter, the sort and the page — the three
 * things a person browsing actually changes. "Open in console" hands the
 * composed statement to a console tab for anything past that.
 */
export function TableDataView({ params }: { params?: Record<string, unknown> }) {
  const schema = typeof params?.schema === "string" ? params.schema : "";
  const table = typeof params?.table === "string" ? params.table : "";
  const key = `${schema}.${table}`;

  useDatabaseChrome();
  const isActive = useIsActiveDocument("database.table", { schema, table });

  const {
    selectedProfileId,
    profiles,
    browse,
    pageKey,
    setBrowseWhere,
    setBrowseOrder,
    setBrowseWidths,
    setPageKey,
    loadBrowsePage,
    stepBrowsePage,
    setInspectedCell,
    setSql,
  } = useDatabaseStore(
    useShallow((s) => ({
      selectedProfileId: s.selectedProfileId,
      profiles: s.profiles,
      browse: s.browse[key] ?? s.browseState(key),
      pageKey: s.pageKeys[key] ?? null,
      setBrowseWhere: s.setBrowseWhere,
      setBrowseOrder: s.setBrowseOrder,
      setBrowseWidths: s.setBrowseWidths,
      setPageKey: s.setPageKey,
      loadBrowsePage: s.loadBrowsePage,
      stepBrowsePage: s.stepBrowsePage,
      setInspectedCell: s.setInspectedCell,
      setSql: s.setSql,
    })),
  );

  // The key decides whether this table can be walked or has to be counted
  // through, so it is read before the first page.
  useEffect(() => {
    if (!selectedProfileId || !schema || !table) {
      return;
    }
    let cancelled = false;
    void listIndexes(selectedProfileId, schema)
      .then((indexes) => {
        if (!cancelled) {
          setPageKey(key, pageKeyFor(indexes, schema, table));
        }
      })
      .catch(() => {
        // An index list we could not read is not a reason to refuse the table:
        // unkeyed is the safe reading, and it pages by offset.
        if (!cancelled) {
          setPageKey(key, null);
        }
      });
    return () => {
      cancelled = true;
    };
  }, [selectedProfileId, schema, table, key, setPageKey]);

  const hasRows = browse.rows !== null;
  useEffect(() => {
    if (!hasRows && selectedProfileId && table) {
      void loadBrowsePage(key, { schema, table });
    }
  }, [hasRows, selectedProfileId, key, schema, table, loadBrowsePage]);

  const engine = profiles.find((p) => p.id === selectedProfileId)?.engine;

  return (
    <div className="flex h-full min-h-0 flex-col overflow-hidden">
      <TableToolbar
        where={browse.where}
        onWhereChange={(value) => setBrowseWhere(key, value)}
        onApply={() => void loadBrowsePage(key, { schema, table })}
        orderLabel={
          browse.orderBy
            ? `ORDER BY ${browse.orderBy.column} ${browse.orderBy.direction.toUpperCase()}`
            : "ORDER BY —"
        }
        pageIndex={browse.pageIndex}
        pageSize={BROWSE_PAGE_SIZE}
        rowCount={browse.rows?.rows.length ?? 0}
        hasNext={browse.hasNext}
        keyed={pageKey !== null}
        loading={browse.loading}
        onStep={(step) => void stepBrowsePage(key, { schema, table }, step)}
        onRefresh={() => void loadBrowsePage(key, { schema, table })}
        onOpenInConsole={() => {
          const consoleId = `console-${key}`;
          setSql(
            consoleId,
            buildBrowseQuery({
              schema,
              table,
              engine,
              where: browse.where,
              orderBy: browse.orderBy,
              pageKey,
              cursor: null,
              pageSize: BROWSE_PAGE_SIZE,
            }).sql,
          );
          openConsoleTab(consoleId, table);
        }}
      />

      {browse.error ? (
        <div className="mb-2 shrink-0 rounded-md border border-(--border-danger) bg-(--feedback-danger-soft) p-3 text-body-12 text-status-danger">
          {browse.error}
        </div>
      ) : null}

      {browse.rows ? (
        <ResultGrid
          result={browse.rows}
          widths={browse.widths}
          columnOrder={[]}
          onResize={(index, delta) => setBrowseWidths(key, resizeColumn(browse.widths, index, delta))}
          onInspectCell={setInspectedCell}
          sort={
            browse.orderBy
              ? {
                  column: browse.rows.columns.indexOf(browse.orderBy.column),
                  direction: browse.orderBy.direction,
                }
              : null
          }
          onSort={(index) => {
            const column = browse.rows!.columns[index];
            const next =
              browse.orderBy?.column === column && browse.orderBy.direction === "asc"
                ? ("desc" as const)
                : ("asc" as const);
            setBrowseOrder(key, { column, direction: next });
            void loadBrowsePage(key, { schema, table });
          }}
        />
      ) : null}

      {isActive ? <DatabaseOverlays /> : null}
    </div>
  );
}

export default TableDataView;
```

- [ ] **Step 3: Verify**

Run: `cd frontend && npx tsc --noEmit && bun test`
Expected: tsc clean, 0 fail.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/modules/database
git commit -m "feat(database): a table tab shows data, not a statement"
```

---

## Phase 3 — Column reorder

### Task 9: Install dnd-kit

**Files:**
- Modify: `frontend/package.json`

- [ ] **Step 1: Install**

```bash
cd frontend && bun add @dnd-kit/core @dnd-kit/modifiers @dnd-kit/sortable @dnd-kit/utilities
```

- [ ] **Step 2: Check the budget**

Run: `cd frontend && bun run build`
Expected: initial JS under 1 MiB, CSS under 280 KiB. The grid is inside the lazily-loaded Database module, so dnd-kit should not reach the initial bundle — if the budget moves by more than a few KiB, say so rather than raising the budget.

- [ ] **Step 3: Commit**

```bash
git add frontend/package.json frontend/bun.lock
git commit -m "build(frontend): add dnd-kit for column reordering"
```

---

### Task 10: Drag a column into place

**Files:**
- Modify: `frontend/src/modules/database/grid/ResultGrid.tsx`
- Modify: `frontend/src/modules/database/TableDataView.tsx`
- Modify: `frontend/src/modules/database/databaseStore.ts`

- [ ] **Step 1: Make the header cells sortable**

**A6 first: the sensor needs an activation distance.** dnd-kit's `PointerSensor`
claims the pointer on pointerdown by default. The header cell is also the sort
button and carries the resize handle, so without a threshold clicking a header
would start a drag instead of sorting, and dragging the resize handle would
reorder the column instead of widening it. In `ResultGrid`:

```tsx
  // A click is a sort and a drag is a reorder, and they start with the same
  // pointerdown — four pixels is what tells them apart. Without this the sort
  // never fires and the resize handle reorders instead of resizing.
  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 4 } }),
  );
```

and the resize handle's `onPointerDown` gets `event.stopPropagation()` beside
its existing `event.preventDefault()`, so a resize never reaches the sensor at
all.

Then wrap the header row in `DndContext` + `SortableContext`, and give `HeaderCell` a `useSortable` with `position: relative` and a `transform`:

```tsx
      <DndContext
        sensors={sensors}
        collisionDetection={closestCenter}
        modifiers={[restrictToHorizontalAxis]}
        onDragEnd={(event) => {
          const { active, over } = event;
          if (!over || active.id === over.id) return;
          const from = order.indexOf(Number(active.id));
          const to = order.indexOf(Number(over.id));
          onReorder?.(reorderColumn(order, from, to));
        }}
      >
        <SortableContext items={order} strategy={horizontalListSortingStrategy}>
          {/* header cells */}
        </SortableContext>
      </DndContext>
```

and in `HeaderCell`:

```tsx
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({
    id: index,
  });

  // `attributes` and `listeners` go on the `<th>`, not on the sort button: a
  // drag handle nested inside a click target is the same fight over the
  // pointer the resize handle already avoids by being a sibling.

  const style: CSSProperties = {
    // relative + transform, never out of flow: a `<th>` taken out of flow takes
    // the table's column widths with it, which is the same reason virtual-rows
    // pads with spacer rows instead of positioning them.
    position: "relative",
    transform: CSS.Translate.toString(transform),
    transition,
    zIndex: isDragging ? 1 : 0,
    opacity: isDragging ? 0.8 : 1,
  };
```

- [ ] **Step 2: Leave the cells out of it**

Do NOT give the `<td>`s a `useSortable`. A `SortableContext` per cell is thirty rows times seventy-four columns of Radix-scale machinery, and the column snapping into place on drop is a smaller loss than that. Add the comment saying so:

```tsx
              {/* Only the header drags. Giving each cell a useSortable would
                  mount one per visible cell — 2,220 on a 74-column table — to
                  animate a column that snaps into place on drop anyway. */}
```

- [ ] **Step 3: Persist the order per table**

Add to `BrowseState` and to `EMPTY_BROWSE` (as `order: []`):

```ts
  /**
   * Display order as column positions. Per table, not per module: the columns
   * belong to the table, and a position means nothing against another one.
   */
  order: number[];
```

Declare on `DatabaseStoreState`, beside `setBrowseWidths`:

```ts
  setBrowseOrderColumns: (key: string, order: number[]) => void;
```

and implement it beside that action:

```ts
  setBrowseOrderColumns: (key, order) => patchBrowse(set, key, { order }),
```

In `TableDataView.tsx`, pass them to the grid:

```tsx
          columnOrder={browse.order}
          onReorder={(next) => setBrowseOrderColumns(key, next)}
```

adding `setBrowseOrderColumns: s.setBrowseOrderColumns` to its `useShallow` selector.

The console keeps `columnOrder={[]}` and passes no `onReorder`: its columns change with every query, so an order saved against the last one describes columns this one may not have — which is what `orderedColumns` drops on a length mismatch anyway.

- [ ] **Step 4: Verify**

Run: `cd frontend && npx tsc --noEmit && bun test && bun run build`
Expected: tsc clean, 0 fail, budget passes.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/modules/database
git commit -m "feat(database): drag a column into place"
```

---

## Task 11: A table tab frees its own state

The previous plan gave a table tab a console, so `consoleIdOfTab` returned one
for `database.table` and `useConsoleLifetime` freed it. Task 8 removed the
console — a table tab now holds a `BrowseState` instead, and nothing frees it.
Left alone it keeps a page of rows per table ever opened, for the life of the
process.

**Files:**
- Modify: `frontend/src/modules/database/useConsoleLifetime.ts`
- Modify: `frontend/src/modules/database/TableDataView.tsx`
- Test: `frontend/src/modules/database/useConsoleLifetime.test.ts`

- [ ] **Step 1: Write the failing test**

**A5: replace, do not only append.** The file already asserts the old answer:

```ts
test("a table tab's console id comes from what it shows", () => {
  expect(consoleIdOfTab("database.table", { schema: "chiro", table: "ba_user" })).toBe(
    "table:chiro.ba_user",
  );
});
```

Delete that test and its three-line comment above it. Leaving it in place would
have the suite assert both answers for the same call, and one of them has to
fail. The test below replaces it — it covers the same call and says what the
answer is now.

Then add to `frontend/src/modules/database/useConsoleLifetime.test.ts`:

```ts
// A table tab stopped owning a console when it stopped being one. Claiming an
// id here would have closeConsole free a console a real console tab might be
// using, and would leave the browse state — a page of rows — behind instead.
test("a table tab owns browse state, not a console", () => {
  expect(consoleIdOfTab("database.table", { schema: "chiro", table: "ba_user" })).toBeNull();
  expect(browseKeyOfTab("database.table", { schema: "chiro", table: "ba_user" })).toBe(
    "chiro.ba_user",
  );
});

test("a console tab owns no browse state", () => {
  expect(browseKeyOfTab("database.console", { consoleId: "c1" })).toBeNull();
});
```

Extend the import to `import { browseKeyOfTab, consoleIdOfTab, useConsoleLifetime } from "./useConsoleLifetime";`

- [ ] **Step 2: Run it to make sure it fails**

Run: `cd frontend && bun test src/modules/database/useConsoleLifetime.test.ts`
Expected: FAIL — `browseKeyOfTab` is not exported, and `consoleIdOfTab` still answers for `database.table`.

- [ ] **Step 3: Split the two lifetimes**

In `useConsoleLifetime.ts`, change the table branch of `consoleIdOfTab` to return
null, and add its twin:

```ts
export function consoleIdOfTab(
  viewId: string,
  params?: Record<string, unknown>,
): string | null {
  if (viewId === "database.console") {
    return typeof params?.consoleId === "string" ? params.consoleId : DEFAULT_CONSOLE_ID;
  }
  // A table tab used to be a console with pre-filled SQL. It is a data grid
  // now, and owns a BrowseState instead — see browseKeyOfTab.
  return null;
}

/** The browse state a Database tab owns, or null when it owns none. */
export function browseKeyOfTab(
  viewId: string,
  params?: Record<string, unknown>,
): string | null {
  if (viewId !== "database.table") {
    return null;
  }
  return `${String(params?.schema ?? "")}.${String(params?.table ?? "")}`;
}

/**
 * Frees a table's browse state once its tab is really gone.
 *
 * The same rule as useConsoleLifetime, for the same reason: unmounting is not
 * closing, and a module WorkbenchViews unmounted has to come back with its
 * filter, its sort and its page intact.
 */
export function useBrowseLifetime(browseKey: string): void {
  const closeBrowse = useDatabaseStore((s) => s.closeBrowse);
  useEffect(() => {
    return () => {
      const open = moduleTabs(useWorkbenchTabsStore.getState().modules, "database").openTabs;
      const stillOpen = open.some((tab) => browseKeyOfTab(tab.viewId, tab.params) === browseKey);
      if (!stillOpen) {
        closeBrowse(browseKey);
      }
    };
  }, [browseKey, closeBrowse]);
}
```

- [ ] **Step 4: Call it from the table document**

In `TableDataView.tsx`, add below `useDatabaseChrome()`:

```tsx
  useBrowseLifetime(key);
```

importing it from `./useConsoleLifetime`.

- [ ] **Step 5: Verify**

Run: `cd frontend && bun test src/modules/database/useConsoleLifetime.test.ts && npx tsc --noEmit && bun test`
Expected: PASS, tsc clean, 0 fail.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/modules/database
git commit -m "fix(database): a table tab frees its rows, not a console it no longer owns"
```

---

## Task 12: Full verification

- [ ] **Step 1: Frontend**

Run: `cd frontend && npx tsc --noEmit && bun test && bun run build`
Expected: clean, 0 fail, budget passes.

- [ ] **Step 2: Cross-platform**

Run: `for os in darwin linux windows; do GOOS=$os GOARCH=amd64 go vet ./internal/database/ ./internal/app/; done`
Expected: no output. Nothing in this plan changes Go, so this is a regression check only.

- [ ] **Step 3: In the app, with MySQL running**

The reported failures came from a database that was not running — `connection_failed` and an empty grid. Start MySQL before this step, or none of it tests anything.

Check each, and report what you saw rather than that you "verified":
- opening a table shows rows immediately, with no SQL editor on screen
- the row-number gutter counts the table's rows, not the window's, when you scroll
- dragging a column edge resizes that column and leaves its neighbours alone
- clicking a header sorts server-side: the first row changes, not just the order of the 200 on screen
- Next reaches rows past 200, and Back returns to exactly the previous page
- a WHERE that matches nothing shows an empty grid, not an error
- a table with no primary key still pages, and shows the "unkeyed" note
- right-clicking a cell offers copy actions and names the right column

- [ ] **Step 4: Commit any fixes**

```bash
git add -A
git commit -m "fix(database): verification fallout"
```

- [ ] **Step 5: Check nothing leaks across tabs**

Open six tables, close them all, and confirm `useDatabaseStore.getState().browse`
is `{}` in the console. A page of 200 rows per table ever opened is the leak
Task 11 exists to prevent, and it is invisible until it is not.
