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
import { DEFAULT_COLUMN_WIDTH_PX, MIN_COLUMN_WIDTH_PX, orderedColumns } from "./gridColumns";
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
