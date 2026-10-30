import type { Usage } from "./types";

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
 * Two exceptions. A window named without a reading (noReading: a rate-limit
 * block with no utilization) keeps the percent the label had and takes only the
 * status and reset time. And a window with no reset time is carried only while
 * the update says nothing about windows: nothing says when it stops being
 * true, so one the vendor stops sending, or relabels, would stay for good.
 *
 * A rate-limit verdict follows the same rule, except a rejected one: that is an
 * event on the turn it stopped, not a standing state, and carrying it would
 * keep the stopped-session banner up after the plan lets turns through again.
 */
export function mergeUsage(previous: Usage | null | undefined, incoming: Usage, now = Date.now()): Usage {
  const merged: Usage = { ...incoming };
  if (!previous) return merged;

  const named = incoming.limits ?? [];
  const brings = named.length > 0;
  // Live: its reset is ahead, or it names none and nothing newer replaces it.
  const live = (resetsAt?: number) => (resetsAt ? resetsAt * 1000 > now : !brings);
  const carried = (previous.limits ?? []).filter((w) => live(w.resetsAt));

  let limits = carried.map((w) => ({ ...w }));
  if (brings) {
    limits = carried.map((w) => {
      const replacement = named.find((n) => n.label === w.label);
      if (!replacement) return { ...w };
      if (!replacement.noReading) return { ...replacement };
      const { noReading: _, ...rest } = replacement;
      return { ...rest, usedPercent: w.usedPercent, ...(w.noReading ? { noReading: true } : {}) };
    });
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

/**
 * `previous` after a usage_update tagged with a subagent's origin. Its context
 * counter, cost and tokens describe the subagent's own context window and are
 * not taken; its plan windows are, because a subagent spends the same plan.
 * `previous` itself when it carries none. With no usage before it, the result
 * holds the windows alone. Mirrors mergeSubagentUsage in usage_limits.go.
 */
export function mergeSubagentUsage(previous: Usage | null | undefined, incoming: Usage, now = Date.now()): Usage | null {
  if (!incoming.limits?.length && !incoming.rateLimit) return previous ?? null;
  const windows: Usage = previous
    ? { ...previous, limits: incoming.limits, rateLimit: incoming.rateLimit }
    : { used: 0, size: 0, amount: 0, limits: incoming.limits, rateLimit: incoming.rateLimit };
  return mergeUsage(previous, windows, now);
}
