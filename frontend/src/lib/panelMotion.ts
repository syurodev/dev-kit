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
 * Only real animations raise it: a panel opening or closing, or a module's
 * requested width. A drag on any shell sash (shellDrag), a window resize, a
 * size forced only by a neighbour's clamp, and Reduce Motion move the panels
 * instantly and never raise it — there is nothing to wait for, and holding a
 * terminal's fit back during a drag would make the drag lie
 * (panelMotionStep).
 */
export type PanelMotionSignal = {
  /** A panel began moving. `watchdogMs` ends it if its completion never comes. */
  start: (id: string, watchdogMs: number) => void;
  /** A panel stopped. Idempotent: ending one that is not moving is a no-op. */
  end: (id: string) => void;
  moving: () => boolean;
  /** Whether this one panel is moving. */
  isMoving: (id: string) => boolean;
  /** How many times every panel has come to rest: changes on the moving → still edge only. */
  settles: () => number;
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
  let settles = 0;
  const notify = () => {
    for (const fn of [...listeners]) fn();
  };
  const end = (id: string) => {
    if (!active.has(id)) return;
    timers.clearTimer(active.get(id));
    active.delete(id);
    if (active.size === 0) {
      settles++;
      notify();
    }
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
    isMoving: (id) => active.has(id),
    settles: () => settles,
    subscribe(listener) {
      listeners.add(listener);
      return () => void listeners.delete(listener);
    },
  };
}

/** The shell's one signal. */
export const panelMotion = createPanelMotion();

/**
 * Whether a shell sash is being dragged. Set by useResizableSize for the
 * length of a drag; every shell panel treats it as "instant", because a drag
 * on one sash moves its neighbours' ceilings too (the sidebar and the columns
 * clamp against each other) and they must follow the pointer, not spring.
 */
export type ShellDrag = {
  set: (dragging: boolean) => void;
  dragging: () => boolean;
  subscribe: (listener: () => void) => () => void;
};

export function createShellDrag(): ShellDrag {
  let dragging = false;
  const listeners = new Set<() => void>();
  return {
    set(next) {
      if (next === dragging) return;
      dragging = next;
      for (const fn of [...listeners]) fn();
    },
    dragging: () => dragging,
    subscribe(listener) {
      listeners.add(listener);
      return () => void listeners.delete(listener);
    },
  };
}

export const shellDrag = createShellDrag();

export function useShellDragging(): boolean {
  return React.useSyncExternalStore(shellDrag.subscribe, shellDrag.dragging, () => false);
}

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
 * `schedule(size)` from a ResizeObserver: with nothing moving it runs right
 * away, exactly as before — a window resize or a drag stays live. While panels
 * move it only remembers that it is owed, and runs once on the frame after
 * they settle; a resize observed in that frame (the final layout) runs it there
 * and drops the queued frame, so it is still once.
 *
 * `size` is any measure of the element that is 0 while it is not laid out.
 * A change to or from 0 runs at once whatever is moving: that is a pane shown
 * from (or parked in) display:none — an inactive tab — not a frame of a panel
 * animation, and deferring it showed the zero-size answer (an unfitted graph,
 * a unified diff about to flip to split) for the length of the motion. The
 * first size seen counts as a change from 0.
 */
