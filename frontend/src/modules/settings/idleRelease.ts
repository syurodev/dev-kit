/**
 * How long an agent session may sit idle before its process is released
 * (internal/preferences/idlerelease.go holds the same range and has the last word).
 */
export const IDLE_MIN_MINUTES = 2;
export const IDLE_MAX_MINUTES = 24 * 60;

export type ParsedMinutes = { ok: true; value: number } | { ok: false; problem: string };

/** What the person typed: a whole number of minutes inside the range. */
export function parseMinutes(text: string): ParsedMinutes {
  const trimmed = text.trim();
  if (!/^\d+$/.test(trimmed)) return { ok: false, problem: "Use a whole number of minutes." };
  const value = Number(trimmed);
  if (value < IDLE_MIN_MINUTES) return { ok: false, problem: `At least ${IDLE_MIN_MINUTES} minutes.` };
  if (value > IDLE_MAX_MINUTES) return { ok: false, problem: `At most ${IDLE_MAX_MINUTES} minutes (24 hours).` };
  return { ok: true, value };
}

/** "10 minutes", "1 hour", "1 hour 30 minutes": the stored value as a person reads it. */
export function minutesText(minutes: number): string {
  const h = Math.floor(minutes / 60);
  const m = minutes % 60;
  const hours = h === 0 ? "" : `${h} hour${h === 1 ? "" : "s"}`;
  const mins = m === 0 ? "" : `${m} minute${m === 1 ? "" : "s"}`;
  return [hours, mins].filter(Boolean).join(" ") || "0 minutes";
}
