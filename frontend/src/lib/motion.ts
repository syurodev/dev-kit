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

/**
 * The CSS motion tokens, in the seconds framer wants. index.css is the source
 * of truth (--motion-fast/base/slow, --ease-out-strong); these mirror it so a
 * framer animation and a CSS one that describe the same gesture cannot drift.
 */
export const MOTION_FAST = 0.12;
export const MOTION_BASE = 0.18;
export const MOTION_SLOW = 0.24;
/** Easing curves mirror index.css. Keep CSS and Motion controls in one family. */
export const EASE_OUT = [0.16, 1, 0.3, 1] as const;
export const EASE_IN_OUT = [0.77, 0, 0.175, 1] as const;
export const EASE_DRAWER = [0.32, 0.72, 0, 1] as const;
export const EASE_OUT_CSS = "cubic-bezier(0.16, 1, 0.3, 1)";
export const EASE_OUT_STRONG = EASE_OUT;

/** Tactile feedback on a control; never used to bounce reading surfaces. */
export const SPRING_PRESS = { type: "spring", stiffness: 500, damping: 30, mass: 0.6 } as const;
/** Small label or icon swaps within a fixed slot. */
export const SPRING_SWAP = { type: "spring", stiffness: 460, damping: 30, mass: 0.55 } as const;
/** Panel physics for surfaces with an interruptible Motion lifecycle. */
export const SPRING_PANEL = { type: "spring", stiffness: 420, damping: 40, mass: 0.5 } as const;
export const SPRING_LAYOUT = { type: "spring", stiffness: 360, damping: 32, mass: 0.6 } as const;
/** Decorative pointer following, not input value updates. */
export const SPRING_MOUSE = { stiffness: 200, damping: 15, mass: 0.3 } as const;
/** Damped pointer glide, without overshoot at either end. */
export const SPRING_GLIDE = { stiffness: 700, damping: 50, mass: 0.5 } as const;

/** Content trailing its container, so the panel leads and the body follows. */
export const CONTENT_FADE_DELAY = 0.06;

/**
 * animate-text's `fade-through`, for text that replaces text in the same place
 * with no direction to it: the status line of a running turn moving from one
 * step to the next, and a new block of an answer arriving.
 *
 * Its own timings rather than the tokens above. Those are for controls, which
 * answer a click and have to be quick; this is text someone is reading, and at
 * 180ms a 2px blur resolves too fast to register as anything but a flicker.
 * The values are the spec's, from
 * .agents/skills/animate-text/assets/specs/fade-through.json. The entry keeps
 * to opacity, travel, and a small scale change: blur made a streamed answer
 * pay for a filter layer on every new block.
 *
 * `delay` is the swap: the spec starts the new text at exit − overlap(20ms) +
 * micro-delay(60ms). AnimatePresence's "wait" already holds the new text for
 * the whole exit, which leaves 40ms.
 */
export const FADE_THROUGH = {
  enter: { duration: 0.42, ease: [0.2, 0, 0, 1] as const },
  exit: { duration: 0.26, ease: [0.4, 0, 1, 1] as const },
  delay: 0.04,
  from: { opacity: 0, y: 6, scale: 0.99 },
  to: { opacity: 1, y: 0, scale: 1 },
  out: { opacity: 0, y: -4 },
} as const;
