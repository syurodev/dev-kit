import * as React from "react";
import type { Transition } from "framer-motion";
import { CONTENT_FADE_DELAY, EASE_OUT_STRONG, MOTION_BASE, MOTION_FAST, MOTION_SLOW, noMotion } from "./motion";

/**
 * Shell panels in motion: the sidebar, the right-hand columns and the bottom
 * row while they open, close or change size by animation.
 *
 * While any of them moves, the document beside it changes width every frame.
 * That is cheap for CSS layout and expensive for anything that re-measures on
 * resize — xterm's fit plus a PTY resize over IPC, a graph refit, a diff that
 * picks split or unified by width. Those listen here and do their work once,
 * after the panels settle (settledRunner), instead of on every frame of it.
 *
 * Only real animations raise it. A drag on a sash, a window resize and Reduce
 * Motion move the panels instantly; there is nothing to wait for, and holding
 * a terminal's fit back during a drag would make the drag lie.
 */
export type PanelMotionSignal = {
  /** A panel began moving. `watchdogMs` ends it if its completion never comes. */
  start: (id: string, watchdogMs: number) => void;
  /** A panel stopped. Idempotent: ending one that is not moving is a no-op. */
  end: (id: string) => void;
  moving: () => boolean;
  /** Called on the edges only: still → moving and moving → still. */
  subscribe: (listener: () => void) => () => void;
};

type Timers = {
  setTimer: (fn: () => void, ms: number) => unknown;
  clearTimer: (id: unknown) => void;
};

const realTimers: Timers = {
  setTimer: (fn, ms) => setTimeout(fn, ms),
  clearTimer: (id) => clearTimeout(id as ReturnType<typeof setTimeout>),
};

/**
 * Keyed by panel, not counted: framer cancels an interrupted animation
 * without completing it, so a retarget is a second start with one end. A
 * counter would leak a "moving" forever on every quick toggle.
 */
export function createPanelMotion(timers: Timers = realTimers): PanelMotionSignal {
  const active = new Map<string, unknown>();
  const listeners = new Set<() => void>();
  const notify = () => {
    for (const fn of [...listeners]) fn();
  };
  const end = (id: string) => {
    if (!active.has(id)) return;
    timers.clearTimer(active.get(id));
    active.delete(id);
    if (active.size === 0) notify();
  };
  return {
    start(id, watchdogMs) {
      const wasStill = active.size === 0;
      if (active.has(id)) timers.clearTimer(active.get(id));
      active.set(id, timers.setTimer(() => end(id), watchdogMs));
      if (wasStill) notify();
    },
    end,
    moving: () => active.size > 0,
    subscribe(listener) {
      listeners.add(listener);
      return () => void listeners.delete(listener);
    },
  };
}

/** The shell's one signal. */
export const panelMotion = createPanelMotion();

/** Whether a shell panel is moving right now, for components that render on it. */
export function usePanelMotion(): boolean {
  return React.useSyncExternalStore(panelMotion.subscribe, panelMotion.moving, () => false);
}

type Frames = {
  raf: (fn: () => void) => number;
  caf: (id: number) => void;
};

/**
 * Wraps a re-measure so it runs at most once per panel motion.
 *
 * `schedule()` from a ResizeObserver: with nothing moving it runs right away,
 * exactly as before — a window resize or a drag stays live. While panels move
 * it only remembers that it is owed, and runs once on the frame after they
 * settle; a resize observed in that frame (the final layout) runs it there and
 * drops the queued frame, so it is still once.
 */
