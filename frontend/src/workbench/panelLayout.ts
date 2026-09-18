import { useEffect, useState } from "react";

/**
 * Transparent gutter / sash between the centre content card and either side panel.
 * Single source of truth for both SidebarSurface and SplitPanelSlot. Both panels
 * set it via inline `style={{ width: GAP_WIDTH }}`, never a utility class.
 *
 * 4, and equal to WINDOW_GUTTER: one gap everywhere, inside and at the edge. It
 * mirrors --shell-gap in index.css, which the card radius is computed from.
 * Figma App Shell 107:137 draws 8. The sash's hit area stays wider than this
 * (resize-handle.tsx), so a narrower gap is not a smaller target.
 */
export const GAP_WIDTH = 4;

/**
 * The margin between the window edge and the content row (`mx-1 mb-1` on
 * AppShell.tsx).
 *
 * Separate from GAP_WIDTH even when equal: one is a draggable sash between two
 * surfaces, the other dead margin at the window edge, and collapsing them hid a
 * bug in maxBottomHeight below.
 */
// The band of app background between the window edge and the shell surfaces,
// on the three sides that have one: left, right and bottom. The top is flush,
// because the title bar is already the app's own chrome and a band under it
// would read as the surfaces hanging rather than sitting.
//
// Not 0: flush surfaces read as walls of the window, and DevKit's are cards.
//
// 4, the same as the sash, so the shell sits on one rhythm. It used to be 8,
// wide enough that a card corner not parallel to the window's went unnoticed.
// At 4 it would be noticed, so the corners are made parallel instead: the card
// radius is the window's radius minus this gap (--shell-radius, index.css),
// with the window's radius read from the OS (window_shape*.go).
export const WINDOW_GUTTER = 4;

/** Hard floor for the centre content area, whatever the two side panels do. */
export const CONTENT_MIN_WIDTH = 480;

/** Tracks the live viewport width so the cross-panel clamp can use real space. */
export function useWindowWidth(): number {
  const [width, setWidth] = useState(() => window.innerWidth);
  useEffect(() => {
    const onResize = () => setWidth(window.innerWidth);
    window.addEventListener("resize", onResize);
    return () => window.removeEventListener("resize", onResize);
  }, []);
  return width;
}

/**
 * How wide one side panel may grow, given how much the *other* one is
 * actually taking up right now.
 *
 * `otherEffectiveWidth` must be the space the other panel currently occupies
 * — zero when it is closed, not its remembered width. Both panels keep their
 * last dragged width in the store while closed, so passing that through
 * shrinks this panel's ceiling for space nothing is using.
 *
 * Never returns less than `min`: on a narrow window the arithmetic can go
 * below (or past) the floor, and a max under the min would drag the panel
 * *down* through its own minimum on the next pointer move.
 */
export function maxPanelWidth(opts: {
  windowWidth: number;
  otherEffectiveWidth: number;
  min: number;
  hardMax: number;
  /** The document area's floor; more than CONTENT_MIN_WIDTH with several groups. */
  contentMin?: number;
}): number {
  const { windowWidth, otherEffectiveWidth, min, hardMax, contentMin = CONTENT_MIN_WIDTH } = opts;
  // Both gutters come off the top: the row is inset from either window edge,
  // so that width was never the panels' to divide, and leaving it in would let
  // a panel grow until the centre content sat WINDOW_GUTTER * 2 under the floor
  // this function exists to hold.
  const available =
    windowWidth - WINDOW_GUTTER * 2 - otherEffectiveWidth - contentMin - GAP_WIDTH * 2;
  return Math.max(min, Math.min(hardMax, available));
}

/**
 * The title bar's fixed height. Mirrors the `h-10` (40px) on Workbench.tsx's
 * <header> — Step 4a reconciled to Figma Titlebar 732:2170 (1440×40).
 */
export const TITLE_BAR_HEIGHT = 40;

/** Hard floor for the centre content row, whatever the bottom slot does. */
export const CONTENT_MIN_HEIGHT = 240;

/** Tracks the live viewport height, for the bottom slot's clamp. */
export function useWindowHeight(): number {
  const [height, setHeight] = useState(() => window.innerHeight);
  useEffect(() => {
    const onResize = () => setHeight(window.innerHeight);
    window.addEventListener("resize", onResize);
    return () => window.removeEventListener("resize", onResize);
  }, []);
  return height;
}

