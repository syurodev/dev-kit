import * as React from "react";

import { cn } from "@/lib/utils";
import { useVirtualRows } from "@/components/ui/virtual-rows";
import {
  DEFAULT_COLUMN_WIDTH_PX,
  MAX_COLUMN_WIDTH_PX,
  MIN_COLUMN_WIDTH_PX,
  dragWidths,
  fillWidths,
  fitWidths,
  keyboardResize,
  tableColumnWidth,
} from "./columns";

export type DataTableColumn = {
  key: string;
  header: React.ReactNode;
  align?: "left" | "center" | "right";
  /** Cells in the code face — ids, values from a database, offsets. */
  mono?: boolean;
  /** Cells that never wrap, such as the row's name in the first column. */
  nowrap?: boolean;
  /** What the resize handle is called, and what auto-fit measures for the
   *  header, when `header` is not a plain string. Defaults to `key`. */
  label?: string;
};

export type DataTableProps = {
  columns: DataTableColumn[];
  rows: React.ReactNode[][] | (string | null)[][];
  /** Starting widths in px, by column position. Without them a table of
   *  strings is measured from its content and anything else gets the default. */
  initialWidths?: number[];
  /** The cells as text, for measuring and auto-fit when `rows` hold nodes. */
  cellText?: (string | null)[][];
  /** A max-height token class such as "max-h-80". A long table gets one anyway:
   *  windowing needs a scroller with a bound. */
  maxHeight?: string;
  dense?: boolean;
  caption?: string;
  onRowClick?(i: number): void;
  /** The name of the button that opens a clickable row (it sits in the first
   *  cell); without it the button is named by that cell's content. */
  rowAriaLabel?: (row: DataTableProps["rows"][number], index: number) => string;
  /** The clickable row whose detail is open: it reads as expanded and stays
   *  highlighted. Leave unset when a click opens nothing in place. */
  expandedRow?: number | null;
  /** Until somebody resizes, a table narrower than the room grows its columns
   *  to span it (`fillWidths`). Off, it is as wide as its content. */
  fill?: boolean;
  className?: string;
  "data-testid"?: string;
};

const ALIGN = { left: "text-left", center: "text-center", right: "text-right" } as const;

/** The estimate useVirtualRows windows by: line height plus the cell padding. */
const ROW_HEIGHT_PX = 32;
const DENSE_ROW_HEIGHT_PX = 26;

/** The scroller a long table gets when the caller set no bound of its own. */
const VIRTUALIZED_MAX_HEIGHT = "max-h-120";

/**
 * Rows drawn before the windowing hook has measured its scroller — on the first
 * paint, and under renderToString. More than a max-h-120 scroller shows, so the
 * first screen is never blank; the rest is a spacer of their estimated height.
 */
const FIRST_PAINT_ROWS = 40;

/** Rows a column is measured from; a long table is sized by its first screen. */
const MEASURED_ROWS = 50;

function labelOf(column: DataTableColumn): string {
  return column.label ?? (typeof column.header === "string" ? column.header : column.key);
}

/** The rows themselves, when they are strings — only a sample is read. */
function stringRows(rows: DataTableProps["rows"]): (string | null)[][] | undefined {
  for (const row of rows.slice(0, 50)) {
    for (const cell of row) {
      if (cell !== null && typeof cell !== "string") return undefined;
    }
  }
  return rows as (string | null)[][];
}

/**
 * A table whose columns the reader can resize: the shared one behind tool
 * results and markdown tables.
 *
 * `table-layout: fixed` with a `<colgroup>` is what makes a width a width — under
 * `auto` a width on a cell is a suggestion the content overrides. The table is
 * exactly as wide as its columns, so a drag moves the edge under the pointer by
 * the distance the pointer moved, and the scroller around it scrolls sideways
 * when that is wider than the room.
 *
 * Widths are this component's state: they survive re-renders and are not
 * persisted. Until somebody resizes, they follow the content, so a table that
 * streams in is measured on what it has rather than on its first row, and they
 * are fitted to the room (`fitWidths`, `fillWidths`), so the last column is never cut at the
 * edge because an earlier one measured wide. A drag or auto-fit ends both.
 */
