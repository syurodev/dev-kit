import type { CSSProperties } from "react";
import { useMemo, useRef, useState } from "react";
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
import { Braces, Columns3, Copy, List, Pin, PinOff, Rows3 } from "lucide-react";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuLabel,
  ContextMenuTrigger,
} from "@/components/ui/context-menu";
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty";
import { useVirtualRows } from "@/components/ui/virtual-rows";
import { cn } from "@/lib/utils";
import { copyText } from "@/bridge/clipboard";
import type { QueryResult } from "@/bridge/database";
import {
  DEFAULT_COLUMN_WIDTH_PX,
  MIN_COLUMN_WIDTH_PX,
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
import { columnValues, rowAsJSON } from "./rowText";

const ROW_HEIGHT = 36;

/**
 * What the pointer was over when the menu opened.
 *
 * `row: null` is a column header — the menu still knows which column, which is
 * most of what it offers. Headers used to carry no target at all, so the menu
 * rendered every item behind `cell` and opened EMPTY on the one place anyone
 * right-clicks to act on a column.
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
}: {
  result: QueryResult;
  widths: readonly number[];
  columnOrder: readonly number[];
  onResize: (widths: number[]) => void;
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
   * How many columns of the DISPLAY order stay on screen while the grid
   * scrolls sideways. A count, not a set — see frozenOffsets.
   */
  frozen?: number;
  /**
   * Absent in the console, whose columns change with every query. Takes the
   * whole arrangement, because pinning MOVES a column — see pinColumn.
   */
  onFreeze?: (next: { order: number[]; frozen: number }) => void;
}) {
  const rows = useVirtualRows(result.rows.length, ROW_HEIGHT);
  const order = orderedColumns(result.columns, columnOrder);
  // What the content needs, for every column nobody has dragged. Recomputed
  // per result, not per render: it walks fifty rows.
  const measured = useMemo(
    () => measureColumns(result.columns, result.rows),
    [result.columns, result.rows],
  );
  const [target, setTarget] = useState<Target>(null);

  // A click is a sort and a drag is a reorder, and they start with the same
  // pointerdown — four pixels is what tells them apart. Without this the sort
  // never fires and the resize handle reorders instead of resizing.
  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 4 } }),
  );

  const widthOf = (index: number) => widths[index] ?? measured[index] ?? DEFAULT_COLUMN_WIDTH_PX;
  // A drag starts from what is on screen, not from a default: seeding the
  // untouched columns with 180 made dragging the fourth column snap the first
  // three to a width nobody chose.
  const effectiveWidths = result.columns.map((_, index) => widthOf(index));
  // Frozen columns are sticky at an absolute offset — the widths of the frozen
  // columns before them. Indexed by DISPLAY position, which is what `order`
  // walks, not by the column's own index.
  const offsets = frozenOffsets(order, widthOf, frozen);
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
          value: result.rows[target.row][target.column] ?? "",
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
              const el = (event.target as HTMLElement).closest("[data-cell],[data-column]");
              const raw = el?.getAttribute("data-cell");
              if (raw) {
                const [row, column] = raw.split(":").map(Number);
                setTarget(Number.isNaN(row) ? null : { row, column });
                return;
              }
              const column = Number(el?.getAttribute("data-column"));
              setTarget(Number.isNaN(column) ? null : { row: null, column });
            }}
          >
            <table
              className="w-full table-fixed border-collapse text-left text-code-12"
              aria-label="Query results"
            >
              <colgroup>
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
                  <SortableContext items={order} strategy={horizontalListSortingStrategy}>
                    {order.map((index, position) => (
                      <HeaderCell
                        key={`${result.columns[index]}-${index}`}
                        label={result.columns[index]}
                        index={index}
                        sorted={sort?.column === index ? sort.direction : null}
                        frozenAt={offsets[position]}
                        onSort={onSort}
                        onResize={(column, delta) =>
                          onResize(resizeColumn(effectiveWidths, column, delta))
                        }
                      />
                    ))}
                  </SortableContext>
                </tr>
              </thead>
              <tbody>
                {rows.padTop > 0 && <tr style={{ height: rows.padTop }} aria-hidden />}
                {rows.indices.map((rowIndex) => (
                  <tr key={rowIndex} className="group transition-colors hover:bg-surface-raised/60">
                    {/* Only the header drags. Giving each cell a useSortable would
                        mount one per visible cell — 2,220 on a 74-column table — to
                        animate a column that snaps into place on drop anyway. */}
                    {order.map((columnIndex, position) => {
                      const value = result.rows[rowIndex][columnIndex] ?? "";
                      return (
                        <td
                          key={columnIndex}
                          data-cell={`${rowIndex}:${columnIndex}`}
                          style={frozenStyle(position)}
                          className={cn(
                            "h-9 cursor-pointer truncate border-r border-b border-border-subtle px-4 select-text last:border-r-0 hover:text-text-primary",
                            columnIndex === order[0] ? "text-text-primary" : "text-text-secondary",
                            // The row's hover tint has to reach a pinned cell
                            // too, or half the row lights up — and it has to
                            // stack ON the ground frozenStyle painted rather
                            // than replace it, or hovering makes the cell
                            // see-through again.
                            offsets[position] !== undefined &&
                              "group-hover:[background-image:linear-gradient(var(--state-soft),var(--state-soft)),linear-gradient(var(--surface-well),var(--surface-well))]",
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
            {/* A result with columns but no rows used to render as the header
                row over blank space, which reads as a grid still loading. The
                headers stay — they are the answer to "what would have been
                here" — and the shared Empty says the rest. */}
            {result.rows.length === 0 ? (
              // sticky left-0: this sits inside the horizontal scroller, so
              // without it the message slides out of view as you scroll a wide
              // table sideways. Sticky pins it to the scroller's own left edge,
              // where its container width centres it over what you can see.
              <Empty className="sticky left-0 p-8">
                <EmptyHeader>
                  <EmptyMedia variant="icon">
                    <Rows3 />
                  </EmptyMedia>
                  <EmptyTitle>No rows</EmptyTitle>
                  <EmptyDescription>
                    The query ran and matched nothing. Change the filter or the sort and run it
                    again.
                  </EmptyDescription>
                </EmptyHeader>
              </Empty>
            ) : null}
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
            </>
          ) : null}
          {column ? (
            <>
              <ContextMenuItem onSelect={() => void copyText(column.name)}>
                <Columns3 />
                Copy column name
              </ContextMenuItem>
              <ContextMenuItem
                onSelect={() => void copyText(columnValues(result.rows, column.index))}
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
              {onFreeze ? (
                <ContextMenuItem
                  onSelect={() => onFreeze(pinColumn(order, frozen, column.index))}
                >
                  {column.position < frozen ? <PinOff /> : <Pin />}
                  {column.position < frozen ? `Unpin ${column.name}` : `Pin ${column.name}`}
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
}: {
  label: string;
  index: number;
  sorted: "asc" | "desc" | null;
  /** Sticky left offset when this column is frozen, undefined when it scrolls. */
  frozenAt: number | undefined;
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
      className="relative h-10 border-r border-b border-border-subtle bg-surface-raised p-0 text-code-12-medium text-text-secondary last:border-r-0"
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
