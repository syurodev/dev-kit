import * as React from "react";

/**
 * What a closing panel keeps showing while it closes.
 *
 * A panel's content used to unmount on the first frame of its close, so what
 * slid out was an empty card. Now the last thing it showed stays for the
 * close's perceptual duration (lib/panelMotion.panelCloseMs) and then goes —
 * on a timer, never on an animation-end event that an interrupted animation
 * does not send (smoothness §10).
 */
export type Kept<V> = Record<string, { value: V; until: number }>;

/** The kept entries after `live` replaced `prevLive` at `now`. */
export function retainStep<V>(
  prevLive: Record<string, V>,
  live: Record<string, V>,
  kept: Kept<V>,
  now: number,
  ms: number,
): Kept<V> {
  const out: Kept<V> = {};
  // Still leaving from an earlier close, unless it came back.
  for (const [key, entry] of Object.entries(kept)) {
    if (!(key in live) && entry.until > now) out[key] = entry;
  }
  if (ms <= 0) return out;
  for (const [key, value] of Object.entries(prevLive)) {
    if (!(key in live)) out[key] = { value, until: now + ms };
  }
  return out;
}

/** `kept` without what has run out by `now`; the same object when nothing has. */
export function expireKept<V>(kept: Kept<V>, now: number): Kept<V> {
  const entries = Object.entries(kept);
  const left = entries.filter(([, entry]) => entry.until > now);
  return left.length === entries.length ? kept : Object.fromEntries(left);
}

/** Live entries, and the kept ones under them. */
export function shownRecord<V>(live: Record<string, V>, kept: Kept<V>): Record<string, V> {
  const out: Record<string, V> = {};
  for (const [key, entry] of Object.entries(kept)) out[key] = entry.value;
  return Object.assign(out, live);
}

/**
 * `live`, plus each entry that left it in the last `ms`.
 *
 * `live` must keep its identity between renders that did not change it (memo
 * it): a change is what starts a close, and a fresh object every render would
 * read as one.
 */
export function useRetainedRecord<V>(live: Record<string, V>, ms: number): Record<string, V> {
  const [state, setState] = React.useState<{ live: Record<string, V>; kept: Kept<V> }>(() => ({ live, kept: {} }));
  let current = state;
  if (live !== state.live) {
    // Derived during render (React's "storing information from previous
    // renders"), so the frame that closes a panel still draws its content.
    current = { live, kept: retainStep(state.live, live, state.kept, Date.now(), ms) };
    setState(current);
  }

  const kept = current.kept;
  React.useEffect(() => {
    const untils = Object.values(kept).map((entry) => entry.until);
    if (untils.length === 0) return;
    const first = Math.min(...untils);
    const timer = setTimeout(
      // At least up to the entry this timer was set for, so a timer that
      // fires a millisecond early by the wall clock still makes progress.
      () => setState((s) => (s.kept === kept ? { ...s, kept: expireKept(s.kept, Math.max(Date.now(), first)) } : s)),
      Math.max(0, first - Date.now()),
    );
    return () => clearTimeout(timer);
  }, [kept]);

  return React.useMemo(() => shownRecord(current.live, kept), [current.live, kept]);
}

const NOTHING: Record<string, never> = {};

/** One value's version of useRetainedRecord: `live` while there is one, then the last for `ms`. */
export function useRetainedWhileClosing<T>(live: T | null, ms: number): T | null {
  const record = React.useMemo<Record<string, T>>(() => (live === null ? NOTHING : { value: live }), [live]);
  return useRetainedRecord(record, ms).value ?? null;
}
