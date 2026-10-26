import { Events } from "@wailsio/runtime";
import { AppService } from "../../bindings/devkit/internal/bridge";
import type {
  AgentAccountInfo as RawAccount,
  AgentAccountPolicyState as RawPolicy,
  AgentAccountVendorPolicyState as RawVendorPolicy,
} from "../../bindings/devkit/internal/bridge/models";
import type { SignInFlow } from "../../bindings/devkit/internal/agenthub/models";
import { callBridge } from "./client";

export type { SignInFlow };

/**
 * Several accounts per agent (Settings → Agents → Accounts & limits).
 *
 * Every method here is the window's own: there is no MCP tool for any of them,
 * the backend refuses an agent caller, and only the list and the policy read
 * work in server mode. An account is a row — the agent's own login lives in the
 * account's home folder, and neither a credential nor that folder's path ever
 * reaches the window.
 */

/** One usage ceiling an account has: a window that resets. `resetsAt` is Unix seconds, 0 when unknown. */
export type AccountWindow = {
  label: string;
  usedPercent: number;
  resetsAt: number;
  /** "ok", "warning" or "reached"; "" when the vendor gave none. */
  status: string;
};

/**
 * One vendor account as Settings draws it. Times are Unix seconds, 0 for none.
 * `exhaustedUntil` and `cooldownUntil` can be past values — nothing clears them
 * when time passes — so a time at or before now means "not limited".
 */
export type AgentAccount = {
  usageGeneration?: string;
  id: string;
  vendor: string;
  label: string;
  /** The machine's own login (Claude) or DevKit's own home (Codex): fixed name, never removed. */
  builtin: boolean;
  signedIn: boolean;
  /** Other people also use this login, so what DevKit saw of its usage is a lower bound. */
  shared: boolean;
  exhaustedUntil: number;
  cooldownUntil: number;
  usage: AccountWindow[];
  /** "observed" (seen in a session's own traffic), "checked" (asked of the vendor) or "". */
  usageSource: string;
  observedAt: number;
};

/** One vendor's switching rules. A zero threshold or max age in storage means "the default". */
export type VendorPolicy = {
  enabled: boolean;
  thresholdPercent: number;
  /** 0 = the same as `thresholdPercent`. */
  weeklyThresholdPercent: number;
  usageMaxAgeSeconds: number;
  /** Account ids, first used first; may name an account that was removed. */
  order: string[];
  /** "none", "resendIfNoProgress" or "continue". */
  onLimitRefusal: string;
};

/** The whole rules preference: `revision` is the compare-and-swap token. */
export type AccountPolicy = {
  revision: number;
  /** RFC 3339, written by DevKit when the person accepted the terms warning; "" until then. */
  riskAcknowledgedAt: string;
  vendors: Record<string, VendorPolicy>;
};

/** The bindings type `usage` and `order` as nullable though Go sends `[]`: normalised here, once. */
export function accountOf(raw: RawAccount): AgentAccount {
  return {
    id: raw.id,
    usageGeneration: raw.usageGeneration ?? "",
    vendor: raw.vendor,
    label: raw.label,
    builtin: raw.builtin,
    signedIn: raw.signedIn,
    shared: raw.shared,
    exhaustedUntil: raw.exhaustedUntil ?? 0,
    cooldownUntil: raw.cooldownUntil ?? 0,
    usage: (raw.usage ?? []).map((window) => ({
      label: window.label,
      usedPercent: window.usedPercent,
      resetsAt: window.resetsAt ?? 0,
      status: window.status ?? "",
    })),
    usageSource: raw.usageSource ?? "",
    observedAt: raw.observedAt ?? 0,
  };
}

function vendorPolicyOf(raw: RawVendorPolicy): VendorPolicy {
  return {
    enabled: raw.enabled,
    thresholdPercent: raw.thresholdPercent,
    weeklyThresholdPercent: raw.weeklyThresholdPercent,
    usageMaxAgeSeconds: raw.usageMaxAgeSeconds,
    order: [...(raw.order ?? [])],
    onLimitRefusal: raw.onLimitRefusal,
  };
}

export function policyOf(raw: RawPolicy): AccountPolicy {
  const vendors: Record<string, VendorPolicy> = {};
  for (const [name, value] of Object.entries(raw.vendors ?? {})) {
    if (value) vendors[name] = vendorPolicyOf(value);
  }
  return { revision: raw.revision, riskAcknowledgedAt: raw.riskAcknowledgedAt ?? "", vendors };
}

/**
 * What crosses to Go. `riskAcknowledgedAt` is sent back as read and ignored
 * there: DevKit stamps it itself when `acknowledgeRisk` is true.
 */
export function rawPolicyOf(policy: AccountPolicy): RawPolicy {
  const vendors: Record<string, RawVendorPolicy> = {};
  for (const [name, value] of Object.entries(policy.vendors)) {
    vendors[name] = { ...value, order: [...value.order] };
  }
  return { revision: policy.revision, riskAcknowledgedAt: policy.riskAcknowledgedAt, vendors };
}

