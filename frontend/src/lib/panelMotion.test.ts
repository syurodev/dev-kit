import { describe, expect, test } from "bun:test";
import {
  afterUnmount,
  createPanelMotion,
  createShellDrag,
  panelCloseMs,
  panelContentFade,
  panelMotionStep,
  panelTransition,
  settledRunner,
  type PanelMotionLast,
} from "./panelMotion";
import { MOTION_BASE, MOTION_FAST, MOTION_SLOW, CONTENT_FADE_DELAY, noMotion } from "./motion";

/** A hand-wound clock: timers fire only when the test says time has passed. */
function fakeTimers() {
  let now = 0;
  let next = 1;
  const pending = new Map<number, { at: number; fn: () => void }>();
  return {
    setTimer: (fn: () => void, ms: number) => {
      const id = next++;
      pending.set(id, { at: now + ms, fn });
      return id;
    },
    clearTimer: (id: unknown) => void pending.delete(id as number),
    advance(ms: number) {
      now += ms;
      for (const [id, t] of [...pending].sort((a, b) => a[1].at - b[1].at)) {
        if (t.at <= now && pending.has(id)) {
          pending.delete(id);
          t.fn();
        }
      }
    },
    get size() {
      return pending.size;
    },
  };
}

/** requestAnimationFrame you step by hand. */
function fakeFrames() {
  let next = 1;
  const queued = new Map<number, () => void>();
  return {
    raf: (fn: () => void) => {
      const id = next++;
      queued.set(id, fn);
      return id;
    },
    caf: (id: number) => void queued.delete(id),
    frame() {
      const run = [...queued.values()];
      queued.clear();
      for (const fn of run) fn();
    },
    get size() {
      return queued.size;
    },
  };
}

describe("panel motion signal", () => {
  test("is still until a panel starts, and moving until every one has ended", () => {
    const t = fakeTimers();
    const motion = createPanelMotion(t);
    expect(motion.moving()).toBe(false);
    motion.start("column-a", 400);
    motion.start("sidebar", 400);
    expect(motion.moving()).toBe(true);
    motion.end("column-a");
    expect(motion.moving()).toBe(true);
    motion.end("sidebar");
    expect(motion.moving()).toBe(false);
  });

  test("a retarget mid-flight is one motion, not two to end", () => {
    // framer cancels the interrupted animation without completing it: the
    // panel starts again and completes once.
    const t = fakeTimers();
    const motion = createPanelMotion(t);
    motion.start("column-a", 400);
    motion.start("column-a", 200);
    motion.end("column-a");
    expect(motion.moving()).toBe(false);
  });

  test("ending a panel that never started changes nothing and tells no one", () => {
    const motion = createPanelMotion(fakeTimers());
    let calls = 0;
    motion.subscribe(() => calls++);
    motion.end("column-b");
    expect(calls).toBe(0);
    expect(motion.moving()).toBe(false);
  });

  test("a lost completion is ended by the watchdog", () => {
    const t = fakeTimers();
    const motion = createPanelMotion(t);
    motion.start("bottom", 300);
    t.advance(299);
    expect(motion.moving()).toBe(true);
    t.advance(1);
    expect(motion.moving()).toBe(false);
  });

  test("restarting rearms the watchdog from the new start", () => {
    const t = fakeTimers();
    const motion = createPanelMotion(t);
    motion.start("bottom", 300);
    t.advance(200);
    motion.start("bottom", 300);
    t.advance(200);
    expect(motion.moving()).toBe(true);
    t.advance(100);
    expect(motion.moving()).toBe(false);
    expect(t.size).toBe(0);
  });

  test("listeners hear only the edges", () => {
    const motion = createPanelMotion(fakeTimers());
    const seen: boolean[] = [];
    const off = motion.subscribe(() => seen.push(motion.moving()));
    motion.start("a", 100);
    motion.start("b", 100);
    motion.end("a");
    motion.end("b");
    off();
    motion.start("a", 100);
    expect(seen).toEqual([true, false]);
  });
});

