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
 * Deliberately a separate constant from GAP_WIDTH even when the two are equal:
 * one is a draggable sash between two surfaces, the other is dead margin at the
 * window edge, and collapsing them hid a bug in maxBottomHeight below.
 */
// Zero while the shell runs flush to the window. A gutter only ever cost
// something: it opened a 4px band whose corner had to be kept parallel to the
// window's, and macOS draws that corner as a squircle no CSS radius can match.
// Flush, the window's own mask does the rounding and there is nothing to match.
// Restore to 4 to bring the band back — every consumer reads this constant.
export const WINDOW_GUTTER = 0;

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
  const available = windowWidth - otherEffectiveWidth - CONTENT_MIN_WIDTH - GAP_WIDTH * 2;
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
 * between the content row and this slot, WINDOW_GUTTER is the dead margin below
 * it at the window edge. They used to be one `GAP_WIDTH * 2`, which was only
 * correct while both happened to be 8.
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
