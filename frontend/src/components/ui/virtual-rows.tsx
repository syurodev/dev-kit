import { useRef, type RefObject } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";

/**
 * Frontend performance sets virtualization at 100 rows; below it a table is
 * cheaper to render whole than to window. Keeping the number here means the
 * three tables that use it cannot drift apart.
 */
export const VIRTUALIZE_THRESHOLD = 100;

export function shouldVirtualize(rowCount: number): boolean {
  return rowCount > VIRTUALIZE_THRESHOLD;
}

export type VirtualRows = {
  /** Attach to the scrolling ancestor of the table. */
  scrollRef: RefObject<HTMLDivElement | null>;
  /** Row indices to render, in order. */
  indices: number[];
  /** Spacer heights standing in for the rows above and below the window. */
  padTop: number;
  padBottom: number;
  /** False when the table is short enough to render whole. */
  virtualized: boolean;
};

/**
 * Windowing for a real `<table>`, as spacer rows rather than absolute
 * positioning.
 *
 * A `<tr>` taken out of flow takes the table's column widths with it, and
 * `position: sticky` on `<thead>` stops working the moment the body is no
 * longer laid out as table rows. Two empty `<tr>`s carrying the height of what
 * is scrolled past keep both — the browser still lays the table out normally,
 * `table-fixed` still governs the columns, and the sticky header still sticks.
 *
 * Under the threshold this returns every index and no padding, so callers
 * render one way and the table simply stops windowing when it is short —
 * which is also what makes it render whole under renderToString, where there
 * is no scroll element to measure.
 */
export function useVirtualRows(rowCount: number, rowHeight: number): VirtualRows {
  const scrollRef = useRef<HTMLDivElement | null>(null);
  const virtualized = shouldVirtualize(rowCount);

  const virtualizer = useVirtualizer({
    count: virtualized ? rowCount : 0,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => rowHeight,
    overscan: 16,
  });

  if (!virtualized) {
    return {
      scrollRef,
      indices: Array.from({ length: rowCount }, (_, i) => i),
      padTop: 0,
      padBottom: 0,
      virtualized: false,
    };
  }

  const items = virtualizer.getVirtualItems();
  const total = virtualizer.getTotalSize();
  const padTop = items.length > 0 ? items[0].start : 0;
  const padBottom = items.length > 0 ? total - items[items.length - 1].end : 0;

  return {
    scrollRef,
    indices: items.map((item) => item.index),
    padTop,
    padBottom,
    virtualized: true,
  };
}
