import type { AccountPolicy, VendorPolicy } from "@/bridge/agentAccounts";

/**
 * The switching rules, as the form edits them. The limits repeat what the
 * backend enforces (internal/preferences/accountpolicy.go) so a wrong value is
 * told in words before a save is refused, never instead of it.
 */

export const THRESHOLD_MIN = 50;
export const THRESHOLD_MAX = 100;
export const THRESHOLD_DEFAULT = 90;
export const MAX_AGE_MIN = 60;
export const MAX_AGE_MAX = 3600;
export const MAX_AGE_DEFAULT = 300;
/** The most account ids the order can hold. */
export const ORDER_MAX = 32;

export type Refusal = "none" | "resendIfNoProgress" | "continue";

/** What to do with a turn the vendor refused at a real limit, and what each choice does. */
export const REFUSAL_CHOICES: readonly { value: Refusal; label: string; detail: string }[] = [
  {
    value: "none",
    label: "Do not resend (default)",
    detail: "A turn the vendor refused stays refused. Your next message starts on the next account.",
  },
  {
    value: "resendIfNoProgress",
    label: "Resend if it had not started",
    detail:
      "If the refused turn had produced no text and run no tool, DevKit sends the same message once on the next account.",
  },
  {
    value: "continue",
    label: "Send a continue message",
    detail:
      "DevKit sends one fixed message asking the agent to continue on the next account, even if the refused turn had made progress.",
  },
];

export function refusalOf(value: string): Refusal {
  return value === "resendIfNoProgress" || value === "continue" ? value : "none";
}

/** A vendor's rules, with the defaults filled in where nothing is stored (zero means "the default"). */
export function vendorPolicyOf(policy: AccountPolicy | undefined, vendor: string): VendorPolicy {
  const stored = policy?.vendors[vendor];
  return {
    enabled: stored?.enabled ?? false,
    thresholdPercent: stored?.thresholdPercent || THRESHOLD_DEFAULT,
    weeklyThresholdPercent: stored?.weeklyThresholdPercent ?? 0,
    usageMaxAgeSeconds: stored?.usageMaxAgeSeconds || MAX_AGE_DEFAULT,
    order: [...(stored?.order ?? [])],
    onLimitRefusal: refusalOf(stored?.onLimitRefusal ?? ""),
  };
}

/** The form's fields: text for the numbers, so a half-typed value is not a number yet. */
export type PolicyFields = {
  enabled: boolean;
  threshold: string;
  /** Empty means "the same as the threshold". */
  weekly: string;
  maxAge: string;
  refusal: Refusal;
};

export function fieldsOf(policy: VendorPolicy): PolicyFields {
  return {
    enabled: policy.enabled,
    threshold: String(policy.thresholdPercent),
    weekly: policy.weeklyThresholdPercent ? String(policy.weeklyThresholdPercent) : "",
    maxAge: String(policy.usageMaxAgeSeconds),
    refusal: refusalOf(policy.onLimitRefusal),
  };
}

export type ParsedFields =
  | { ok: true; value: Omit<VendorPolicy, "order"> }
  | { ok: false; field: "threshold" | "weekly" | "maxAge"; problem: string };

const WHOLE = /^\d+$/;
const PERCENT_PROBLEM = `Use a whole number from ${THRESHOLD_MIN} to ${THRESHOLD_MAX}.`;

function inRange(text: string, min: number, max: number): number | null {
  const trimmed = text.trim();
  if (!WHOLE.test(trimmed)) return null;
  const value = Number(trimmed);
  return value >= min && value <= max ? value : null;
}

export function parseFields(fields: PolicyFields): ParsedFields {
  const threshold = inRange(fields.threshold, THRESHOLD_MIN, THRESHOLD_MAX);
  if (threshold === null) return { ok: false, field: "threshold", problem: PERCENT_PROBLEM };
  let weekly = 0;
  if (fields.weekly.trim() !== "") {
    const parsed = inRange(fields.weekly, THRESHOLD_MIN, THRESHOLD_MAX);
    if (parsed === null) return { ok: false, field: "weekly", problem: `${PERCENT_PROBLEM} Leave it empty to use the same as above.` };
    weekly = parsed;
  }
  const maxAge = inRange(fields.maxAge, MAX_AGE_MIN, MAX_AGE_MAX);
  if (maxAge === null) {
    return { ok: false, field: "maxAge", problem: `Use a whole number of seconds from ${MAX_AGE_MIN} to ${MAX_AGE_MAX}.` };
  }
  return {
    ok: true,
    value: {
      enabled: fields.enabled,
      thresholdPercent: threshold,
      weeklyThresholdPercent: weekly,
      usageMaxAgeSeconds: maxAge,
      onLimitRefusal: fields.refusal,
    },
  };
}

/** What the two thresholds mean for a session, in a sentence; nothing while a field is not a number yet. */
export function thresholdMeaning(parsed: ParsedFields): string {
  if (!parsed.ok) return "";
  const { thresholdPercent, weeklyThresholdPercent } = parsed.value;
  const base = `A session moves to the next signed-in account at its next turn once its account passes ${thresholdPercent}% of a window`;
  return weeklyThresholdPercent ? `${base}, or ${weeklyThresholdPercent}% of a weekly one.` : `${base}.`;
}

/** The whole preference with one vendor's rules replaced; the other vendors and the revision are untouched. */
export function withVendorPolicy(policy: AccountPolicy, vendor: string, next: VendorPolicy): AccountPolicy {
  return { ...policy, vendors: { ...policy.vendors, [vendor]: { ...next, order: next.order.slice(0, ORDER_MAX) } } };
}

/** Turning switching on is refused until the terms warning was accepted once. */
export function needsRiskAcknowledgement(policy: Pick<AccountPolicy, "riskAcknowledgedAt"> | undefined): boolean {
  return !policy?.riskAcknowledgedAt?.trim();
}

/** Whether the form differs from what is stored, field by field as the person wrote them. */
export function fieldsDiffer(a: PolicyFields, b: PolicyFields): boolean {
  return (
    a.enabled !== b.enabled ||
    a.threshold.trim() !== b.threshold.trim() ||
    a.weekly.trim() !== b.weekly.trim() ||
    a.maxAge.trim() !== b.maxAge.trim() ||
    a.refusal !== b.refusal
  );
}

/** The one-line truth shown beside the switch and in the terms dialog. */
export const SWITCHING_TRUTH =
  "Switching happens only at the start of a new turn: a turn that is already running is never moved. Each account signs in through the agent's own login, in its own folder, so DevKit never holds a token.";

/** The terms warning. Short and factual: DevKit makes no claim that any of this is permitted. */
export const TERMS_WARNING =
  "Some providers' terms restrict how subscription logins may be used, including holding several accounts or sharing one login. DevKit cannot tell what yours allow: use only accounts you are entitled to use, and check each provider's terms and your plan's.";
