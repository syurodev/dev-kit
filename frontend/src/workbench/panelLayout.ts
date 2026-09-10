import { useEffect, useState } from "react";

/**
 * Transparent gutter / sash between the centre content card and either side panel.
 * Single source of truth for both SidebarSurface and SplitPanelSlot — matches 8px
 * sash in Figma App Shell (107:137). Both panels set it via inline
 * `style={{ width: GAP_WIDTH }}`, never a utility class.
 */
export const GAP_WIDTH = 8;

/**
 * The margin between the window edge and the content row (`mx-1 mb-1` on
 * Workbench.tsx). Figma App Shell 107:137 draws this at 8, the same as the
 * sash; DevKit runs it at 4 so the shell reads tighter against the window.
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
// Inset far enough to read as a deliberate band rather than a failed parallel
// corner, they owe the window's squircle nothing — and they look the same on
// Linux and Windows, which never had one to defer to.
//
// 8, the same as the sash, so the shell sits on one rhythm.
export const WINDOW_GUTTER = 8;

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
}): number {
  const { windowWidth, otherEffectiveWidth, min, hardMax } = opts;
  // Both gutters come off the top: the row is inset from either window edge,
  // so that width was never the panels' to divide, and leaving it in would let
  // a panel grow until the centre content sat WINDOW_GUTTER * 2 under the floor
  // this function exists to hold.
  const available =
    windowWidth - WINDOW_GUTTER * 2 - otherEffectiveWidth - CONTENT_MIN_WIDTH - GAP_WIDTH * 2;
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
