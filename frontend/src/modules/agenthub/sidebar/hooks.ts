import * as React from "react";
import { NARROW_RAIL_WIDTH } from "./model";

/** How long a "just started waiting" effect holds its class: flash 1.4s, then 3 pulses of 0.64s. */
export const ATTENTION_EFFECT_MS = 2200;

/**
 * True for a moment after `value` goes UP — a session that started waiting, a
 * group whose waiting count grew.
 *
 * Only a change seen while mounted counts. A card that mounts already waiting
 * (a group opened, a stack unfolded, the rail reopened) plays nothing, and
 * neither does a rerender or a selection change: the effect is "this just
 * happened", and the standing signal for "this is waiting" is the bar and the
 * icon. The timer is not tied to the effect's cleanup, or the next unrelated
 * change would cancel it and leave the class on for good.
 */
export function useRisingEdge(value: number | boolean): boolean {
  const level = Number(value);
  const previous = React.useRef(level);
  const timer = React.useRef<ReturnType<typeof setTimeout> | null>(null);
  const [active, setActive] = React.useState(false);

  React.useEffect(() => {
    const rose = level > previous.current;
    previous.current = level;
    if (!rose) return;
    if (timer.current) clearTimeout(timer.current);
    setActive(true);
    timer.current = setTimeout(() => {
      timer.current = null;
      setActive(false);
    }, ATTENTION_EFFECT_MS);
  }, [level]);

  React.useEffect(
    () => () => {
      if (timer.current) clearTimeout(timer.current);
    },
    [],
  );
  return active;
}

/**
 * The time, rounded down to the minute and refreshed once a minute: the rail
 * shows ages in minutes, so a finer tick would rerender every card for
 * nothing.
 */
export function useMinuteClock(): number {
  const minute = () => Math.floor(Date.now() / 60_000) * 60_000;
  const [now, setNow] = React.useState(minute);
  React.useEffect(() => {
    const id = setInterval(() => setNow(minute()), 60_000);
    return () => clearInterval(id);
  }, []);
  return now;
}

/**
 * Whether the rail is narrow, from the width its own content box is given. The
 * shell owns the width and lets the person drag it; the rail only reads it, to
 * decide what it can still afford to say. Wide until measured, which is also
 * what server rendering shows.
 */
export function useNarrowRail(ref: React.RefObject<HTMLElement | null>): boolean {
  const [narrow, setNarrow] = React.useState(false);
  React.useLayoutEffect(() => {
    const el = ref.current;
    if (!el || typeof ResizeObserver === "undefined") return;
    const read = (width: number) => setNarrow(width > 0 && width < NARROW_RAIL_WIDTH);
    read(el.getBoundingClientRect().width);
    const observer = new ResizeObserver((entries) => {
      const box = entries[0]?.contentRect;
      if (box) read(box.width);
    });
    observer.observe(el);
    return () => observer.disconnect();
  }, [ref]);
  return narrow;
}
