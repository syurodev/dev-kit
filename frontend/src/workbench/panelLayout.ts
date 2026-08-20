import { useEffect, useState } from "react";

/**
 * Transparent gutter between the centre content card and either side panel.
 * Single source of truth for both SidebarSurface and SplitPanelSlot — see the
 * gotcha in docs: this value once drifted from a hardcoded Tailwind `w-2`,
 * which made changing it have no visual effect. Both panels set it via inline
 * `style={{ width: GAP_WIDTH }}`, never a utility class.
 */
export const GAP_WIDTH = 4;

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