export function DataTable({
  columns,
  rows,
  initialWidths,
  cellText,
  maxHeight,
  dense = false,
  caption,
  onRowClick,
  rowAriaLabel,
  expandedRow,
  fill = true,
  className,
  "data-testid": testId,
}: DataTableProps) {
  const text = cellText ?? stringRows(rows);
  const labels = columns.map(labelOf);
  const measured = React.useMemo(() => {
    if (initialWidths) return initialWidths;
    if (!text) return undefined;
    const sample = text.slice(0, MEASURED_ROWS);
    return columns.map((column, index) =>
      tableColumnWidth(
        labelOf(column),
        sample.map((row) => row[index] ?? null),
        column,
      ),
    );
  }, [initialWidths, text, columns]);
  const [dragged, setDragged] = React.useState<number[] | null>(null);
  const rowHeight = dense ? DENSE_ROW_HEIGHT_PX : ROW_HEIGHT_PX;
  const virtual = useVirtualRows(rows.length, rowHeight);
  const room = useRoom(virtual.scrollRef);
  // Until somebody resizes, the content's widths are fitted to the room: a wide
  // column cannot push the last one past the edge, and a narrow table grows to
  // span it. After a resize, theirs win.
  const fitted = React.useMemo(() => {
    const start = columns.map((_, index) => measured?.[index] ?? DEFAULT_COLUMN_WIDTH_PX);
    if (room === null) return start;
    const fit = fitWidths(start, room, {
      min: columns.map((column) => tableColumnWidth(labelOf(column), [], {})),
      wraps: columns.map((column) => !column.nowrap && !virtual.virtualized),
    });
    return fill ? fillWidths(fit, room) : fit;
  }, [columns, measured, room, virtual.virtualized, fill]);
  const widths = columns.map((_, index) => dragged?.[index] ?? fitted[index]);
  const total = widths.reduce((sum, width) => sum + width, 0);

  // One width update per frame, however many pointer events arrive in it.
  const pending = React.useRef<number[] | null>(null);
  const frame = React.useRef<number | null>(null);
  React.useEffect(
    () => () => {
      if (frame.current !== null) cancelAnimationFrame(frame.current);
    },
    [],
  );

  const resizeTo = (next: number[]) => {
    pending.current = next;
    if (frame.current !== null) return;
    frame.current = requestAnimationFrame(() => {
      frame.current = null;
      const queued = pending.current;
      pending.current = null;
      if (queued) setDragged(queued);
    });
  };

  // What the rendered cells need, when there are rendered cells; otherwise
  // what the text says they need.
  const autoFit = (index: number, table: HTMLTableElement | null) => {
    const fit =
      (table ? renderedWidth(table, index) : null) ??
      (text
        ? tableColumnWidth(labels[index], text.map((row) => row[index] ?? null), columns[index])
        : null);
    if (fit === null) return;
    setDragged((current) => {
      const next = [...(current ?? widths)];
      next[index] = Math.min(MAX_COLUMN_WIDTH_PX, Math.max(MIN_COLUMN_WIDTH_PX, fit));
      return next;
    });
  };

  const unmeasured = virtual.virtualized && virtual.indices.length === 0;
  const firstPaint = Math.min(rows.length, FIRST_PAINT_ROWS);
  const indices = unmeasured ? Array.from({ length: firstPaint }, (_, i) => i) : virtual.indices;
  const padTop = unmeasured ? 0 : virtual.padTop;
  const padBottom = unmeasured ? (rows.length - firstPaint) * rowHeight : virtual.padBottom;
  const bound = maxHeight ?? (virtual.virtualized ? VIRTUALIZED_MAX_HEIGHT : undefined);

  return (
    <div
      ref={virtual.scrollRef}
      data-testid={testId}
      className={cn("w-fit max-w-full overflow-auto", bound, className)}
    >
      <table className="table-fixed border-collapse" style={{ width: total }}>
        {caption && (
          <caption className="caption-top px-3 py-1.5 text-left text-caption-12 text-text-tertiary">
            {caption}
          </caption>
        )}
        <colgroup>
          {widths.map((width, index) => (
            <col key={columns[index].key} style={{ width }} />
          ))}
        </colgroup>
        <thead className={cn(bound && "sticky top-0 z-10")}>
          <tr>
            {columns.map((column, index) => (
              <th
                key={column.key}
                scope="col"
                // A sticky header slides over the rows, so it needs a ground the
                // rows cannot show through; a static one keeps the soft wash.
                style={
                  bound
                    ? { background: "linear-gradient(var(--state-soft), var(--state-soft)), var(--surface-base)" }
                    : undefined
                }
                className={cn(
                  "relative truncate px-3 align-bottom text-body-13-medium whitespace-nowrap text-text-secondary",
                  dense ? "py-1" : "py-1.5",
                  !bound && "bg-(--state-soft)",
                  ALIGN[column.align ?? "left"],
                )}
              >
                {column.header}
                <ResizeHandle
                  label={labels[index]}
                  width={widths[index]}
                  onDrag={(from, startX, x) => resizeTo(dragWidths(from, index, startX, x))}
                  onKey={(key, shift) => {
                    const next = keyboardResize(widths, index, key, shift);
                    if (next) setDragged(next);
                    return next !== null;
                  }}
                  onAutoFit={(table) => autoFit(index, table)}
                  widths={widths}
                />
              </th>
            ))}
          </tr>
        </thead>
        <Body
          columns={columns}
          rows={rows}
          text={text}
          clip={virtual.virtualized}
          indices={indices}
          padTop={padTop}
          padBottom={padBottom}
          dense={dense}
          onRowClick={onRowClick}
          rowAriaLabel={rowAriaLabel}
          expandedRow={expandedRow}
        />
      </table>
    </div>
  );
}

