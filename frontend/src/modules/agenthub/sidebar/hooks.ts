import * as React from "react";

/** How long a "just started waiting" effect holds its class: flash 1.4s, then 3 pulses of 0.64s. */
export const ATTENTION_EFFECT_MS = 2200;

/**
 * True for a moment after `value` goes UP — the composer tray's count of
 * permissions and questions grew.
 *
 * Only a change seen while mounted counts. A tray that mounts with something
 * already waiting plays nothing, and neither does a rerender: the effect is
 * "this just happened", and the standing signal for "this is waiting" is
 * whatever stays drawn (the tray's chip, the dot on a session's face). The
 * timer is not tied to the effect's cleanup, or the next unrelated change
 * would cancel it and leave the class on for good.
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