export function settledRunner(
  run: () => void,
  opts: { signal?: PanelMotionSignal } & Partial<Frames> = {},
): { schedule: (size?: number) => void; dispose: () => void } {
  const signal = opts.signal ?? panelMotion;
  const raf = opts.raf ?? ((fn: () => void) => requestAnimationFrame(fn));
  const caf = opts.caf ?? ((id: number) => cancelAnimationFrame(id));
  let owed = false;
  let frame = 0;
  let lastSize = 0;

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
    schedule(size) {
      const previous = lastSize;
      if (size !== undefined) lastSize = size;
      const appeared = size !== undefined && (size === 0 || previous === 0);
      if (signal.moving() && !appeared) {
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
 * What a panel last committed: the window's size along its axis, whether it
 * was open, the size stored for it (what a drag or a module set), and the
 * size it was drawn at — `null` when unknown, which makes the next target a
 * real change (a remount replaying an interrupted motion).
 */
export type PanelMotionLast = { window: number; open: boolean; stored: number; target: number | null };

/**
 * Whether a panel's change of size is instant, and what to tell panelMotion.
 *
 * Instant — and never raising the signal — for its own sash drag or Reduce
 * Motion (`requestedInstant`), a drag on any shell sash (`dragging`), a window
 * resize, and a size forced only by a clamp (neither its open state nor its
 * stored size changed: a neighbour moved its ceiling) while the panel is
 * still. The last one is how a window resize that makes the sidebar yield
 * stays instant on the second pass, when the sidebar's open state reaches the
 * columns. While the panel is already moving, the same clamp change is the
 * opening retargeting before its first frame (a column opening as the sidebar
 * yields to it) and keeps the spring.
 */
export function panelMotionStep(
  last: PanelMotionLast,
  next: { window: number; open: boolean; stored: number; target: number; requestedInstant: boolean; dragging: boolean },
  moving: boolean,
): { instant: boolean; action: "start" | "end" | "none" } {
  const changed = last.target !== next.target;
  const clampOnly = changed && last.target !== null && last.open === next.open && last.stored === next.stored;
  const instant =
    next.requestedInstant || next.dragging || last.window !== next.window || (clampOnly && !moving);
  return { instant, action: !changed ? "none" : instant ? "end" : "start" };
}

/**
 * A value that changes only while the panel is still: `next` once nothing
 * moves, else the value it held. For what costs the engine a pass over the
 * whole panel whenever it changes: `inert` on a panel's content restyles
 * every element under it, which on WebKitGTK took 25-30 ms of a column's
 * first frame with a conversation in it (smoothness §3.8). Changed when the
 * panel is still, it costs one frame nobody watches.
 */
export function whenStill<T>(held: T, next: T, still: boolean): T {
  return still ? next : held;
}

/** whenStill across renders; `still` comes from usePanelMotionReport. */
export function useWhenStill<T>(next: T, still: boolean): T {
  // Derived during render: idempotent per input, so a StrictMode double render agrees.
  const held = React.useRef(next);
  held.current = whenStill(held.current, next, still);
  return held.current;
}

/**
 * A closed shell panel's content is inert — not focusable, not clickable —
 * but the attribute is set and lifted only while the panel is still: it
 * stays inert through an open and becomes inert after a close. While it
 * closes, `shielded` asks for a cover over it to take the clicks instead: one
 * element with nothing under it to restyle.
 */
export function useClosedInert(open: boolean, still: boolean): { inert: boolean; shielded: boolean } {
  const inert = useWhenStill(!open, still);
  return { inert, shielded: !open && !inert };
}

/** What to remember across an unmount: a motion cut short is replayed on a remount (StrictMode). */
export function afterUnmount(last: PanelMotionLast, wasMoving: boolean): PanelMotionLast {
  return wasMoving ? { ...last, target: null } : last;
}

/**
 * Reports one panel's size animation to `panelMotion`, and says whether the
 * change should be instant (panelMotionStep).
 *
 * Started in a layout effect on the target's change — before the first frame
 * of the animation, so a ResizeObserver firing in that frame already sees it —
 * and ended by framer's onAnimationComplete (the returned callback), by the
 * watchdog, or by unmounting. The id is scoped to this instance, so a second
 * shell (the gallery, a harness) cannot end this one's motion.
 */
export function usePanelMotionReport(opts: {
  id: string;
  target: number;
  open: boolean;
  /** The size stored for the panel, before any clamp. */
  stored: number;
  /** Its own sash drag, or Reduce Motion. */
  instant: boolean;
  /** The window's size along this panel's axis. */
  windowSize: number;
  /** The size the panel animates from on mount, when it does (the sidebar). */
  from?: number;
}): { instant: boolean; still: boolean; onAnimationComplete: () => void } {
  const { target, open, stored, windowSize } = opts;
  const id = `${opts.id}:${React.useId()}`;
  const dragging = useShellDragging();
  // Re-rendered when every panel comes to rest — and only then, not as one
  // starts — so `still` turns true on that edge.
  React.useSyncExternalStore(panelMotion.subscribe, panelMotion.settles, () => 0);
  const last = React.useRef<PanelMotionLast>({
    window: windowSize,
    open: opts.from !== undefined ? false : open,
    stored,
    target: opts.from ?? target,
  });
  const next = { window: windowSize, open, stored, target, requestedInstant: opts.instant, dragging };
  const { instant, action } = panelMotionStep(last.current, next, panelMotion.isMoving(id));
  // Not moving, and this render does not start a motion either (the start
  // itself is raised in the layout effect below, after this render).
  const still = action !== "start" && !panelMotion.isMoving(id);

  // Before the effect below, so on StrictMode's rehearsal its cleanup has
  // run when that effect runs again.
  React.useLayoutEffect(
    () => () => {
      last.current = afterUnmount(last.current, panelMotion.isMoving(id));
      panelMotion.end(id);
    },
    [id],
  );

  React.useLayoutEffect(() => {
    const step = panelMotionStep(
      last.current,
      { window: windowSize, open, stored, target, requestedInstant: opts.instant, dragging },
      panelMotion.isMoving(id),
    );
    last.current = { window: windowSize, open, stored, target };
    if (step.action === "end") panelMotion.end(id);
    else if (step.action === "start") panelMotion.start(id, (open ? MOTION_SLOW : MOTION_FAST) * 1000 + WATCHDOG_SLACK_MS);
  }, [id, target, open, stored, windowSize, opts.instant, dragging]);

  const onAnimationComplete = React.useCallback(() => panelMotion.end(id), [id]);
  return { instant, still, onAnimationComplete };
}