/**
 * The width the table may take before it scrolls sideways: the content box of
 * the scroller's parent (the scroller is `max-w-full`), less what the scroller
 * spends on its own border, padding and a vertical scrollbar. Null until it
 * has been read — under renderToString, and without ResizeObserver.
 *
 * The parent is assumed to take its width from outside; one that shrinks to
 * its content would report the table's own width back and never let it grow.
 */
function useRoom(ref: React.RefObject<HTMLDivElement | null>): number | null {
  const [room, setRoom] = React.useState<number | null>(null);
  React.useLayoutEffect(() => {
    const scroller = ref.current;
    const parent = scroller?.parentElement;
    if (!scroller || !parent || typeof ResizeObserver === "undefined") return;
    const read = () => {
      const outer = getComputedStyle(parent);
      const inner = getComputedStyle(scroller);
      const content =
        parent.clientWidth - parseFloat(outer.paddingLeft) - parseFloat(outer.paddingRight);
      const chrome =
        scroller.offsetWidth -
        scroller.clientWidth +
        parseFloat(inner.paddingLeft) +
        parseFloat(inner.paddingRight);
      const next = Math.max(0, Math.floor(content - chrome));
      setRoom((current) => (current === next ? current : next));
    };
    read();
    const observer = new ResizeObserver(read);
    observer.observe(parent);
    return () => observer.disconnect();
  }, [ref]);
  return room;
}

/**
 * The 2px line at a column's edge: the text colour (the focus ring's) for the
 * whole of a drag, whether or not the pointer is still over the handle, and a
 * quieter one on hover; the focus colour when the keyboard is on it.
 */
export function resizeLineClass(dragging: boolean): string {
  return cn(
    "pointer-events-none h-full w-0.5 transition-colors duration-(--motion-fast)",
    dragging
      ? "bg-ring"
      : "bg-transparent group-hover/resize:bg-text-tertiary group-focus-visible/resize:bg-border-focus",
  );
}

