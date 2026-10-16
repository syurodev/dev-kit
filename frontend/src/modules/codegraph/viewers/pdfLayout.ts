/**
 * Geometry of the PDF viewer, kept pure so it can be tested without a DOM.
 *
 * A column of pages is laid out from the page sizes alone (points, from Go) —
 * every page has its place before any of them is drawn, so the scrollbar is
 * right from the start and a page that loads later moves nothing. Only the
 * pages near the viewport are asked for.
 */

/** CSS pixels per PDF point at 100%: a point is 1/72 in, a CSS inch 96 px. */
export const CSS_PX_PER_POINT = 96 / 72;
export const PAGE_GAP = 12;
export const PAGE_MARGIN = 16;

export const ZOOM_STEPS = [0.5, 0.75, 1, 1.25, 1.5, 2, 3] as const;
export const MIN_ZOOM = ZOOM_STEPS[0];
export const MAX_ZOOM = ZOOM_STEPS[ZOOM_STEPS.length - 1];

/** The next zoom step in a direction; from a fit-width value, the nearest step that way. */
export function stepZoom(zoom: number, direction: 1 | -1): number {
  if (direction === 1) return ZOOM_STEPS.find((z) => z > zoom + 1e-6) ?? MAX_ZOOM;
  return [...ZOOM_STEPS].reverse().find((z) => z < zoom - 1e-6) ?? MIN_ZOOM;
}

export const clampZoom = (zoom: number) => Math.min(MAX_ZOOM, Math.max(MIN_ZOOM, zoom));

/** The zoom at which the widest page fills `available` CSS pixels. */
export function fitWidthZoom(sizes: ReadonlyArray<readonly [number, number]>, available: number): number {
  const widest = Math.max(1, ...sizes.map((s) => s[0]));
  return clampZoom(Math.floor((available / (widest * CSS_PX_PER_POINT)) * 100) / 100);
}

export type PageBox = { page: number; top: number; left: number; width: number; height: number };
export type Layout = { boxes: PageBox[]; width: number; height: number };

/** Every page's box in a column, centred on the widest page. */
export function layoutPages(sizes: ReadonlyArray<readonly [number, number]>, zoom: number): Layout {
  const scale = CSS_PX_PER_POINT * zoom;
  const widest = Math.max(0, ...sizes.map((s) => s[0] * scale));
  let top = PAGE_MARGIN;
  const boxes = sizes.map(([w, h], i) => {
    const width = Math.round(w * scale);
    const height = Math.round(h * scale);
    const box = { page: i + 1, top, left: Math.max(0, Math.round((widest - width) / 2)), width, height };
    top += height + PAGE_GAP;
    return box;
  });
  return { boxes, width: Math.round(widest), height: boxes.length ? top - PAGE_GAP + PAGE_MARGIN : 0 };
}

/** First index whose bottom edge is below `y` (binary search on a column). */
function firstEndingAfter(boxes: PageBox[], y: number): number {
  let lo = 0;
  let hi = boxes.length;
  while (lo < hi) {
    const mid = (lo + hi) >> 1;
    if (boxes[mid].top + boxes[mid].height <= y) lo = mid + 1;
    else hi = mid;
  }
  return lo;
}

/** Pages (1-based, inclusive) touching the viewport, widened by `overscan` pages each way. */
export function visibleRange(boxes: PageBox[], scrollTop: number, viewport: number, overscan = 1): { first: number; last: number } | null {
  if (boxes.length === 0) return null;
  const startIdx = Math.min(firstEndingAfter(boxes, scrollTop), boxes.length - 1);
  let endIdx = startIdx;
  while (endIdx + 1 < boxes.length && boxes[endIdx + 1].top < scrollTop + viewport) endIdx++;
  return { first: Math.max(1, startIdx + 1 - overscan), last: Math.min(boxes.length, endIdx + 1 + overscan) };
}

/** The page the reader is on: the one under the middle of the viewport. */
export function currentPage(boxes: PageBox[], scrollTop: number, viewport: number): number {
  if (boxes.length === 0) return 0;
  const mid = scrollTop + viewport / 2;
  const i = Math.min(firstEndingAfter(boxes, mid), boxes.length - 1);
  return boxes[i].page;
}

/** Where to scroll to put a page's top at the top of the viewport. */
export function scrollTopOfPage(boxes: PageBox[], page: number): number {
  const box = boxes[page - 1];
  return box ? Math.max(0, box.top - PAGE_MARGIN) : 0;
}

/** Scroll position that keeps the same spot in view after a zoom change. */
export function scrollAfterZoom(scrollTop: number, from: number, to: number): number {
  return from > 0 ? Math.max(0, Math.round((scrollTop * to) / from)) : scrollTop;
}

/**
 * The resolution to ask Go for. Drawn at the display's pixel density so text is
 * sharp, bucketed so zooming through a few steps reuses renders (Go caches by
 * dpi) instead of asking for a new size at every step. Go's own range is 50–300.
 */
const DPI_LEVELS = [72, 96, 120, 150, 192, 240, 300];
export function dpiFor(zoom: number, devicePixelRatio = 1): number {
  const wanted = 96 * zoom * Math.max(1, devicePixelRatio);
  return DPI_LEVELS.find((d) => d >= wanted) ?? DPI_LEVELS[DPI_LEVELS.length - 1];
}

/** "3.4 MB" for the status line. */
export function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(n < 10 * 1024 ? 1 : 0)} KB`;
  return `${(n / (1024 * 1024)).toFixed(n < 10 * 1024 * 1024 ? 1 : 0)} MB`;
}