export function agentAccountsList(vendor: string): Promise<AgentAccount[]> {
  return callBridge(async () => ((await AppService.AgentAccountsList(vendor)) ?? []).map(accountOf));
}

/** Adds an account: its folder only. Nothing is signed in yet. */
export function agentAccountsAdd(vendor: string, label: string): Promise<AgentAccount> {
  return callBridge(async () => accountOf(await AppService.AgentAccountsAdd(vendor, label)));
}

export function agentAccountsRename(id: string, label: string): Promise<AgentAccount> {
  return callBridge(async () => accountOf(await AppService.AgentAccountsRename(id, label)));
}

/** Refused while a live session runs as the account; the sentence says so. */
export function agentAccountsRemove(id: string): Promise<void> {
  return callBridge(() => AppService.AgentAccountsRemove(id) as Promise<void>);
}

export function agentAccountsSetShared(id: string, shared: boolean): Promise<AgentAccount> {
  return callBridge(async () => accountOf(await AppService.AgentAccountsSetShared(id, shared)));
}

/** Re-reads whether the account is signed in. */
export function agentAccountsRefreshStatus(id: string): Promise<AgentAccount> {
  return callBridge(async () => accountOf(await AppService.AgentAccountsRefreshStatus(id)));
}

/**
 * Asks the vendor what the account has left. The backend limits how often that
 * runs per account and answers with what it already has when asked too soon —
 * no error, no flag — so compare `observedAt` before and after to tell.
 */
export function agentAccountsRefreshUsage(id: string): Promise<AgentAccount> {
  return callBridge(async () => accountOf(await AppService.AgentAccountsRefreshUsage(id)));
}

/** Claude's terminal sign-in at an added account's folder: the id of the terminal session to attach a tab to. */
export function agentAccountsStartSignIn(id: string, methodId: string, rows: number, cols: number): Promise<string> {
  return callBridge(() => AppService.AgentAccountsStartSignIn(id, methodId, rows, cols));
}

/** Codex's in-app sign-in at an added account's folder; it ends with agenthub:signInCompleted carrying `accountId`. */
export function agentAccountsStartBrowserSignIn(id: string): Promise<SignInFlow> {
  return callBridge(() => AppService.AgentAccountsStartBrowserSignIn(id));
}

export function agentAccountsStartDeviceSignIn(id: string): Promise<SignInFlow> {
  return callBridge(() => AppService.AgentAccountsStartDeviceSignIn(id));
}

export function agentAccountPolicyGet(): Promise<AccountPolicy> {
  return callBridge(async () => policyOf(await AppService.AgentAccountPolicyGet()));
}

/**
 * Replaces the rules when `policy.revision` is the stored one. `acknowledgeRisk`
 * is for an explicit click on the terms warning and nothing else; a stored
 * acknowledgement is kept on later saves.
 */
export function agentAccountPolicySet(policy: AccountPolicy, acknowledgeRisk: boolean): Promise<AccountPolicy> {
  return callBridge(async () => policyOf(await AppService.AgentAccountPolicySet(rawPolicyOf(policy), acknowledgeRisk)));
}

export const AGENT_ACCOUNTS_CHANGED_EVENT = "agentaccounts:changed";
export const AGENT_ACCOUNTS_SWITCHED_EVENT = "agentaccounts:switched";

/**
 * An account was added, renamed, removed, signed in, marked limited, or its usage
 * was read. No payload. Readings seen in a session's traffic are announced at
 * most once every two seconds with no trailing event, so the newest one can go
 * unannounced: refetch on window focus as well.
 */
export function onAgentAccountsChanged(cb: () => void): () => void {
  return Events.On(AGENT_ACCOUNTS_CHANGED_EVENT, () => cb());
}

/** A session moved to another account at the start of a turn. `from` and `to` are account ids. */
export type AccountSwitchedEvent = { sessionId: string; from: string; to: string; reason: string };

/** Reads the event's payload; anything malformed is not a switch. */
export function parseAccountSwitched(data: unknown): AccountSwitchedEvent | null {
  const record = (Array.isArray(data) ? data[0] : data) as Record<string, unknown> | null | undefined;
  if (!record || typeof record !== "object") return null;
  if (typeof record.sessionId !== "string" || typeof record.from !== "string" || typeof record.to !== "string") return null;
  if (!record.sessionId || !record.to) return null;
  return {
    sessionId: record.sessionId,
    from: record.from,
    to: record.to,
    reason: typeof record.reason === "string" ? record.reason : "",
  };
}

/** Fires once per move that stood, after the new process opened. Returns unsubscribe. */
export function onAgentAccountSwitched(handler: (event: AccountSwitchedEvent) => void): () => void {
  return Events.On(AGENT_ACCOUNTS_SWITCHED_EVENT, (ev) => {
    const parsed = parseAccountSwitched(ev?.data);
    if (parsed) handler(parsed);
  });
}
