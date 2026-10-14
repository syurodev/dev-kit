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
  // A ref, not state: derived during render, so the frame that closes a panel
  // still draws its content, without the extra render a state update during
  // render costs. Idempotent per `live`, so a StrictMode double render agrees.
  const state = React.useRef<{ live: Record<string, V>; kept: Kept<V> }>({ live, kept: {} });
  const [, rerender] = React.useReducer((n: number) => n + 1, 0);
  if (live !== state.current.live) {
    state.current = { live, kept: retainStep(state.current.live, live, state.current.kept, Date.now(), ms) };
  }

  const kept = state.current.kept;
  React.useEffect(() => {
    const untils = Object.values(kept).map((entry) => entry.until);
    if (untils.length === 0) return;
    const first = Math.min(...untils);
    const timer = setTimeout(() => {
      if (state.current.kept !== kept) return;
      // At least up to the entry this timer was set for, so a timer that
      // fires a millisecond early by the wall clock still makes progress.
      state.current = { ...state.current, kept: expireKept(kept, Math.max(Date.now(), first)) };
      rerender();
    }, Math.max(0, first - Date.now()));
    return () => clearTimeout(timer);
  }, [kept]);

  return React.useMemo(() => shownRecord(live, kept), [live, kept]);
}

const NOTHING: Record<string, never> = {};

/** One value's version of useRetainedRecord: `live` while there is one, then the last for `ms`. */
export function useRetainedWhileClosing<T>(live: T | null, ms: number): T | null {
  const record = React.useMemo<Record<string, T>>(() => (live === null ? NOTHING : { value: live }), [live]);
  return useRetainedRecord(record, ms).value ?? null;
}

/**
 * The kept panes a closing container may still draw: not one on screen
 * elsewhere now, and not one that was at any point since it started closing.
 * Without the second rule a pane moved out (column → bottom row) and then
 * closed within the close would be drawn by both closing containers at once,
 * two cards fighting over one host. `seen` is the caller's to keep for the
 * length of one close.
 */
export function stillLeaving<K extends string>(
  kept: Record<K, string | null>,
  onScreen: ReadonlySet<string>,
  seen: ReadonlySet<string>,
): { shown: Record<K, string | null>; seen: Set<string> } {
  const nextSeen = new Set(seen);
  for (const pane of Object.values(kept) as (string | null)[]) if (pane !== null && onScreen.has(pane)) nextSeen.add(pane);
  const shown = {} as Record<K, string | null>;
  for (const key of Object.keys(kept) as K[]) {
    const pane = kept[key];
    shown[key] = pane !== null && !nextSeen.has(pane) ? pane : null;
  }
  return { shown, seen: nextSeen };
}

/**
 * Per slot of a container, the pane resting there: one kept alive while
 * hidden (`alive`: the Agent companion) that this container drew at that
 * slot last time (`drawn`), that nothing has taken the slot from (`live`)
 * and that is not on screen anywhere (`onScreen`). Once let go it is gone:
 * the next step's `drawn` no longer has it.
 *
 * Its card stays where it was — in a closed container the way a closed
 * sidebar keeps its rail, stowed beside a pane still open in the same column
 * — so its host never moves to the park and back. A move re-parents the whole
 * conversation, which an engine without `moveBefore` (WebKit) tears down and
 * rebuilds: measured on WebKitGTK, 70-90 ms on the first frame of every open
 * and again at the end of every close (smoothness §3.8).
 */
export function restingStep<K extends string>(
  drawn: Partial<Record<K, string | null>>,
  live: Partial<Record<K, string | null>>,
  alive: ReadonlySet<string>,
  onScreen: ReadonlySet<string>,
): Partial<Record<K, string>> {
  const out: Partial<Record<K, string>> = {};
  for (const key of Object.keys(drawn) as K[]) {
    const pane = drawn[key];
    if (pane && alive.has(pane) && !onScreen.has(pane) && !live[key]) out[key] = pane;
  }
  return out;
}

/**
 * Which resting slots are stowed — out of the flow, invisible, at the
 * container's size — rather than laid out where they were. A resting pane
 * alone in its container stays in the flow: the container is closed and
 * clips it, and leaving it untouched costs nothing. Beside a pane drawn in
 * the same container (open, or still closing) it is stowed so that one gets
 * the whole column; and once stowed it stays so while it rests, so it never
 * drops back into the bottom half of a column that is closing.
 */
export function stowStep<K extends string>(prev: ReadonlySet<K>, rest: ReadonlySet<K>, others: ReadonlySet<K>): Set<K> {
  const out = new Set<K>();
  for (const key of rest) if (others.size > 0 || prev.has(key)) out.add(key);
  return out;
}

/**
 * `shown` (what a container shows open or while closing) plus the panes
 * resting in it (restingStep), and which of those are stowed (stowStep).
 */
export function useResting<K extends string>(
  shown: Record<K, string | null> | null,
  alive: ReadonlySet<string>,
  onScreen: readonly (string | undefined)[],
): { drawn: Partial<Record<K, string | null>>; stowed: ReadonlySet<K> } {
  // Derived during render, like useStillLeaving: idempotent per input, so a
  // StrictMode double render agrees.
  const drawn = React.useRef<Partial<Record<K, string | null>>>({});
  const stowed = React.useRef<ReadonlySet<K>>(new Set());
  const live: Partial<Record<K, string | null>> = shown ?? {};
  const resting = restingStep(drawn.current, live, alive, new Set(onScreen.filter((p): p is string => p !== undefined)));
  const next: Partial<Record<K, string | null>> = { ...resting };
  for (const key of Object.keys(live) as K[]) if (live[key]) next[key] = live[key];
  drawn.current = next;
  const rest = new Set(Object.keys(resting) as K[]);
  const others = new Set((Object.keys(live) as K[]).filter((key) => live[key]));
  stowed.current = stowStep(stowed.current, rest, others);
  return { drawn: next, stowed: stowed.current };
}

/**
 * stillLeaving for one closing container:`closing` is what it kept (null
 * while it is open, which also starts the next close afresh), `onScreen` the
 * panes drawn anywhere right now.
 */
export function useStillLeaving<K extends string>(
  closing: Record<K, string | null> | null,
  onScreen: readonly (string | undefined)[],
): Record<K, string | null> | null {
  const seen = React.useRef<ReadonlySet<string>>(new Set());
  if (closing === null) {
    if (seen.current.size > 0) seen.current = new Set();
    return null;
  }
  const result = stillLeaving(closing, new Set(onScreen.filter((p): p is string => p !== undefined)), seen.current);
  seen.current = result.seen;
  return result.shown;
}