function ResizeHandle({
  label,
  width,
  widths,
  onDrag,
  onKey,
  onAutoFit,
}: {
  label: string;
  width: number;
  widths: number[];
  onDrag(from: number[], startX: number, x: number): void;
  onKey(key: string, shift: boolean): boolean;
  onAutoFit(table: HTMLTableElement | null): void;
}) {
  const drag = React.useRef<{ startX: number; from: number[] } | null>(null);
  const [dragging, setDragging] = React.useState(false);
  const endDrag = () => {
    drag.current = null;
    setDragging(false);
  };
  const rounded = Math.round(width);
  return (
    <span
      role="separator"
      tabIndex={0}
      aria-label={`Resize ${label}`}
      aria-orientation="vertical"
      aria-valuenow={rounded}
      aria-valuemin={MIN_COLUMN_WIDTH_PX}
      aria-valuemax={MAX_COLUMN_WIDTH_PX}
      aria-valuetext={`${rounded} pixels`}
      data-resizing={dragging || undefined}
      className="group/resize absolute inset-y-0 right-0 flex w-1.5 cursor-col-resize touch-none justify-end outline-none"
      onPointerDown={(event) => {
        if (event.button !== 0) return;
        event.preventDefault();
        event.stopPropagation();
        drag.current = { startX: event.clientX, from: widths };
        setDragging(true);
        event.currentTarget.setPointerCapture(event.pointerId);
      }}
      onPointerMove={(event) => {
        const start = drag.current;
        if (!start) return;
        onDrag(start.from, start.startX, event.clientX);
      }}
      onPointerUp={(event) => {
        endDrag();
        if (event.currentTarget.hasPointerCapture(event.pointerId)) {
          event.currentTarget.releasePointerCapture(event.pointerId);
        }
      }}
      onPointerCancel={endDrag}
      onLostPointerCapture={endDrag}
      onDoubleClick={(event) => {
        event.stopPropagation();
        onAutoFit(event.currentTarget.closest("table"));
      }}
      onKeyDown={(event) => {
        if (onKey(event.key, event.shiftKey)) {
          event.preventDefault();
          event.stopPropagation();
        }
      }}
    >
      <span data-slot="resize-line" aria-hidden className={resizeLineClass(dragging)} />
    </span>
  );
}

/**
 * The width column `index` needs to show every rendered cell on one line: each
 * cell is set to nowrap for one read, the widest scrollWidth wins, and the
 * cells are put back. Rendered cells only — in a windowed table that is the
 * rows on screen, which is what the reader asked to fit.
 */
function renderedWidth(table: HTMLTableElement, index: number): number | null {
  const cells = Array.from(
    table.querySelectorAll<HTMLTableCellElement>(`tr > :nth-child(${index + 1})`),
  ).filter((cell) => cell.tagName === "TD" || cell.tagName === "TH");
  if (cells.length === 0) return null;
  const saved = cells.map((cell) => cell.style.whiteSpace);
  for (const cell of cells) cell.style.whiteSpace = "nowrap";
  let widest = 0;
  for (const cell of cells) widest = Math.max(widest, cell.scrollWidth);
  cells.forEach((cell, i) => {
    cell.style.whiteSpace = saved[i];
  });
  return widest > 0 ? Math.ceil(widest) + 1 : null;
}

function CellValue({ value }: { value: React.ReactNode }) {
  return value === null || value === undefined ? (
    <span className="text-text-tertiary italic">NULL</span>
  ) : (
    <>{value}</>
  );
}

type BodyProps = {
  columns: DataTableColumn[];
  rows: DataTableProps["rows"];
  /** The cells as text, for the hover title of a cell that may be cut. */
  text?: (string | null)[][];
  /** Every cell on one line: a windowed table estimates one height per row. */
  clip: boolean;
  indices: number[];
  padTop: number;
  padBottom: number;
  dense: boolean;
  onRowClick?(i: number): void;
  rowAriaLabel?: DataTableProps["rowAriaLabel"];
  expandedRow?: number | null;
};