describe("settledRunner", () => {
  test("runs at once while nothing moves (window resize, drag)", () => {
    const motion = createPanelMotion(fakeTimers());
    const f = fakeFrames();
    let runs = 0;
    const r = settledRunner(() => runs++, { signal: motion, ...f });
    r.schedule();
    r.schedule();
    expect(runs).toBe(2);
    r.dispose();
  });

  test("holds every call while panels move and runs exactly once after they settle", () => {
    const motion = createPanelMotion(fakeTimers());
    const f = fakeFrames();
    let runs = 0;
    const r = settledRunner(() => runs++, { signal: motion, ...f });
    motion.start("column-a", 400);
    for (let i = 0; i < 14; i++) r.schedule();
    expect(runs).toBe(0);
    motion.end("column-a");
    // Not in the completion callback itself: on the next frame, after the
    // final size has been laid out.
    expect(runs).toBe(0);
    f.frame();
    expect(runs).toBe(1);
    f.frame();
    expect(runs).toBe(1);
    r.dispose();
  });

  test("a resize observed after settling runs now and drops the queued frame", () => {
    const motion = createPanelMotion(fakeTimers());
    const f = fakeFrames();
    let runs = 0;
    const r = settledRunner(() => runs++, { signal: motion, ...f });
    motion.start("column-a", 400);
    r.schedule();
    motion.end("column-a");
    r.schedule(); // the observer's callback for the final layout
    expect(runs).toBe(1);
    f.frame();
    expect(runs).toBe(1);
    r.dispose();
  });

  test("waits again when another panel starts before the frame", () => {
    const motion = createPanelMotion(fakeTimers());
    const f = fakeFrames();
    let runs = 0;
    const r = settledRunner(() => runs++, { signal: motion, ...f });
    motion.start("sidebar", 200);
    r.schedule();
    motion.end("sidebar");
    motion.start("column-a", 400);
    f.frame();
    expect(runs).toBe(0);
    motion.end("column-a");
    f.frame();
    expect(runs).toBe(1);
    r.dispose();
  });

  test("nothing pending, nothing run on settle", () => {
    const motion = createPanelMotion(fakeTimers());
    const f = fakeFrames();
    let runs = 0;
    const r = settledRunner(() => runs++, { signal: motion, ...f });
    motion.start("a", 100);
    motion.end("a");
    f.frame();
    expect(runs).toBe(0);
    r.dispose();
  });

  test("dispose drops a pending run and stops listening", () => {
    const motion = createPanelMotion(fakeTimers());
    const f = fakeFrames();
    let runs = 0;
    const r = settledRunner(() => runs++, { signal: motion, ...f });
    motion.start("a", 100);
    r.schedule();
    r.dispose();
    motion.end("a");
    f.frame();
    expect(runs).toBe(0);
    expect(f.size).toBe(0);
  });
});

describe("panel transitions", () => {
  test("one curve for every shell panel: a bounce-free spring, slow open, fast close", () => {
    expect(panelTransition(true, false)).toEqual({ type: "spring", duration: MOTION_SLOW, bounce: 0 });
    expect(panelTransition(false, false)).toEqual({ type: "spring", duration: MOTION_FAST, bounce: 0 });
    expect(panelTransition(true, true)).toBe(noMotion);
  });

  test("content trails the container in, and leaves with it", () => {
    expect(panelContentFade(true, false)).toMatchObject({ duration: MOTION_BASE, delay: CONTENT_FADE_DELAY });
    expect(panelContentFade(false, false)).toMatchObject({ duration: MOTION_FAST });
    expect(panelContentFade(false, false)).not.toHaveProperty("delay");
    expect(panelContentFade(true, true)).toBe(noMotion);
  });

  test("a closing panel keeps its content for the close, not for a completion event", () => {
    expect(panelCloseMs(false)).toBe(MOTION_FAST * 1000);
    expect(panelCloseMs(true)).toBe(0);
  });
});

describe("isMoving", () => {
  test("answers per panel", () => {
    const motion = createPanelMotion(fakeTimers());
    motion.start("column-a", 100);
    expect(motion.isMoving("column-a")).toBe(true);
    expect(motion.isMoving("sidebar")).toBe(false);
    motion.end("column-a");
    expect(motion.isMoving("column-a")).toBe(false);
  });
});