/**
 * How tall the bottom slot may grow.
 *
 * Deliberately not maxPanelWidth with different constants: the two side
 * panels clamp against *each other* because they share one horizontal budget,
 * and the bottom slot has no such neighbour — it only owes the content row
 * above it a floor. Passing a fake `otherEffectiveWidth: 0` through the other
 * function would have hidden that difference behind a coincidence.
 *
 * The two terms subtracted below are different things: GAP_WIDTH is the sash
 * between the content row and this slot, WINDOW_GUTTER the dead margin below it
 * at the window edge. Folding them into one `GAP_WIDTH * 2` is correct only
 * while both happen to be 8.
 *
 * Never returns less than `min`, for the same reason maxPanelWidth does not.
 */
export function maxBottomHeight(opts: {
  windowHeight: number;
  min: number;
  hardMax: number;
}): number {
  const { windowHeight, min, hardMax } = opts;
  const available =
    windowHeight - TITLE_BAR_HEIGHT - CONTENT_MIN_HEIGHT - GAP_WIDTH - WINDOW_GUTTER;
  return Math.max(min, Math.min(hardMax, available));
}

/** The narrowest a right-hand column goes. */
export const COLUMN_MIN_WIDTH = 320;
/** The narrowest a document group goes while it is open. */
export const GROUP_MIN_WIDTH = 360;
/** An unfocused group folded to a strip: its module's icon and its tab. */
export const GROUP_STRIP_WIDTH = 40;

/** How much width `groups` side-by-side document groups need, each open. */
export function documentFloor(groups: number): number {
  return Math.max(CONTENT_MIN_WIDTH, groups * GROUP_MIN_WIDTH + (groups - 1) * GAP_WIDTH);
}

export type LayoutPressure = {
  /** The sidebar gives way, its open state untouched: it comes back with the room. */
  sidebarYields: boolean;
  /** Column B's panes stack into column A instead of taking a column of their own. */
  mergeColumns: boolean;
  /** Every group but the focused one folds to a strip. */
  foldGroups: boolean;
  /** The floor the columns clamp against, given what folded. */
  contentMin: number;
};

/**
 * What gives way when the window is too narrow for everything that is open
 * (docs/dev-kit/layout.mdx § Khi cửa sổ nhỏ lại), in this order:
 *
 *   1. panes shrink to their minimum and the document to its floor — the
 *      clamps elsewhere in this file already do that;
 *   2. the sidebar gives way;
 *   3. the second column stacks into the first;
 *   4. unfocused document groups fold to strips — never the focused one.
 *
 * Companions are never closed. Nothing here is stored: widen the window and
 * each step undoes itself, back to the layout that was saved.
 */
export function layoutPressure(opts: {
  windowWidth: number;
  sidebarOpen: boolean;
  sidebarWidth: number;
  columns: number;
  groups: number;
}): LayoutPressure {
  const { windowWidth, sidebarOpen, sidebarWidth, columns, groups } = opts;
  const room = windowWidth - WINDOW_GUTTER * 2;
  const side = (open: boolean) => (open ? sidebarWidth + GAP_WIDTH : 0);
  const cols = (n: number) => n * (COLUMN_MIN_WIDTH + GAP_WIDTH);
  const folded = CONTENT_MIN_WIDTH + (groups - 1) * (GROUP_STRIP_WIDTH + GAP_WIDTH);
  const fits = (sidebar: boolean, n: number, doc: number) => room - side(sidebar) - cols(n) >= doc;

  const floor = documentFloor(groups);
  if (fits(sidebarOpen, columns, floor)) {
    return { sidebarYields: false, mergeColumns: false, foldGroups: false, contentMin: floor };
  }
  if (fits(false, columns, floor)) {
    return { sidebarYields: sidebarOpen, mergeColumns: false, foldGroups: false, contentMin: floor };
  }
  const merged = Math.min(columns, 1);
  if (fits(false, merged, floor)) {
    return { sidebarYields: sidebarOpen, mergeColumns: columns > 1, foldGroups: false, contentMin: floor };
  }
  return { sidebarYields: sidebarOpen, mergeColumns: columns > 1, foldGroups: groups > 1, contentMin: folded };
}