export function settledRunner(
  run: () => void,
  opts: { signal?: PanelMotionSignal } & Partial<Frames> = {},
): { schedule: () => void; dispose: () => void } {
  const signal = opts.signal ?? panelMotion;
  const raf = opts.raf ?? ((fn: () => void) => requestAnimationFrame(fn));
  const caf = opts.caf ?? ((id: number) => cancelAnimationFrame(id));
  let owed = false;
  let frame = 0;

  const flush = () => {
    if (frame) caf(frame);
    frame = 0;
    owed = false;
    run();
  };
  const unsubscribe = signal.subscribe(() => {
    if (!owed || signal.moving() || frame) return;
    frame = raf(() => {
      frame = 0;
      // Another panel may have started in between: keep waiting for it.
      if (owed && !signal.moving()) flush();
    });
  });

  return {
    schedule() {
      if (signal.moving()) {
        owed = true;
        return;
      }
      flush();
    },
    dispose() {
      unsubscribe();
      if (frame) caf(frame);
      frame = 0;
      owed = false;
    },
  };
}

/**
 * How every shell panel moves: a spring with no bounce, `--motion-slow` to
 * open and `--motion-fast` to close. A spring rather than an eased tween
 * because panels get toggled again mid-flight, and a spring retargets from
 * where it is with the velocity it has (smoothness §4.5). SPRING_PANEL was the
 * other candidate; it is stiffness-based and so has one speed both ways, where
 * the shell's rule is that closing is quicker than opening (§4.1).
 */
export function panelTransition(open: boolean, instant: boolean): Transition {
  return instant ? noMotion : { type: "spring", duration: open ? MOTION_SLOW : MOTION_FAST, bounce: 0 };
}

/** The panel's content: in `CONTENT_FADE_DELAY` after the container starts, out with it. */
export function panelContentFade(open: boolean, instant: boolean): Transition {
  if (instant) return noMotion;
  return open
    ? { duration: MOTION_BASE, delay: CONTENT_FADE_DELAY, ease: EASE_OUT_STRONG }
    : { duration: MOTION_FAST, ease: EASE_OUT_STRONG };
}

/**
 * How long a closing panel keeps showing what it last showed: the close's own
 * perceptual duration, not an animation-end event (smoothness §10, "unmount on
 * --motion-*"). Zero under Reduce Motion, where the panel snaps shut.
 */
export function panelCloseMs(reduceMotion: boolean): number {
  return reduceMotion ? 0 : MOTION_FAST * 1000;
}

/** Margin past the transition before the watchdog calls a motion over. */
const WATCHDOG_SLACK_MS = 250;

/**
 * Reports one panel's size animation to `panelMotion`, and says whether the
 * change should be instant.
 *
 * Started in a layout effect on the target's change — before the first frame
 * of the animation, so a ResizeObserver firing in that frame already sees it —
 * and ended by framer's onAnimationComplete (the returned callback), by the
 * watchdog, or by unmounting. A window resize moves the panel instantly: its
 * edge follows the window's rather than trailing it by a transition, and a
 * terminal beside it keeps fitting live.
 */
export function usePanelMotionReport(opts: {
  id: string;
  target: number;
  open: boolean;
  /** A drag on the sash or Reduce Motion. */
  instant: boolean;
  /** The window's size along this panel's axis. */
  windowSize: number;
  /** The size the panel animates from on mount, when it does (the sidebar). */
  from?: number;
}): { instant: boolean; onAnimationComplete: () => void } {
  const { id, target, open, windowSize } = opts;
  const lastWindow = React.useRef(windowSize);
  const windowMoved = lastWindow.current !== windowSize;
  const instant = opts.instant || windowMoved;
  const lastTarget = React.useRef(opts.from ?? target);

  React.useLayoutEffect(() => {
    lastWindow.current = windowSize;
    if (lastTarget.current === target) return;
    lastTarget.current = target;
    if (instant) panelMotion.end(id);
    else panelMotion.start(id, (open ? MOTION_SLOW : MOTION_FAST) * 1000 + WATCHDOG_SLACK_MS);
  }, [id, target, open, instant, windowSize]);

  React.useEffect(() => () => panelMotion.end(id), [id]);

  const onAnimationComplete = React.useCallback(() => panelMotion.end(id), [id]);
  return { instant, onAnimationComplete };
}
