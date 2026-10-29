import type { LimitWindow, Usage } from "./types";

/**
 * The session's usage once `incoming` arrives on top of `previous`. Mirrors
 * internal/agenthub/usage_limits.go: mergeUsage, which the recorder applies to
 * the stored row; this one applies to the live window and to loaded pages.
 *
 * The context counter, cost and tokens are the incoming update's: they
 * describe the latest request. The plan's windows are not: Claude puts them on
 * some usage_updates only, and a window means "this much of the plan is used
 * until it resets", so an update that does not repeat it has not withdrawn it.
 * A window the update names (by label) replaces the old one, one it does not
 * stays, and one whose reset time has passed goes: its usage started over.
 *
 * A rate-limit verdict follows the same rule, except a rejected one: that is an
 * event on the turn it stopped, not a standing state, and carrying it would
 * keep the stopped-session banner up after the plan lets turns through again.
 */
export function mergeUsage(previous: Usage | null | undefined, incoming: Usage, now = Date.now()): Usage {
  const merged: Usage = { ...incoming };
  if (!previous) return merged;

  const live = (resetsAt?: number) => !resetsAt || resetsAt * 1000 > now;
  const carried = (previous.limits ?? []).filter((w) => live(w.resetsAt));
  const named = incoming.limits ?? [];
  let limits: LimitWindow[] = carried.map((w) => ({ ...w }));
  if (named.length > 0) {
    limits = carried.map((w) => ({ ...(named.find((n) => n.label === w.label) ?? w) }));
    for (const n of named) {
      if (!carried.some((w) => w.label === n.label)) limits.push({ ...n });
    }
  }
  if (limits.length > 0) merged.limits = limits;
  else delete merged.limits;

  const rate = previous.rateLimit;
  if (!incoming.rateLimit && rate && rate.status !== "rejected" && live(rate.resetsAt)) merged.rateLimit = { ...rate };
  return merged;
}