function sameBody(a: BodyProps, b: BodyProps): boolean {
  return (
    a.columns === b.columns &&
    a.rows === b.rows &&
    a.text === b.text &&
    a.clip === b.clip &&
    a.padTop === b.padTop &&
    a.padBottom === b.padBottom &&
    a.dense === b.dense &&
    a.onRowClick === b.onRowClick &&
    a.rowAriaLabel === b.rowAriaLabel &&
    a.expandedRow === b.expandedRow &&
    a.indices.length === b.indices.length &&
    a.indices.every((index, position) => index === b.indices[position])
  );
}

/**
 * The cells, kept out of a resize: a drag changes the colgroup and the header,
 * and the windowing hook hands back new arrays on every render, so the body
 * compares what it shows rather than what it was given.
 */
const Body = React.memo(function Body({
  columns,
  rows,
  text,
  clip,
  indices,
  padTop,
  padBottom,
  dense,
  onRowClick,
  rowAriaLabel,
  expandedRow,
}: BodyProps) {
  return (
    <tbody>
      {padTop > 0 && <tr style={{ height: padTop }} aria-hidden />}
      {indices.map((rowIndex) => {
        const clickable = Boolean(onRowClick);
        const expanded = clickable && expandedRow !== undefined ? expandedRow === rowIndex : undefined;
        return (
          <tr
            key={rowIndex}
            data-expanded={expanded || undefined}
            // A pointer convenience: the whole row is a target for the mouse.
            // The keyboard and assistive tech go through the button in the
            // first cell, and a click that started on it is its own.
            onClick={
              onRowClick
                ? (event) => {
                    if ((event.target as HTMLElement).closest("button")) return;
                    onRowClick(rowIndex);
                  }
                : undefined
            }
            className={cn(
              "border-t border-border-subtle transition-colors duration-(--motion-fast) hover:bg-(--state-soft)",
              clickable && "cursor-pointer",
              expanded && "bg-(--state-soft)",
            )}
          >
            {columns.map((column, index) => {
              const value = rows[rowIndex][index];
              const oneLine = clip || column.nowrap;
              const title = oneLine ? (text?.[rowIndex]?.[index] ?? undefined) : undefined;
              return (
                <td
                  key={column.key}
                  title={title}
                  className={cn(
                    "px-3 align-top text-text-primary",
                    dense ? "py-1" : "py-1.5",
                    column.mono ? "text-code-12" : dense ? "text-body-13" : "text-body-14",
                    oneLine ? "truncate whitespace-nowrap" : "wrap-anywhere",
                    ALIGN[column.align ?? "left"],
                    column.align === "right" && "tabular-nums",
                  )}
                >
                  {onRowClick && index === 0 ? (
                    // The row stays a row — its cells keep their owner — and
                    // what opens it is a real button, which brings Enter and
                    // Space, the name and the expanded state with it.
                    <button
                      type="button"
                      aria-label={rowAriaLabel?.(rows[rowIndex], rowIndex)}
                      aria-expanded={expanded}
                      onClick={() => onRowClick(rowIndex)}
                      className={cn(
                        "w-full cursor-pointer rounded-sm outline-none focus-visible:ring-1 focus-visible:ring-ring focus-visible:ring-inset",
                        ALIGN[column.align ?? "left"],
                        oneLine ? "block truncate" : "wrap-anywhere",
                      )}
                    >
                      <CellValue value={value} />
                    </button>
                  ) : (
                    <CellValue value={value} />
                  )}
                </td>
              );
            })}
          </tr>
        );
      })}
      {padBottom > 0 && <tr style={{ height: padBottom }} aria-hidden />}
    </tbody>
  );
}, sameBody);
