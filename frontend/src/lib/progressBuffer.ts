/**
 * A cadence for progress that arrives faster than anyone can read it.
 *
 * Smoothness 5 asks for 4–10 updates a second, not one per file: a cold-cache
 * Maven build writes thousands of lines, and a setState per line is thousands of
 * renders nobody sees. This collects what arrives and hands it over on a beat.
 *
 * Not a debounce: a debounce would show nothing at all while output keeps
 * coming, which is precisely the case that needs to look alive. The first item
 * flushes immediately so the log starts moving in the frame after the click,
 * and everything after it waits for the beat.
 */
export const PROGRESS_INTERVAL_MS = 125;

export type ProgressBuffer<T> = {
  /** Records one item; the sink runs on the next beat. */
  push: (item: T) => void;
  /** Flushes anything pending and stops the timer. */
  dispose: () => void;
};

export function createProgressBuffer<T>(
  flush: (items: T[]) => void,
  intervalMs: number = PROGRESS_INTERVAL_MS,
): ProgressBuffer<T> {
  let pending: T[] = [];
  let timer: ReturnType<typeof setTimeout> | null = null;

  const drain = () => {
    if (pending.length === 0) return;
    const items = pending;
    pending = [];
    flush(items);
  };

  const onBeat = () => {
    timer = null;
    drain();
  };

  return {
    push: (item) => {
      pending.push(item);
      // A beat is already running: this item rides it.
      if (timer !== null) return;
      // Start of a burst — show it now, then open the window for the rest.
      drain();
      timer = setTimeout(onBeat, intervalMs);
    },
    dispose: () => {
      if (timer !== null) clearTimeout(timer);
      timer = null;
      drain();
    },
  };
}
