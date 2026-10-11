import type { CSSProperties, KeyboardEvent, ReactNode } from "react";
import type { LucideIcon } from "lucide-react";
import { lazy, Suspense, useMemo, useRef, useState } from "react";
import { Button } from "@/components/ui/button";
import { CellEditor } from "./CellEditor";
import { bulkEditRefusal } from "./bulkEditLimits";
import { validateCellValue, booleanValue, cellKind } from "../sql/cellValue";
import type { CellValue } from "../sql/updateCell";
import {
  DndContext,
  PointerSensor,
  closestCenter,
  useSensor,
  useSensors,
} from "@dnd-kit/core";
import { restrictToHorizontalAxis } from "@dnd-kit/modifiers";
import {
  SortableContext,
  horizontalListSortingStrategy,
  useSortable,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import {
  ArrowDown,
  ArrowUp,
  Braces,
  ChevronsUpDown,
  Columns3,
  Copy, Download,
  Eraser,
  Filter,
  FilterX,
  KeyRound,
  List,
  Pin,
  PinOff,
  Rows3,
} from "lucide-react";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuLabel,
  ContextMenuSeparator,
  ContextMenuTrigger,
} from "@/components/ui/context-menu";
import { EmptyState } from "@/components/ui/empty-state";
import { useVirtualRows } from "@/components/ui/virtual-rows";
import { chordLabel } from "@/lib/shortcut";
import { cn } from "@/lib/utils";
import { toast } from "sonner";
import { copyText } from "@/bridge/clipboard";
import type { QueryResult } from "@/bridge/database";
import {
  DEFAULT_COLUMN_WIDTH_PX,
  MIN_COLUMN_WIDTH_PX,
  ROW_NUMBER_WIDTH_PX,
  frozenOffsets,
  measureColumns,
  pinColumn,
  orderedColumns,
  reorderColumn,
  resizeColumn,
} from "./gridColumns";
// A4: moved here from QueryConsole rather than reimplemented. rowAsJSON maps
// over the ROW and names an empty column col_${i}; an inlined
// Object.fromEntries over `columns` is a different function with its own bugs.
import { columnValues, rowAsJSON, serializeSelection, selectedResult, type CopyFormat } from "./rowText";
import { DropdownMenu, DropdownMenuTrigger, DropdownMenuContent, DropdownMenuItem } from "@/components/ui/dropdown-menu";
import { useDatabaseStore } from "../databaseStore";
import { NativeSelect, NativeSelectOption } from "@/components/ui/native-select";
import { typeGlyph } from "./columnTypes";
import { numericColumns } from "./numericColumns";
import {
  HEADER_ROW,
  inRange,
  nextSelection,
  rangeSize,
  selectedRange,
  type GridSelection,
} from "./gridFocus";

// 28, not 36. A grid is read by scanning down it, and the row that matters is
// usually the one below the fold — four more of them on a 500px pane is worth
// more than the airiness. Read by useVirtualRows, so it has to match the `h-7`
// on the cells or the spacer rows drift out of step with the real ones.
const ROW_HEIGHT = 28;

// Enough to decide whether a value will be clipped, which is all the tooltip
// needs to know. The real measurement lives in gridColumns (CHAR_PX); this one
// runs per visible cell, so it stays arithmetic rather than a DOM read.
const CHAR_PX_APPROX = 7.3;

const BulkEditDialog = lazy(() =>
  import("./BulkEditDialog").then((m) => ({ default: m.BulkEditDialog })),
);

/** The header's own height, which the sticky row occupies above the first row. */
const HEADER_HEIGHT = 28;

/**
 * What the pointer was over when the menu opened.
 *
 * `row: null` is a column header — the menu still knows which column, which is
 * most of what it offers. Headers used to carry no target at all, so the menu
 * rendered every item behind `cell` and opened EMPTY on the one place anyone
 * right-clicks to act on a column.
 *
 * The row-number gutter resolves to the row's FIRST displayed column, so
 * right-clicking a row number offers the row rather than the "right-click a
 * cell" hint — which reads exactly like a right-click that did not register.
 */
