import * as React from "react";

import { cn } from "@/lib/utils";
import { useVirtualRows } from "@/components/ui/virtual-rows";
import { DEFAULT_COLUMN_WIDTH_PX, keyboardResize, measureColumns, nextWidths } from "./columns";

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
  className?: string;
  "data-testid"?: string;
};

const ALIGN = { left: "text-left", center: "text-center", right: "text-right" } as const;

/** The estimate useVirtualRows windows by: line height plus the cell padding. */
const ROW_HEIGHT_PX = 32;
const DENSE_ROW_HEIGHT_PX = 26;

/** The scroller a long table gets when the caller set no bound of its own. */
const VIRTUALIZED_MAX_HEIGHT = "max-h-120";

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
 * streams in is measured on what it has rather than on its first row.
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
  className,
  "data-testid": testId,
}: DataTableProps) {
  const text = cellText ?? stringRows(rows);
  const labels = columns.map(labelOf);
  const labelKey = labels.join("\u0000");
  const measured = React.useMemo(
    () =>
      initialWidths ??
      (text ? measureColumns(labelKey.split("\u0000"), text) : undefined),
    [initialWidths, text, labelKey],
  );
  const [dragged, setDragged] = React.useState<number[] | null>(null);
  const widths = columns.map(
    (_, index) => dragged?.[index] ?? measured?.[index] ?? DEFAULT_COLUMN_WIDTH_PX,
  );
  const total = widths.reduce((sum, width) => sum + width, 0);

  // One width update per frame, however many pointer events arrive in it.
  const pending = React.useRef<{ index: number; delta: number; from: number[] } | null>(null);
  const frame = React.useRef<number | null>(null);
  React.useEffect(
    () => () => {
      if (frame.current !== null) cancelAnimationFrame(frame.current);
    },
    [],
  );

  const resizeBy = (index: number, delta: number, from: number[]) => {
    const queued = pending.current;
    pending.current =
      queued && queued.index === index ? { ...queued, delta: queued.delta + delta } : { index, delta, from };
    if (frame.current !== null) return;
    frame.current = requestAnimationFrame(() => {
      frame.current = null;
      const next = pending.current;
      pending.current = null;
      if (!next) return;
      setDragged((current) => nextWidths(current ?? next.from, next.index, next.delta));
    });
  };

  const autoFit = (index: number) => {
    if (!text) return;
    const [fit] = measureColumns(
      [labels[index]],
      text.map((row) => [row[index] ?? null]),
    );
    setDragged((current) => {
      const next = [...(current ?? widths)];
      next[index] = fit;
      return next;
    });
  };

  const virtual = useVirtualRows(rows.length, dense ? DENSE_ROW_HEIGHT_PX : ROW_HEIGHT_PX);
  const bound = maxHeight ?? (virtual.virtualized ? VIRTUALIZED_MAX_HEIGHT : undefined);

  return (
    <div
      ref={virtual.scrollRef}
      data-testid={testId}
      className={cn("max-w-full overflow-auto", bound, className)}
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
                  onDrag={(delta, from) => resizeBy(index, delta, from)}
                  onKey={(key, shift) => {
                    const next = keyboardResize(widths, index, key, shift);
                    if (next) setDragged(next);
                    return next !== null;
                  }}
                  onAutoFit={() => autoFit(index)}
                  widths={widths}
                />
              </th>
            ))}
          </tr>
        </thead>
        <Body
          columns={columns}
          rows={rows}
          indices={virtual.indices}
          padTop={virtual.padTop}
          padBottom={virtual.padBottom}
          dense={dense}
          onRowClick={onRowClick}
        />
      </table>
    </div>
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
  onDrag(delta: number, from: number[]): void;
  onKey(key: string, shift: boolean): boolean;
  onAutoFit(): void;
}) {
  const last = React.useRef<{ x: number; from: number[] } | null>(null);
  return (
    <span
      role="separator"
      tabIndex={0}
      aria-label={`Resize ${label}`}
      aria-orientation="vertical"
      aria-valuenow={Math.round(width)}
      className="absolute inset-y-0 right-0 w-1 cursor-col-resize outline-none hover:bg-border-default focus-visible:bg-border-focus"
      onPointerDown={(event) => {
        if (event.button !== 0) return;
        event.preventDefault();
        event.stopPropagation();
        last.current = { x: event.clientX, from: widths };
        event.currentTarget.setPointerCapture(event.pointerId);
      }}
      onPointerMove={(event) => {
        const drag = last.current;
        if (!drag) return;
        const delta = event.clientX - drag.x;
        if (delta === 0) return;
        last.current = { ...drag, x: event.clientX };
        onDrag(delta, drag.from);
      }}
      onPointerUp={(event) => {
        last.current = null;
        if (event.currentTarget.hasPointerCapture(event.pointerId)) {
          event.currentTarget.releasePointerCapture(event.pointerId);
        }
      }}
      onPointerCancel={() => {
        last.current = null;
      }}
      onDoubleClick={(event) => {
        event.stopPropagation();
        onAutoFit();
      }}
      onKeyDown={(event) => {
        if (onKey(event.key, event.shiftKey)) {
          event.preventDefault();
          event.stopPropagation();
        }
      }}
    />
  );
}

type BodyProps = {
  columns: DataTableColumn[];
  rows: DataTableProps["rows"];
  indices: number[];
  padTop: number;
  padBottom: number;
  dense: boolean;
  onRowClick?(i: number): void;
};

function sameBody(a: BodyProps, b: BodyProps): boolean {
  return (
    a.columns === b.columns &&
    a.rows === b.rows &&
    a.padTop === b.padTop &&
    a.padBottom === b.padBottom &&
    a.dense === b.dense &&
    a.onRowClick === b.onRowClick &&
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
  indices,
  padTop,
  padBottom,
  dense,
  onRowClick,
}: BodyProps) {
  return (
    <tbody>
      {padTop > 0 && <tr style={{ height: padTop }} aria-hidden />}
      {indices.map((rowIndex) => (
        <tr
          key={rowIndex}
          tabIndex={onRowClick ? 0 : undefined}
          onClick={onRowClick ? () => onRowClick(rowIndex) : undefined}
          onKeyDown={
            onRowClick
              ? (event) => {
                  if (event.key !== "Enter") return;
                  event.preventDefault();
                  onRowClick(rowIndex);
                }
              : undefined
          }
          className={cn(
            "border-t border-border-subtle transition-colors duration-(--motion-fast) hover:bg-(--state-soft)",
            onRowClick && "cursor-pointer outline-none focus-visible:bg-(--state-soft)",
          )}
        >
          {columns.map((column, index) => {
            const value = rows[rowIndex][index];
            return (
              <td
                key={column.key}
                className={cn(
                  "px-3 align-top text-text-primary",
                  dense ? "py-1" : "py-1.5",
                  column.mono ? "text-code-12" : dense ? "text-body-13" : "text-body-14",
                  column.nowrap ? "truncate whitespace-nowrap" : "wrap-anywhere",
                  ALIGN[column.align ?? "left"],
                  column.align === "right" && "tabular-nums",
                )}
              >
                {value === null || value === undefined ? (
                  <span className="text-text-tertiary italic">NULL</span>
                ) : (
                  value
                )}
              </td>
            );
          })}
        </tr>
      ))}
      {padBottom > 0 && <tr style={{ height: padBottom }} aria-hidden />}
    </tbody>
  );
}, sameBody);
