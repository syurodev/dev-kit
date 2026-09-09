import type { Transition, Variants } from "framer-motion";

/**
 * One slide, shared by every tab system so they all move the same way.
 *
 * `custom` is what the leaving panel needs to stay put while the arriving one
 * takes its place in flow: direction of travel (+1 = the new panel is further
 * along the strip, so it arrives from the right and the old one leaves left),
 * and the old panel's own top/height so it can go absolute without jumping —
 * an absolute child of a flex column otherwise snaps to the column's start.
 */
export type SlideCustom = { dir: number; top: number; height: number };

export const slideVariants: Variants = {
  enter: ({ dir }: SlideCustom) => ({ x: dir < 0 ? "-100%" : "100%", opacity: 0 }),
  center: { x: 0, opacity: 1 },
  exit: ({ dir, top, height }: SlideCustom) => ({
    x: dir < 0 ? "100%" : "-100%",
    opacity: 0,
    position: "absolute",
    top,
    left: 0,
    right: 0,
    height,
  }),
};

// --motion-base, as a spring with no bounce: data does not wobble.
export const slideTransition: Transition = { type: "spring", duration: 0.18, bounce: 0 };
export const noMotion: Transition = { duration: 0 };