type Target = { row: number | null; column: number } | null;

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
  onCopySelection = copyText,
  status, onExport,
  result,
  widths,
  columnOrder,
  onResize,
  onInspectCell,
  sort,
  onSort,
  onReorder,
  frozen = 0,
  onFreeze,
  columnTypes,
  columnNullable,
  engine,
  editScope,
  keyColumns,
  rowOffset = 0,
  onFilterByValue,
  onEditCells,
  onStageCells,
  changedCells,
  editRefusal = "",
}: {
  onCopySelection?: (text: string) => Promise<void>;
  status?: ReactNode;
  onExport?: (result: QueryResult) => void;
  result: QueryResult;
  widths: readonly number[];
  columnOrder: readonly number[];
  onResize: (widths: number[]) => void;
  onInspectCell: (cell: { columnName: string; value: string | null }) => void;
  /** Server-side sort, when the caller has one. Absent in the console. */
  sort?: { column: number; direction: "asc" | "desc" } | null;
  onSort?: (column: number) => void;
  /**
   * Accepts a new column order. Absent in the console, whose columns change
   * with every query, so an order saved against the last one describes columns
   * this one may not have.
   */
  onReorder?: (order: number[]) => void;
  /**
   * How many columns of the DISPLAY order stay on screen while the grid
   * scrolls sideways. A count, not a set — see frozenOffsets.
   */
  frozen?: number;
  /**
   * Absent in the console, whose columns change with every query. Takes the
   * whole arrangement, because pinning MOVES a column — see pinColumn.
   */
  onFreeze?: (next: { order: number[]; frozen: number }) => void;
  /**
   * Column name → datatype, for the header glyphs. Absent in the console,
   * whose columns can be expressions: a catalogue type for `count(*)` does not
   * exist, and the neutral glyph is the honest answer rather than a guess.
   */
  columnTypes?: Readonly<Record<string, string>>;
  columnNullable?: Readonly<Record<string, boolean>>;
  engine?: string;
  /** A new load invalidates positional selection/editors, unlike a staged overlay. */
  editScope?: string;
  /** The columns of the table's key, drawn with a key glyph instead of a type. */
  keyColumns?: readonly string[];
  /**
   * The number of the first row on this page. The gutter counts through the
   * table, not through the page — "row 3" of page 8 is not row 3.
   */
  rowOffset?: number;
  /**
   * Offered in the cell menu as "Filter by this value". The grid hands up the
   * column and the value and nothing else: quoting them into SQL needs the
   * engine, which is the caller's to know.
   */
  /** `value` is null for a NULL cell, which filters with IS NULL (#47). */
  onFilterByValue?: (filter: {
    column: string;
    value: string | null;
    negate: boolean;
  }) => void;
  /**
   * Commits an edit. Absent means the grid is a reader — the console's result
   * can be a join or an expression, and there is no single table to write to.
   *
   * Row and column INDICES, not values: the caller owns the key columns and
   * the statement, and the grid owns only which cells the user picked.
   */
  onEditCells?: (edit: {
    rows: number[];
    columns: number[];
    value: string | null;
  }) => void;
  /** Multiple edits are always staged, including on non-production connections. */
  onStageCells?: (
    cells: readonly { row: number; column: number; value: CellValue }[],
  ) => void;
  /** #52: cells holding a staged change, as "row:column", drawn as changed. */
  changedCells?: ReadonlySet<string>;
  /** Why editing is refused, shown where the attempt is made. "" means it is not. */
  editRefusal?: string;
}) {
  // Decided from the values, because a query result carries no types. Once
  // per result rather than per cell: a 200-row sample across 74 columns is
  // cheap, and doing it inside the cell would do it again on every scroll.
  const numeric = useMemo(
    () => numericColumns(result.rows, result.columns.length),
    [result.rows, result.columns.length],
  );

  const rows = useVirtualRows(result.rows.length, ROW_HEIGHT);
  const order = orderedColumns(result.columns, columnOrder);
  // What the content needs, for every column nobody has dragged. Recomputed
  // per result, not per render: it walks fifty rows.
  const measured = useMemo(
    () => measureColumns(result.columns, result.rows),
    [result.columns, result.rows],
  );
  const [target, setTarget] = useState<Target>(null);
  /**
   * The focused cell, as {row, position}: a row index and a DISPLAY position,
   * not a column index — arrow keys walk what is on screen, and `order` is
   * what says which column that is. `row: HEADER_ROW` is the header, which is
   * how sorting stays reachable without giving 74 header buttons their own tab
   * stop each (keyboards.md › the Tab table: Tab moves through CONTROLS, and a
   * grid is one control).
   */
  const [selection, setSelection] = useState<GridSelection | null>(null);
  /** The active editor position. Its draft stays inside CellEditor. */
  const [editing, setEditing] = useState<{
    row: number;
    position: number;
  } | null>(null);
  const [bulkRows, setBulkRows] = useState<number[] | null>(null);
  const [scope, setScope] = useState(editScope);
  if (scope !== editScope) {
    setScope(editScope);
    setEditing(null);
    setSelection(null);
    setBulkRows(null);
    setTarget(null);
  }
  const focus = selection?.focus ?? null;
  const range = selection ? selectedRange(selection) : null;

  // A click is a sort and a drag is a reorder, and they start with the same
  // pointerdown — four pixels is what tells them apart. Without this the sort
  // never fires and the resize handle reorders instead of resizing.
  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 4 } }),
  );

  const widthOf = (index: number) =>
    widths[index] ?? measured[index] ?? DEFAULT_COLUMN_WIDTH_PX;
  // A drag starts from what is on screen, not from a default: seeding the
  // untouched columns with 180 made dragging the fourth column snap the first
  // three to a width nobody chose.
  const effectiveWidths = result.columns.map((_, index) => widthOf(index));
  // Frozen columns are sticky at an absolute offset — the widths of the frozen
  // columns before them. Indexed by DISPLAY position, which is what `order`
  // walks, not by the column's own index.
  // From the gutter's right edge: the row numbers are sticky at 0, so a frozen
  // column starting there would paint underneath them.
  const offsets = frozenOffsets(order, widthOf, frozen, ROW_NUMBER_WIDTH_PX);
  const frozenStyle = (position: number): CSSProperties | undefined =>
    offsets[position] === undefined
      ? undefined
      : {
          position: "sticky",
          left: offsets[position],
          zIndex: 2,
          // A pinned cell needs an OPAQUE ground: columns slide under it, and
          // `bg-surface-well` is rgba(255,255,255,0.06) — a tint meant to sit
          // on a surface, not to be one. Painted straight onto a pinned cell
          // it veiled the scrolling columns instead of hiding them, which is
          // what "the pinned column is transparent" was.
          //
          // The tint as a gradient layer over the base reproduces exactly what
          // the grid body already shows — the pinned run stays the colour it
          // had, and is opaque.
          background:
            "linear-gradient(var(--surface-well), var(--surface-well)), var(--surface-base)",
        };
  /**
   * Brings a cell into view, both axes, without animating.
   *
   * Rows are a fixed height, so the row's top is arithmetic rather than a DOM
   * read — which matters because the row being moved to is usually not
   * rendered yet: the virtualiser only mounts a window, and scrolling is what
   * makes the target exist. Columns are measured the same way the frozen
   * offsets are, from the widths the grid already has.
   */
  function revealCell(row: number, position: number): void {
    const scroller = rows.scrollRef.current;
    if (!scroller) return;

    if (row >= 0) {
      const top = row * ROW_HEIGHT;
      // The sticky header covers the first HEADER_HEIGHT of the scrollport, so
      // a row scrolled flush to the top would be under it.
      if (top < scroller.scrollTop + HEADER_HEIGHT) {
        scroller.scrollTop = Math.max(0, top - HEADER_HEIGHT);
      } else if (
        top + ROW_HEIGHT >
        scroller.scrollTop + scroller.clientHeight
      ) {
        scroller.scrollTop = top + ROW_HEIGHT - scroller.clientHeight;
      }
    }

    // A frozen column is on screen by definition — scrolling to it would
    // scroll the grid back to the left edge for nothing.
    if (position < frozen) return;
    let left = ROW_NUMBER_WIDTH_PX;
    for (let i = 0; i < position; i += 1) left += widthOf(order[i]);
    const width = widthOf(order[position]);
    // Everything pinned stays painted over the left edge, so that band is not
    // free space to scroll a cell into.
    const pinned = offsets.reduce<number>(
      (sum, offset, i) =>
        offset === undefined ? sum : sum + widthOf(order[i]),
      ROW_NUMBER_WIDTH_PX,
    );
    if (left < scroller.scrollLeft + pinned) {
      scroller.scrollLeft = Math.max(0, left - pinned);
    } else if (left + width > scroller.scrollLeft + scroller.clientWidth) {
      scroller.scrollLeft = left + width - scroller.clientWidth;
    }
  }

  /**
   * The grid is one tab stop and the arrows move inside it.
   *
   * Before this, Tab walked 74 header buttons in a row and never reached a
   * cell at all — the body cells are `<td>`s with a click handler, which the
   * keyboard cannot reach. Full Keyboard Access is the thing this restores
   * (accessibility.md › Mobility). Where a key LANDS is gridFocus; this is the
   * part that cannot be pure.
   */
  /** Puts the cell under the keyboard into edit mode, or says why not. */
  function beginEdit(row: number, position: number): void {
    if (!onEditCells || row < 0) return;
    if (editRefusal) {
      toast.error("This result cannot be edited", { description: editRefusal });
      return;
    }
    setEditing({ row, position });
  }

  /**
   * Applies the draft to every cell of the selection.
   *
   * The whole rectangle, not the one cell being typed into: that is what makes
   * this a bulk edit, and it is the DataGrip gesture — pick a run of rows, type
   * once. The caller stages the cells and builds one UPDATE per affected row.
   */
  function commitEdit(value: CellValue): string {
    const target =
      range ??
      (editing
        ? {
            top: editing.row,
            bottom: editing.row,
            left: editing.position,
            right: editing.position,
          }
        : null);
    if (!onEditCells || !target || target.top < 0)
      return "Select a data cell to edit.";
    if (editRefusal) return editRefusal;
    const limit = bulkEditRefusal(
      target.bottom - target.top + 1,
      Array.from({ length: target.right - target.left + 1 }, () => value),
    );
    if (limit) return limit;
    const edits: { row: number; column: number; value: CellValue }[] = [];
    const selectedRows: number[] = [],
      selectedColumns: number[] = [];
    for (let position = target.left; position <= target.right; position++) {
      const column = order[position],
        name = result.columns[column];
      const error = validateCellValue(
        value,
        columnTypes?.[name],
        columnNullable?.[name] !== false,
        engine,
      );
      if (error) return `${name}: ${error}`;
      selectedColumns.push(column);
    }
    for (let row = target.top; row <= target.bottom; row++) {
      if (!result.rows[row])
        return "The selected row is no longer on this page.";
      selectedRows.push(row);
      for (const column of selectedColumns)
        edits.push({
          row,
          column,
          value:
            value !== null &&
            cellKind(columnTypes?.[result.columns[column]]) === "boolean"
              ? booleanValue(value)
              : value,
        });
    }
    if (edits.length > 1 && onStageCells) onStageCells(edits);
    else
      onEditCells({
        rows: selectedRows,
        columns: selectedColumns,
        value: edits[0].value,
      });
    setEditing(null);
    return "";
  }
  function openBulk() {
    if (!range || range.top < 0 || !onStageCells || editRefusal) return;
    const selected: number[] = [];
    for (let row = range.top; row <= range.bottom; row++)
      if (result.rows[row]) selected.push(row);
    if (selected.length) setBulkRows(selected);
  }

  const copyFormat = useDatabaseStore((state) => state.copyFormat);
  const setCopyFormat = useDatabaseStore((state) => state.setCopyFormat);
  const selectionText = (selected: NonNullable<typeof range>) => serializeSelection(result, order, selected, copyFormat);
  const copySelection = () => {
    if (range && range.top >= 0 && result.rows.length) {
      void onCopySelection(selectionText(range)).catch(() => toast.error("Could not copy selection"));
    }
  };

  function onGridKeyDown(event: KeyboardEvent<HTMLTableElement>): void {
    const here = selection ?? {
      anchor: { row: 0, position: 0 },
      focus: { row: 0, position: 0 },
    };

    if (editing) {
      // While typing, the grid's own keys belong to the input — except the two
      // that end the edit, which the input cannot handle alone because the
      // commit spans cells it does not know about.
      return;
    }
    if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "c" && selection && selection.focus.row !== HEADER_ROW && result.rows.length > 0) {
      event.preventDefault(); event.stopPropagation();
      void onCopySelection(selectionText(selectedRange(selection))).catch(() => toast.error("Could not copy selection"));
      return;
    }
    if (event.shiftKey && event.key === " " && here.focus.row !== HEADER_ROW) {
      event.preventDefault();
      setSelection({ anchor: { row: Math.max(0, here.anchor.row), position: 0 }, focus: { row: here.focus.row, position: order.length - 1 } });
      return;
    }
    if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "a") {
      event.preventDefault(); event.stopPropagation();
      if (result.rows.length && order.length) setSelection({ anchor: { row: 0, position: 0 }, focus: { row: result.rows.length - 1, position: order.length - 1 } });
      return;
    }
    if (event.key === "Escape") {
      setSelection(null);
      event.currentTarget.blur();
      return;
    }
    if (event.key === "Enter" || event.key === " " || event.key === "F2") {
      if (here.focus.row === HEADER_ROW) {
        onSort?.(order[here.focus.position]);
      } else if (onEditCells) {
        beginEdit(here.focus.row, here.focus.position);
      } else {
        const columnIndex = order[here.focus.position];
        onInspectCell({
          columnName: result.columns[columnIndex] || `col_${columnIndex}`,
          value: result.rows[here.focus.row]?.[columnIndex] ?? null,
        });
      }
      event.preventDefault();
      return;
    }

    const next = nextSelection(
      here,
      event.key,
      {
        rows: result.rows.length,
        columns: order.length,
        // One row of overlap, so a page turn keeps a line of context.
        page: Math.max(
          1,
          Math.floor((rows.scrollRef.current?.clientHeight ?? 0) / ROW_HEIGHT) -
            1,
        ),
        jump: event.metaKey || event.ctrlKey,
      },
      event.shiftKey,
    );
    // null is a key this grid does not handle: Tab still has to leave, and a
    // bare character still has to reach the shell.
    if (!next) return;
    setSelection(next);
    revealCell(next.focus.row, next.focus.position);
    event.preventDefault();
  }

  // Two targets, because they answer different questions. `column` is known
  // for a header as well as a cell; `cell` needs a row under the pointer.
  const column =
    target && result.columns[target.column] !== undefined
      ? {
          index: target.column,
          name: result.columns[target.column] || `col_${target.column}`,
          position: order.indexOf(target.column),
        }
      : null;
  const cell =
    target && target.row !== null && result.rows[target.row]
      ? {
          value: result.rows[target.row][target.column] ?? null,
          row: result.rows[target.row],
        }
      : null;

  return (
    // The DndContext sits outside the table, not around the header row: it
    // renders a live region and a hidden description div, and a `<div>` is not
    // a `<tr>`. SortableContext renders no DOM of its own, so that one can sit
    // where the header cells are.
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
      <div className="flex shrink-0 flex-wrap items-center gap-2 border-b border-border-subtle px-3 py-1.5 text-body-12 text-text-secondary" aria-label="Copy options">
        <div className="mr-auto flex min-w-0 flex-wrap items-center gap-2">{status}</div>
        <label className="flex items-center gap-2">
          Copy as
          <NativeSelect size="sm" aria-label="Copy format" value={copyFormat} onChange={(event) => setCopyFormat(event.target.value as CopyFormat)}>
            <NativeSelectOption value="tsv">TSV</NativeSelectOption>
            <NativeSelectOption value="csv">CSV</NativeSelectOption>
            <NativeSelectOption value="json">JSON</NativeSelectOption>
          </NativeSelect>
        </label>
        <Button size="xs" variant="ghost" disabled={!range || range.top < 0 || !result.rows.length} onClick={copySelection} title={chordLabel({ mod: true, key: "c" })}>
          <Copy className="size-3.5" /> Copy selection
        </Button>
        {onExport && <DropdownMenu><DropdownMenuTrigger asChild><Button size="xs" variant="ghost"><Download className="size-3.5"/> Export</Button></DropdownMenuTrigger>
          <DropdownMenuContent align="end"><DropdownMenuItem onSelect={() => onExport(result)}>Export loaded result…</DropdownMenuItem><DropdownMenuItem disabled={!range || range.top < 0} onSelect={() => {if(range && range.top >= 0) onExport(selectedResult(result,order,range));}}>Export selection…</DropdownMenuItem></DropdownMenuContent>
        </DropdownMenu>}
      </div>
      {onStageCells ? (
        <div
          className="flex shrink-0 flex-wrap items-center gap-2 py-1 text-body-12 text-text-secondary"
          aria-label="Table selection actions"
        >
          <span className="mr-auto tabular-nums">
            {range && range.top >= 0
              ? `${range.bottom - range.top + 1} ${range.bottom === range.top ? "row" : "rows"} · ${rangeSize(range)} ${rangeSize(range) === 1 ? "cell" : "cells"} selected`
              : "Select cells or row numbers; Shift extends selection."}
          </span>
          <Button
            size="xs"
            variant="ghost"
            disabled={!result.rows.length || Boolean(editRefusal)}
            onClick={() =>
              setSelection({
                anchor: { row: 0, position: 0 },
                focus: {
                  row: result.rows.length - 1,
                  position: order.length - 1,
                },
              })
            }
          >
            Select loaded page
          </Button>
          <Button
            size="xs"
            variant="ghost"
            disabled={!selection}
            onClick={() => setSelection(null)}
          >
            Clear selection
          </Button>
          <Button
            size="xs"
            variant="outline"
            disabled={!range || range.top < 0 || Boolean(editRefusal)}
            onClick={openBulk}
          >
            Edit selected rows…
          </Button>
        </div>
      ) : null}
      <ContextMenu>
        <ContextMenuTrigger asChild>
          <div
            ref={rows.scrollRef}
            className="min-h-0 flex-1 overflow-auto scrollbar-gutter-stable bg-surface-base"
            onContextMenu={(event) => {
              const el = (event.target as HTMLElement).closest(
                "[data-cell],[data-column],[data-row]",
              );
              const raw = el?.getAttribute("data-cell");
              if (raw) {
                const [row, column] = raw.split(":").map(Number);
                setTarget(Number.isNaN(row) ? null : { row, column });
                const position = order.indexOf(column);
                if (position >= 0 && (!range || !inRange(range, row, position)))
                  setSelection({
                    anchor: { row, position },
                    focus: { row, position },
                  });
                return;
              }
              const gutter = Number(el?.getAttribute("data-row"));
              if (!Number.isNaN(gutter) && el?.hasAttribute("data-row")) {
                setTarget({ row: gutter, column: order[0] ?? 0 });
                if (!range || gutter < range.top || gutter > range.bottom)
                  setSelection({
                    anchor: { row: gutter, position: 0 },
                    focus: { row: gutter, position: order.length - 1 },
                  });
                return;
              }
              const column = Number(el?.getAttribute("data-column"));
              setTarget(Number.isNaN(column) ? null : { row: null, column });
            }}
          >
            <table
              className="w-full table-fixed border-collapse text-left text-code-12 outline-none focus-visible:ring-1 focus-visible:ring-status-info"
              aria-label="Query results"
              // A grid, not a static table: its cells are interactive and one
              // of them holds the focus at a time.
              role="grid"
              tabIndex={0}
              aria-activedescendant={focus ? "grid-focus" : undefined}
              onKeyDown={onGridKeyDown}
              onCopy={(event) => {
                // WebKit may dispatch the native Edit → Copy action without a
                // keydown. Editors keep their own text selection/clipboard.
                if (editing || !selection || selection.focus.row < 0 || !result.rows.length) return;
                event.preventDefault(); event.stopPropagation();
                const text = selectionText(selectedRange(selection));
                event.clipboardData.setData("text/plain", text);
                void onCopySelection(text).catch(() => toast.error("Could not copy selection"));
              }}
              onFocus={() =>
                setSelection(
                  (current) =>
                    current ?? {
                      anchor: { row: 0, position: 0 },
                      focus: { row: 0, position: 0 },
                    },
                )
              }
            >
              <colgroup>
                <col style={{ width: ROW_NUMBER_WIDTH_PX }} />
                {order.map((index) => (
                  <col key={index} style={{ width: widthOf(index) }} />
                ))}
              </colgroup>
              {/* The background lives on the cells, not here. A `position:
                  sticky` row group moves its CELLS and leaves a background set
                  on the group behind at the scroll origin, so a header styled
                  this way is transparent the moment you scroll — and the first
                  row reads straight through it. */}
              <thead className="sticky top-0 z-10">
                <tr>
                  {/* The corner. Sticky on both axes and above the frozen
                      columns, which slide under it. */}
                  <th
                    aria-label="Row"
                    style={{ position: "sticky", left: 0, zIndex: 3 }}
                    className="h-7 border-r border-b border-border-subtle bg-surface-raised p-0"
                  ><button type="button" aria-label="Select all rows on this page" title="Select all rows on this page" disabled={!result.rows.length || !order.length} onClick={(event) => { setSelection({ anchor: { row: 0, position: 0 }, focus: { row: result.rows.length - 1, position: order.length - 1 } }); event.currentTarget.closest("table")?.focus(); }} className="flex h-full w-full items-center justify-center text-text-tertiary hover:bg-state-soft hover:text-text-primary"><Rows3 className="size-3" /></button></th>
                  <SortableContext
                    items={order}
                    strategy={horizontalListSortingStrategy}
                  >
                    {order.map((index, position) => (
                      <HeaderCell
                        key={`${result.columns[index]}-${index}`}
                        label={result.columns[index]}
                        index={index}
                        sorted={sort?.column === index ? sort.direction : null}
                        frozenAt={offsets[position]}
                        onSort={onSort}
                        focused={
                          focus?.row === HEADER_ROW &&
                          focus.position === position
                        }
                        glyph={
                          keyColumns?.includes(result.columns[index])
                            ? KeyRound
                            : typeGlyph(columnTypes?.[result.columns[index]])
                        }
                        isKey={!!keyColumns?.includes(result.columns[index])}
                        onResize={(column, delta) =>
                          onResize(resizeColumn(effectiveWidths, column, delta))
                        }
                      />
                    ))}
                  </SortableContext>
                </tr>
              </thead>
              <tbody>
                {rows.padTop > 0 && (
                  <tr style={{ height: rows.padTop }} aria-hidden />
                )}
                {rows.indices.map((rowIndex) => (
                  <tr
                    key={rowIndex}
                    aria-selected={Boolean(range && rowIndex >= range.top && rowIndex <= range.bottom && range.left === 0 && range.right === order.length - 1)}
                    className={cn(
                      "group transition-colors hover:bg-state-soft",
                      // lists-and-tables.md › Desktop: "Consider using
                      // alternating row colors in a multicolumn table…
                      // especially in a wide table." A 74-column table is read
                      // by tracking one row across a screen and a half, and
                      // hover only ever helps the row under the pointer.
                      // Indexed by the ROW, so the banding does not crawl as
                      // virtualised rows come and go.
                      // 40%, so hovering an odd row is still a visible step up
                      // to the full state layer rather than a repeat of it.
                      rowIndex % 2 === 1 && "bg-state-soft/40",
                    )}
                  >
                    {/* The row number, counting through the table rather than
                        through the page. Sticky like the corner above it, and
                        on the same opaque ground the pinned cells use — the
                        scrolling columns pass beneath it. */}
                    <td
                      onMouseDown={(event) => {
                        if (event.button !== 0) return;
                        event.preventDefault();
                        window.getSelection()?.removeAllRanges();
                        setSelection((current) => ({
                          anchor: {
                            row:
                              event.shiftKey && current && current.anchor.row >= 0
                                ? current.anchor.row
                                : rowIndex,
                            position: 0,
                          },
                          focus: { row: rowIndex, position: order.length - 1 },
                        }));
                        event.currentTarget.closest("table")?.focus();
                      }}
                      data-row={rowIndex}
                      title={`Click to select this row · Shift-click to select a range · ${chordLabel({ mod: true, key: "c" })} to copy`}
                      style={{
                        position: "sticky",
                        left: 0,
                        zIndex: 3,
                        background:
                          "linear-gradient(var(--surface-well), var(--surface-well)), var(--surface-base)",
                      }}
                      className="h-7 border-r border-b border-border-subtle px-2 text-right tabular-nums text-code-11 text-text-tertiary select-none group-hover:bg-state-soft-on-well"
                    >
                      {rowOffset + rowIndex + 1}
                    </td>
                    {/* Only the header drags. Giving each cell a useSortable would
                        mount one per visible cell — 2,220 on a 74-column table — to
                        animate a column that snaps into place on drop anyway. */}
                    {order.map((columnIndex, position) => {
                      const value = result.rows[rowIndex][columnIndex] ?? null;
                      const isFocused =
                        focus?.row === rowIndex && focus.position === position;
                      const isSelected = range
                        ? inRange(range, rowIndex, position)
                        : false;
                      const isEditing =
                        editing?.row === rowIndex &&
                        editing.position === position;
                      return (
                        <td
                          key={columnIndex}
                          data-cell={`${rowIndex}:${columnIndex}`}
                          // Only the focused cell is named. An id on all 2,220
                          // is 2,220 attributes written per render for one
                          // pointer aria-activedescendant needs.
                          id={isFocused ? "grid-focus" : undefined}
                          aria-selected={isSelected || undefined}
                          // Shift extends from wherever the selection started,
                          // the same as Shift+arrow. mousedown, not click, so
                          // the browser's own text selection does not start
                          // inside a drag the grid is about to own.
                          onMouseDown={(event) => {
                            if (event.shiftKey) {
                              // Cells keep `select-text` so a value can be
                              // copied out of one by hand — but a Shift-click
                              // is the grid extending its rectangle, and left
                              // to itself the browser ALSO dragged a text
                              // selection across every cell between, painting
                              // the range twice in two different colours.
                              event.preventDefault();
                              window.getSelection()?.removeAllRanges();
                            }
                            setSelection((current) => ({
                              anchor:
                                event.shiftKey && current
                                  ? current.anchor
                                  : { row: rowIndex, position },
                              focus: { row: rowIndex, position },
                            }));
                          }}
                          onDoubleClick={() =>
                            onEditCells
                              ? beginEdit(rowIndex, position)
                              : onInspectCell({
                                  columnName:
                                    result.columns[columnIndex] ||
                                    `col_${columnIndex}`,
                                  value,
                                })
                          }
                          style={frozenStyle(position)}
                          className={cn(
                            "h-7 cursor-pointer truncate border-r border-b border-border-subtle px-3 select-text last:border-r-0 hover:text-text-primary",
                            changedCells?.has(`${rowIndex}:${columnIndex}`) &&
                              "bg-status-warning/10",
                            // A number goes right, and in figures of one
                            // width: that is what makes a column of ids
                            // scannable — the four-digit one is visibly
                            // longer instead of being one character further
                            // along a ragged right edge.
                            numeric[columnIndex] && "text-right tabular-nums",
                            isSelected &&
                              !isFocused &&
                              "bg-(--feedback-selected)",
                            isFocused &&
                              "outline outline-1 -outline-offset-1 outline-status-info",
                            "text-text-primary",
                            // The row's hover tint has to reach a pinned cell
                            // too, or half the row lights up — and it has to
                            // stack ON the ground frozenStyle painted rather
                            // than replace it, or hovering makes the cell
                            // see-through again.
                            offsets[position] !== undefined &&
                              "group-hover:bg-state-soft-on-well",
                          )}
                          // The full value, and only when the cell is too
                          // narrow to show it. "Click to inspect cell" on
                          // every cell meant scanning forty rows raised four
                          // hundred tooltips saying the same thing.
                          title={
                            value !== null &&
                            value.length * CHAR_PX_APPROX > widthOf(columnIndex)
                              ? value
                              : undefined
                          }
                        >
                          {isEditing ? (
                            <CellEditor
                              initialValue={value}
                              column={result.columns[columnIndex]}
                              type={columnTypes?.[result.columns[columnIndex]]}
                              nullable={
                                columnNullable?.[
                                  result.columns[columnIndex]
                                ] !== false
                              }
                              cells={range ? rangeSize(range) : 1}
                              onCommit={commitEdit}
                              onCancel={() => setEditing(null)}
                            />
                          ) : value === null ? (
                            /* The absence of a value, dressed as an absence.
                               It was NULL in the warning colour — four
                               saturated letters repeated down a column of
                               empty cells, louder than the data they were
                               standing in for, and warning of nothing. */
                            <span className="text-code-11 text-text-tertiary italic">
                              null
                            </span>
                          ) : cellKind(
                              columnTypes?.[result.columns[columnIndex]],
                            ) === "boolean" ? (
                            booleanValue(value)
                          ) : (
                            value
                          )}
                        </td>
                      );
                    })}
                  </tr>
                ))}
                {rows.padBottom > 0 && (
                  <tr style={{ height: rows.padBottom }} aria-hidden />
                )}
              </tbody>
            </table>
            {/* A result with columns but no rows used to render as the header
                row over blank space, which reads as a grid still loading. The
                headers stay — they are the answer to "what would have been
                here" — and the shared Empty says the rest. */}
            {result.rows.length === 0 ? (
              // sticky left-0: this sits inside the horizontal scroller, so
              // without it the message slides out of view as you scroll a wide
              // table sideways. Sticky pins it to the scroller's own left edge,
              // where its container width centres it over what you can see.
              <EmptyState
                size="rail"
                className="sticky left-0 w-full py-8"
                icon={Rows3}
                title="No rows"
                body="The query ran and matched nothing. Change the filter or the sort and run it again."
              />
            ) : null}
          </div>
        </ContextMenuTrigger>
        <ContextMenuContent>
          {range && range.top >= 0 && result.rows.length > 0 && <ContextMenuItem icon={<Copy />} shortcut={chordLabel({ mod: true, key: "c" })} onSelect={() => void onCopySelection(selectionText(range)).catch(() => toast.error("Could not copy selection"))}>Copy selection</ContextMenuItem>}
          {cell ? (
            <>
              <ContextMenuItem
                icon={<Copy />}
                onSelect={() => void copyText(cell.value ?? "")}
              >
                Copy cell
              </ContextMenuItem>
              <ContextMenuItem
                icon={<Braces />}
                onSelect={() =>
                  void copyText(rowAsJSON(result.columns, cell.row))
                }
              >
                Copy row as JSON
              </ContextMenuItem>
              {onFilterByValue && column ? (
                <>
                  <ContextMenuItem
                    icon={<Filter />}
                    onSelect={() =>
                      onFilterByValue({
                        column: column.name,
                        value: cell.value,
                        negate: false,
                      })
                    }
                  >
                    Filter by this value
                  </ContextMenuItem>
                  <ContextMenuItem
                    icon={<FilterX />}
                    onSelect={() =>
                      onFilterByValue({
                        column: column.name,
                        value: cell.value,
                        negate: true,
                      })
                    }
                  >
                    Exclude this value
                  </ContextMenuItem>
                </>
              ) : null}
            </>
          ) : null}
          {column ? (
            <>
              <ContextMenuItem
                icon={<Columns3 />}
                onSelect={() => void copyText(column.name)}
              >
                Copy column name
              </ContextMenuItem>
              <ContextMenuItem
                icon={<List />}
                onSelect={() =>
                  void copyText(columnValues(result.rows, column.index))
                }
              >
                Copy {result.rows.length} value
                {result.rows.length === 1 ? "" : "s"}
              </ContextMenuItem>
              {/* The count is what is loaded, not what the query would have
                  returned. Offering "the column" over a capped result would hand
                  someone 200 rows of an unknown total. */}
              {result.truncated ? (
                <ContextMenuLabel className="text-caption-11 font-normal text-status-warning">
                  result was capped — this is not the whole column
                </ContextMenuLabel>
              ) : null}
              {onEditCells && cell ? (
                <>
                  <ContextMenuSeparator />
                  {onStageCells ? (
                    <ContextMenuItem
                      disabled={Boolean(editRefusal)}
                      onSelect={openBulk}
                    >
                      Edit selected rows…
                    </ContextMenuItem>
                  ) : null}
                  <ContextMenuItem
                    disabled={Boolean(editRefusal)}
                    onSelect={() => {
                      if (target?.row !== null && target)
                        beginEdit(target.row, order.indexOf(target.column));
                    }}
                  >
                    Edit value…
                  </ContextMenuItem>
                  {/* A value, not the four letters — typing NULL into a cell
                      sets the string, which is why it needs an item of its
                      own. It follows the SELECTION, like any other edit. */}
                  <ContextMenuItem
                    icon={<Eraser />}
                    disabled={Boolean(editRefusal)}
                    onSelect={() => {
                      const error = commitEdit(null);
                      if (error)
                        toast.error("Could not set NULL", {
                          description: error,
                        });
                    }}
                  >
                    Set{" "}
                    {range && rangeSize(range) > 1
                      ? `${rangeSize(range)} cells`
                      : "cell"}{" "}
                    to NULL
                  </ContextMenuItem>
                </>
              ) : null}
              {onFreeze ? (
                <ContextMenuItem
                  icon={column.position < frozen ? <PinOff /> : <Pin />}
                  onSelect={() =>
                    onFreeze(pinColumn(order, frozen, column.index))
                  }
                >
                  {column.position < frozen
                    ? `Unpin ${column.name}`
                    : `Pin ${column.name}`}
                </ContextMenuItem>
              ) : null}
            </>
          ) : (
            // Never an empty menu. A popup that opens onto nothing is
            // indistinguishable from a right-click that did not register, and
            // that is exactly how this read when headers carried no target.
            <ContextMenuLabel className="font-normal text-text-tertiary">
              Right-click a cell or a column header
            </ContextMenuLabel>
          )}
        </ContextMenuContent>
      </ContextMenu>
      <div className="flex h-6 shrink-0 items-center gap-3 border-t border-border-subtle px-3 text-code-11 text-text-tertiary" aria-label="Grid selection status">
        {range && range.top >= 0 ? `${range.bottom - range.top + 1} ${range.bottom === range.top ? "row" : "rows"} · ${rangeSize(range)} ${rangeSize(range) === 1 ? "cell" : "cells"} selected` : `${result.rows.length} loaded rows · ${result.columns.length} columns`}
      </div>
      {bulkRows && onStageCells ? (
        <Suspense fallback={null}>
          <BulkEditDialog
            rows={bulkRows}
            columns={result.columns}
            columnTypes={columnTypes}
            columnNullable={columnNullable}
            keyColumns={keyColumns}
            engine={engine}
            onClose={() => setBulkRows(null)}
            onRestoreFocus={() =>
              rows.scrollRef.current
                ?.querySelector<HTMLElement>('[role="grid"]')
                ?.focus()
            }
            onStage={(assignments) => {
              if (editRefusal) return editRefusal;
              const limit = bulkEditRefusal(
                bulkRows.length,
                assignments.map((a) => a.value),
              );
              if (limit) return limit;
              const edits = bulkRows.flatMap((row) =>
                assignments.map((a) => ({
                  row,
                  column: a.column,
                  value: a.value,
                })),
              );
              if (edits.some((e) => !result.rows[e.row]))
                return "Selection changed. Select rows again.";
              onStageCells(edits);
              return "";
            }}
          />
        </Suspense>
      ) : null}
    </DndContext>
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
  frozenAt,
  onSort,
  onResize,
  glyph: Glyph,
  isKey,
  focused,
}: {
  label: string;
  index: number;
  sorted: "asc" | "desc" | null;
  /** Sticky left offset when this column is frozen, undefined when it scrolls. */
  frozenAt: number | undefined;
  onSort?: (index: number) => void;
  onResize: (index: number, delta: number) => void;
  /** The grid's keyboard focus is on this header. */
  focused: boolean;
  /** The datatype bucket, or a key — see typeGlyph. */
  glyph: LucideIcon;
  isKey: boolean;
}) {
  const dragStart = useRef<number | null>(null);
  const { listeners, setNodeRef, transform, transition, isDragging } =
    useSortable({
      id: index,
    });

  // `listeners` go on the `<th>`, not on the sort button: a drag handle nested
  // inside a click target is the same fight over the pointer the resize handle
  // already avoids by being a sibling.
  //
  // A26: useSortable's `attributes` are deliberately NOT spread. They set
  // role="button" and tabIndex=0, which override the cell's implicit
  // columnheader role — costing the grid its column semantics — and nest the
  // real sort <button> inside a role="button" element, which is invalid ARIA.
  // What they buy is dnd-kit's KEYBOARD drag, and this grid registers a
  // PointerSensor and nothing else, so they bought nothing that could happen.
  // If you add a KeyboardSensor, the attributes come back with it — and the
  // sort button has to stop being a child of the drag target at the same time.

  const style: CSSProperties = {
    // relative + transform, never out of flow: a `<th>` taken out of flow takes
    // the table's column widths with it, which is the same reason virtual-rows
    // pads with spacer rows instead of positioning them.
    //
    // A frozen column is the exception, and sticky is why it can be: it keeps
    // its place in the table's layout and only paints elsewhere. It has to
    // out-stack the scrolling headers beside it, which sit at 0/1.
    position: frozenAt === undefined ? "relative" : "sticky",
    left: frozenAt,
    // Opaque for the same reason the body cells are, on the header's surface.
    background: frozenAt === undefined ? undefined : "var(--surface-raised)",
    transform: CSS.Translate.toString(transform),
    transition,
    zIndex: isDragging ? 3 : frozenAt === undefined ? 0 : 2,
    opacity: isDragging ? 0.8 : 1,
  };

  return (
    <th
      ref={setNodeRef}
      style={style}
      // The grid's one context menu reads this to learn which column the
      // pointer was over. A header carries no row, so it is data-column rather
      // than data-cell — see Target.
      data-column={index}
      {...listeners}
      className="group/th relative h-7 border-r border-b border-border-subtle bg-surface-raised p-0 text-code-12-medium text-text-primary last:border-r-0"
    >
      <button
        type="button"
        // Out of the tab order on purpose: the grid is one tab stop and the
        // arrows move within it. Tab used to walk 74 of these in a row without
        // ever reaching a cell.
        tabIndex={-1}
        className={cn(
          "flex h-full w-full items-center gap-1.5 px-3 text-left disabled:cursor-default",
          focused && "outline outline-1 -outline-offset-1 outline-status-info",
        )}
        disabled={!onSort}
        onClick={() => onSort?.(index)}
      >
        <Glyph
          className={cn(
            "size-3 shrink-0",
            isKey ? "text-status-info" : "text-text-tertiary",
          )}
          aria-hidden
        />
        <span className="min-w-0 flex-1 truncate">{label}</span>
        {/* The slot is always here, whether or not anything is in it. Revealing
            the caret on hover by inserting it would re-flow the label under the
            pointer — and measureColumns has already paid for the width. */}
        <span
          aria-hidden
          className="flex size-3 shrink-0 items-center justify-center"
        >
          {sorted === "asc" ? (
            <ArrowUp className="size-3 text-text-primary" />
          ) : sorted === "desc" ? (
            <ArrowDown className="size-3 text-text-primary" />
          ) : onSort ? (
            <ChevronsUpDown
              className={cn(
                "size-3 text-text-tertiary transition-opacity duration-(--motion-fast) group-hover/th:opacity-100",
                focused ? "opacity-100" : "opacity-0",
              )}
            />
          ) : null}
        </span>
      </button>
      <span
        role="separator"
        aria-label={`Resize ${label}`}
        aria-orientation="vertical"
        className="absolute inset-y-0 right-0 w-1 cursor-col-resize hover:bg-border-default"
        onPointerDown={(event) => {
          event.preventDefault();
          // A resize must never reach the drag sensor, which is listening on
          // the `<th>` this handle sits in.
          event.stopPropagation();
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
