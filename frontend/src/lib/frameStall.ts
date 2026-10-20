import { perfLog, workbenchRenderClock } from "./perfLog";

/**
 * Finds the hitches `perfTrace` cannot: a frame that arrives late while the
 * person scrolls, types or watches an agent stream. WebKit has no `longtask`
 * observer and a tab switch is only one of the moments a window feels janky,
 * so this watches the frames themselves.
 *
 * Two clocks run side by side, and what separates them is the answer to "is it
 * our code or the compositor":
 *
 * - a requestAnimationFrame loop times the gap between frames;
 * - a 25 ms timer times how late the main thread ran it.
 *
 * A long frame gap with a late timer means JavaScript or layout held the main
 * thread. A long frame gap with an on-time timer means the main thread was
 * free and the frame still did not arrive: paint, GPU or the compositor.
 *
 * The pure detector is separate from the browser wiring so the rule can be
 * tested without a window. The wiring runs only behind `devkitPerf` (see
 * main.tsx): an rAF loop that never stops is not something to ship on.
 */

export interface Stall {
  /** Time between the previous frame and this one. */
  gapMs: number;
  /** The most the main thread was late running its timer inside that gap. */
  blockedMs: number;
  /** Workbench React render time inside that gap. */
  reactMs: number;
  /** The last input at or just before the start of the gap, if any; agoMs is how long before it. */
  input?: { type: string; target: string; agoMs: number };
}

export interface StallDetector {
  frame(now: number, reactClockMs: number): void;
  /** The timer ran this many ms later than it asked to. */
  late(ms: number): void;
  input(type: string, target: string, at: number): void;
  /** The page was hidden or frozen: the next gap says nothing about frames. */
  reset(): void;
}

/** 3 dropped frames at 60 Hz; below it a gap is within what a busy frame costs. */
export const STALL_THRESHOLD_MS = 50;
/** An input counts as the cause when it happened this soon before the gap began. */
const INPUT_WINDOW_MS = 100;

export function createStallDetector(
  onStall: (stall: Stall) => void,
  thresholdMs = STALL_THRESHOLD_MS,
): StallDetector {
  let lastFrame: number | undefined;
  let lastReact = 0;
  let worstLate = 0;
  let lastInput: { type: string; target: string; at: number } | undefined;

  return {
    frame(now, reactClockMs) {
      if (lastFrame !== undefined && now - lastFrame > thresholdMs) {
        const stall: Stall = {
          gapMs: now - lastFrame,
          blockedMs: worstLate,
          reactMs: Math.max(0, reactClockMs - lastReact),
        };
        if (lastInput && lastInput.at >= lastFrame - INPUT_WINDOW_MS && lastInput.at <= now) {
          stall.input = {
            type: lastInput.type,
            target: lastInput.target,
            agoMs: Math.max(0, lastFrame - lastInput.at),
          };
        }
        onStall(stall);
      }
      lastFrame = now;
      lastReact = reactClockMs;
      worstLate = 0;
    },
    late(ms) {
      if (ms > worstLate) worstLate = ms;
    },
    input(type, target, at) {
      lastInput = { type, target, at };
    },
    reset() {
      lastFrame = undefined;
      worstLate = 0;
    },
  };
}

/** One line per stall: what was late, which clock says why, and what came before it. */
export function describeStall(stall: Stall, where: string): string {
  const cause =
    stall.blockedMs >= stall.gapMs * 0.5
      ? `main thread held ${Math.round(stall.blockedMs)}ms`
      : `main thread free (blocked ${Math.round(stall.blockedMs)}ms): paint or compositor`;
  const input = stall.input
    ? `, after ${stall.input.type} on ${stall.input.target}`
    : ", no input just before";
  return (
    `[perf] stall ${Math.round(stall.gapMs)}ms: ${cause}, ` +
    `${Math.round(stall.reactMs)}ms React${input} while "${where}" was active`
  );
}

const TIMER_MS = 25;
/** A page cannot flood the log, whatever it does. */
const MAX_LINES = 300;
const INPUT_EVENTS = ["keydown", "wheel", "pointerdown", "input"] as const;

/** Where an event landed, as shadcn's `data-slot` names it, else the tag. */
function describeTarget(target: EventTarget | null): string {
  if (!(target instanceof Element)) return "window";
  const slot = target.closest("[data-slot]")?.getAttribute("data-slot");
  return slot ?? target.tagName.toLowerCase();
}

export function installFrameStallProbe(options: { where: () => string }): () => void {
  let lines = 0;
  const detector = createStallDetector((stall) => {
    if (lines >= MAX_LINES) return;
    lines += 1;
    perfLog(describeStall(stall, options.where()));
    if (lines === MAX_LINES) perfLog(`[perf] stall probe stopped after ${MAX_LINES} lines`);
  });

  let raf = 0;
  const onFrame = (now: number) => {
    detector.frame(now, workbenchRenderClock());
    raf = requestAnimationFrame(onFrame);
  };
  raf = requestAnimationFrame(onFrame);

  let expected = performance.now() + TIMER_MS;
  let timer = window.setTimeout(function tick() {
    const now = performance.now();
    detector.late(Math.max(0, now - expected));
    expected = now + TIMER_MS;
    timer = window.setTimeout(tick, TIMER_MS);
  }, TIMER_MS);

  const onInput = (event: Event) => {
    detector.input(event.type, describeTarget(event.target), event.timeStamp);
  };
  for (const type of INPUT_EVENTS) window.addEventListener(type, onInput, { capture: true, passive: true });

  // Timers and frames stop in a hidden window; the gap on return is not a stall.
  const onVisibility = () => {
    detector.reset();
    expected = performance.now() + TIMER_MS;
  };
  document.addEventListener("visibilitychange", onVisibility);

  perfLog("[perf] frame stall probe on (gaps over 50ms are logged)");

  return () => {
    cancelAnimationFrame(raf);
    window.clearTimeout(timer);
    for (const type of INPUT_EVENTS) window.removeEventListener(type, onInput, { capture: true });
    document.removeEventListener("visibilitychange", onVisibility);
  };
}
