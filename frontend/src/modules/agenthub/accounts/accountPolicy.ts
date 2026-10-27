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

export type Strategy = "order" | "best" | "consumeFirst";

/** How the account a session moves to is chosen. The thresholds alone decide WHETHER it moves. */
export const STRATEGY_CHOICES: readonly { value: Strategy; label: string; detail: string }[] = [
  {
    value: "order",
    label: "My order (default)",
    detail: "The first account in the list above that is signed in and under its limit.",
  },
  {
    value: "best",
    label: "Most room left",
    detail: "The account whose fullest window is the emptiest. Accounts with no reading yet come after those with one.",
  },
  {
    value: "consumeFirst",
    label: "Renews soonest",
    detail: "The account whose weekly window resets first, so quota that is about to renew is spent before the rest.",
  },
];

export function strategyOf(value: string): Strategy {
  return value === "best" || value === "consumeFirst" ? value : "order";
}

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
    strategy: strategyOf(stored?.strategy ?? ""),
    accountThresholds: { ...stored?.accountThresholds },
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
  strategy: Strategy;
  /** Each account's own limit by id, as typed; empty or absent means "the same as the threshold". */
  limits: Record<string, string>;
};

export function fieldsOf(policy: VendorPolicy): PolicyFields {
  return {
    enabled: policy.enabled,
    threshold: String(policy.thresholdPercent),
    weekly: policy.weeklyThresholdPercent ? String(policy.weeklyThresholdPercent) : "",
    maxAge: String(policy.usageMaxAgeSeconds),
    refusal: refusalOf(policy.onLimitRefusal),
    strategy: strategyOf(policy.strategy),
    limits: Object.fromEntries(Object.entries(policy.accountThresholds).map(([id, limit]) => [id, String(limit)])),
  };
}

export type ParsedFields =
  | { ok: true; value: Omit<VendorPolicy, "order"> }
  | { ok: false; field: "threshold" | "weekly" | "maxAge" | `limit:${string}`; problem: string };

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
  const accountThresholds: Record<string, number> = {};
  for (const [id, text] of Object.entries(fields.limits)) {
    if (text.trim() === "") continue;
    const limit = inRange(text, THRESHOLD_MIN, THRESHOLD_MAX);
    if (limit === null) return { ok: false, field: `limit:${id}`, problem: `${PERCENT_PROBLEM} Leave it empty to use the same as above.` };
    accountThresholds[id] = limit;
  }
  return {
    ok: true,
    value: {
      enabled: fields.enabled,
      thresholdPercent: threshold,
      weeklyThresholdPercent: weekly,
      usageMaxAgeSeconds: maxAge,
      onLimitRefusal: fields.refusal,
      strategy: fields.strategy,
      accountThresholds,
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

/**
 * Whether a save carries `acknowledgeRisk`: only when the person accepted the
 * warning, switching is being turned on in this very save, and the terms were
 * not already acknowledged (a stored acknowledgement is kept without being sent again).
 */
export function acknowledgeFor(accepted: boolean, enabled: boolean, riskAcknowledged: boolean): boolean {
  return accepted && enabled && !riskAcknowledged;
}

/**
 * Where the switch and the terms dialog stand: whether switching is on in the
 * form, whether the dialog is open, and whether the person accepted the terms
 * in this sitting (so the next save carries it, and turning the switch on again
 * asks nothing). Pure so every transition can be read and tested; a failed save
 * makes no transition, so `accepted` survives it.
 */
export type RiskFlow = { enabled: boolean; askRisk: boolean; accepted: boolean };

export const riskFlow = {
  initial: (enabled: boolean): RiskFlow => ({ enabled, askRisk: false, accepted: false }),
  /** The switch was flipped. Turning it on asks first when the terms were never accepted; the switch stays where it was until confirmed. */
  toggle: (state: RiskFlow, on: boolean, riskAcknowledged: boolean): RiskFlow =>
    on && !riskAcknowledged && !state.accepted ? { ...state, askRisk: true } : { ...state, enabled: on },
  /** "Turn on" in the dialog. */
  confirm: (): RiskFlow => ({ enabled: true, askRisk: false, accepted: true }),
  /** Cancel in the dialog: the switch stays where it was. */
  cancel: (state: RiskFlow): RiskFlow => ({ ...state, askRisk: false }),
  /** What is stored moved under the form (a save elsewhere, the first read): start from it, forget an acceptance, keep an open dialog. */
  storedChanged: (state: RiskFlow, enabled: boolean): RiskFlow => ({ enabled, askRisk: state.askRisk, accepted: false }),
  /** The save was stored: what was accepted is now the stored acknowledgement. */
  saved: (state: RiskFlow): RiskFlow => ({ ...state, accepted: false }),
};

/** Two sets of typed limits are the same when each id holds the same text; an empty text is no limit. */
function limitsDiffer(a: Record<string, string>, b: Record<string, string>): boolean {
  const text = (limits: Record<string, string>, id: string) => (limits[id] ?? "").trim();
  return [...new Set([...Object.keys(a), ...Object.keys(b)])].some((id) => text(a, id) !== text(b, id));
}

/** Whether the form differs from what is stored, field by field as the person wrote them. */
export function fieldsDiffer(a: PolicyFields, b: PolicyFields): boolean {
  return (
    a.enabled !== b.enabled ||
    a.threshold.trim() !== b.threshold.trim() ||
    a.weekly.trim() !== b.weekly.trim() ||
    a.maxAge.trim() !== b.maxAge.trim() ||
    a.refusal !== b.refusal ||
    a.strategy !== b.strategy ||
    limitsDiffer(a.limits, b.limits)
  );
}

/** The one-line truth shown beside the switch and in the terms dialog. */
export const SWITCHING_TRUTH =
  "Switching happens only at the start of a new turn: a turn that is already running is never moved. Each account signs in through the agent's own login, so DevKit never holds a token.";

/** The terms warning. Short and factual: DevKit makes no claim that any of this is permitted. */
export const TERMS_WARNING =
  "Some providers' terms restrict how subscription logins may be used, and some may treat rotating accounts to avoid a limit, or sharing one login, as a violation. DevKit cannot tell what yours allow: use only accounts you are entitled to use, and check each provider's terms and your plan's.";
