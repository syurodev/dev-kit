import type { CSSProperties } from "react";
import { useRef, useState } from "react";
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
  reorderColumn,
} from "./gridColumns";
// A4: moved here from QueryConsole rather than reimplemented. rowAsJSON maps
// over the ROW and names an empty column col_${i}; an inlined
// Object.fromEntries over `columns` is a different function with its own bugs.
import { columnValues, rowAsJSON } from "./rowText";

const ROW_HEIGHT = 36;

/**
 * The number the gutter shows for a row.
 *
 * `rowIndex` is the row's position in the result; `rowOffset` is how many rows
 * of the same table come before this page. A27: the gutter used to show
 * `rowIndex + 1` alone, so page 2 restarted at 1 while the pager an inch away
 * read "201-400" — the same row called 1 and 201 at once. The offset is what
 * makes the gutter count the table the pager is counting.
 *
 * It stays a function of the ROW's index, never of the render order: the
 * window of virtualised rows starts wherever the scroll is, and a counter over
 * the rendered ones would restart every time you scrolled.
 */
export function rowNumber(rowIndex: number, rowOffset: number): number {
  return rowOffset + rowIndex + 1;
}

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
  onReorder,
  rowOffset = 0,
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
   * Accepts a new column order. Absent in the console, whose columns change
   * with every query, so an order saved against the last one describes columns
   * this one may not have.
   */
  onReorder?: (order: number[]) => void;
  /**
   * Rows of the same table before this page, added to every gutter number. A
   * table's pages are one table, and its page 2 starts where the pager says it
   * does. A console result has nothing before it — its rows are 1..n — so the
   * console passes nothing and this stays 0.
   */
  rowOffset?: number;
}) {
  const rows = useVirtualRows(result.rows.length, ROW_HEIGHT);
  const order = orderedColumns(result.columns, columnOrder);
  const [target, setTarget] = useState<Target>(null);

  // A click is a sort and a drag is a reorder, and they start with the same
  // pointerdown — four pixels is what tells them apart. Without this the sort
  // never fires and the resize handle reorders instead of resizing.
  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 4 } }),
  );

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
                  <SortableContext items={order} strategy={horizontalListSortingStrategy}>
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
                  </SortableContext>
                </tr>
              </thead>
              <tbody>
                {rows.padTop > 0 && <tr style={{ height: rows.padTop }} aria-hidden />}
                {rows.indices.map((rowIndex) => (
                  <tr key={rowIndex} className="group transition-colors hover:bg-surface-raised/60">
                    {/* The row number is the row's own, not the window's: with
                        virtualised rows a counter over the rendered ones would
                        restart every time you scrolled. rowNumber takes the
                        index, and the page's offset, for that reason. */}
                    <td className="h-9 border-r border-b border-border-subtle px-3 text-code-11 text-text-tertiary tabular-nums">
                      {rowNumber(rowIndex, rowOffset)}
                    </td>
                    {/* Only the header drags. Giving each cell a useSortable would
                        mount one per visible cell — 2,220 on a 74-column table — to
                        animate a column that snaps into place on drop anyway. */}
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
  const { listeners, setNodeRef, transform, transition, isDragging } = useSortable({
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
    position: "relative",
    transform: CSS.Translate.toString(transform),
    transition,
    zIndex: isDragging ? 1 : 0,
    opacity: isDragging ? 0.8 : 1,
  };

  return (
    <th
      ref={setNodeRef}
      style={style}
      {...listeners}
      className="relative h-10 border-r border-b border-border-subtle p-0 text-code-12-medium text-text-secondary last:border-r-0"
    >
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