describe("settledRunner and sizes to or from zero", () => {
  test("a pane shown from display:none (0 → width) measures at once, even mid-motion", () => {
    const motion = createPanelMotion(fakeTimers());
    const f = fakeFrames();
    let runs = 0;
    const r = settledRunner(() => runs++, { signal: motion, ...f });
    r.schedule(800);
    motion.start("sidebar", 300);
    r.schedule(0); // parked in an inactive tab
    expect(runs).toBe(2);
    r.schedule(760); // shown again while the sidebar still moves
    expect(runs).toBe(3);
    r.schedule(750); // an ordinary frame of the motion
    expect(runs).toBe(3);
    motion.end("sidebar");
    f.frame();
    expect(runs).toBe(4);
    r.dispose();
  });

  test("the first size it ever sees is measured at once", () => {
    const motion = createPanelMotion(fakeTimers());
    const f = fakeFrames();
    let runs = 0;
    const r = settledRunner(() => runs++, { signal: motion, ...f });
    motion.start("column-a", 300);
    r.schedule(640);
    expect(runs).toBe(1);
    r.dispose();
  });
});

describe("the shell drag flag", () => {
  test("is up between a sash's pointerdown and pointerup, and says so on each edge", () => {
    const drag = createShellDrag();
    const seen: boolean[] = [];
    drag.subscribe(() => seen.push(drag.dragging()));
    drag.set(true);
    drag.set(true);
    drag.set(false);
    expect(seen).toEqual([true, false]);
  });
});

describe("panelMotionStep", () => {
  const last = { window: 1440, open: true, stored: 420, target: 424 } satisfies PanelMotionLast;
  const still = { requestedInstant: false, dragging: false };

  test("opening animates and raises the signal", () => {
    const closed = { ...last, open: false, target: 0 };
    expect(panelMotionStep(closed, { ...last, ...still }, false)).toEqual({ instant: false, action: "start" });
  });

  test("a window resize moves the panel instantly and raises nothing", () => {
    expect(panelMotionStep(last, { ...last, window: 1300, target: 380, ...still }, false)).toEqual({ instant: true, action: "end" });
  });

  test("a drag anywhere in the shell is instant for every panel", () => {
    // Dragging the sidebar changes this column's ceiling, not its stored width.
    expect(panelMotionStep(last, { ...last, target: 400, requestedInstant: false, dragging: true }, false)).toEqual({ instant: true, action: "end" });
  });

  test("its own sash drag (or Reduce Motion) is instant", () => {
    expect(panelMotionStep(last, { ...last, stored: 500, target: 504, requestedInstant: true, dragging: false }, false)).toEqual({ instant: true, action: "end" });
  });

  test("a change made only by a neighbour's clamp is instant while the panel is still", () => {
    // The sidebar yields on a window resize; its open state reaches the
    // columns a pass later and widens their ceiling.
    expect(panelMotionStep(last, { ...last, target: 500, ...still }, false)).toEqual({ instant: true, action: "end" });
  });

  test("but retargets an opening already under way (a column opening as the sidebar yields to it)", () => {
    expect(panelMotionStep(last, { ...last, target: 500, ...still }, true)).toEqual({ instant: false, action: "start" });
  });

  test("a module's requested width animates", () => {
    expect(panelMotionStep(last, { ...last, stored: 520, target: 524, ...still }, false)).toEqual({ instant: false, action: "start" });
  });

  test("no change, nothing to report", () => {
    expect(panelMotionStep(last, { ...last, ...still }, false)).toEqual({ instant: false, action: "none" });
  });

  test("an unmount mid-motion (StrictMode's rehearsal) restarts the motion on remount", () => {
    const remembered = afterUnmount(last, true);
    expect(panelMotionStep(remembered, { ...last, ...still }, false)).toEqual({ instant: false, action: "start" });
    // Still when it unmounted: nothing to replay.
    expect(panelMotionStep(afterUnmount(last, false), { ...last, ...still }, false).action).toBe("none");
  });
});
