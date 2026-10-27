import type { AgentAccount } from "@/bridge/agentAccounts";
import { CLAUDE_VENDOR, CODEX_VENDOR } from "@/bridge/agenthub";
import type { AccountNotice } from "../types";
import { resetsIn } from "../summary/summaryModel";

/**
 * The pure half of "Accounts & limits": names, order, usage rows and the words
 * for them. Nothing here fetches, so the settings page, the transcript notice
 * and the summary card read accounts the same way.
 */

/** The agents that can have several accounts. opencode keeps one login. */
export const ACCOUNT_VENDORS: readonly string[] = [CLAUDE_VENDOR, CODEX_VENDOR];

/** The longest an account's name can be; the backend refuses more. */
export const ACCOUNT_LABEL_MAX = 40;

/** The id of a vendor's built-in account. A session that runs as it may carry "" instead. */
export function builtinAccountId(vendor: string): string {
  return `${vendor}-default`;
}

export function isBuiltinAccountId(vendor: string, id: string): boolean {
  return id === "" || id === builtinAccountId(vendor);
}

/** What the built-in account is, in a sentence, for the page that lists it. */
export function builtinAccountHelp(vendor: string): string {
  return vendor === CLAUDE_VENDOR
    ? "System default is the login Claude Code already uses on this computer."
    : "DevKit default is the Codex sign-in DevKit keeps for its own sessions.";
}

/**
 * The accounts the person added. The built-in account is not drawn on the
 * Accounts page: the backend still lists it, ranks it and runs sessions on it,
 * but its login lives under Settings → Agents → Runtimes, so the page offers
 * nothing to do with it. Everything that names an account (notices, the
 * summary card) still reads the full list.
 */
export function addedAccounts<T extends { builtin: boolean }>(accounts: readonly T[] | undefined): T[] | undefined {
  return accounts?.filter((account) => !account.builtin);
}

/** Where the page tells the person what the built-in account is, now that it has no row. */
export function builtinAccountHiddenHelp(vendor: string): string {
  return vendor === CLAUDE_VENDOR
    ? "Sessions start on the login Claude Code already uses on this computer (sign in to it under Settings → Agents → Runtimes) and move to one of these accounts when it reaches a limit."
    : "Sessions start on the Codex sign-in DevKit keeps for its own sessions (sign in to it under Settings → Agents → Runtimes) and move to one of these accounts when it reaches a limit.";
}

/** What the built-in account is called when the list that names it is not at hand. */
export function builtinAccountLabel(vendor: string): string {
  return vendor === CLAUDE_VENDOR ? "System default" : "DevKit default";
}

/**
 * The words for an account id in a sentence ("Switched from … to …"). Built-in
 * ids always resolve; another id waits for the list, and one the list no longer
 * holds was removed.
 */
export function accountLabelOf(accounts: readonly AgentAccount[] | undefined, vendor: string, id: string): string {
  if (isBuiltinAccountId(vendor, id)) return accounts?.find((a) => a.builtin)?.label || builtinAccountLabel(vendor);
  if (!accounts) return "another account";
  return accounts.find((a) => a.id === id)?.label || "a removed account";
}

/**
 * The summary card's Account row: absent for a session on the built-in account
 * that never moved (its id is empty) and while the list that would name the
 * account has not arrived, so the row never shows a guess.
 */
export function accountLabelFor(
  accounts: readonly AgentAccount[] | undefined,
  vendor: string,
  id: string,
): string | undefined {
  if (!id) return undefined;
  if (!isBuiltinAccountId(vendor, id) && !accounts) return undefined;
  return accountLabelOf(accounts, vendor, id);
}

/** Moves `id` one place; an id that is not there, or already at the end, leaves the order as it was. */
export function moveId(order: readonly string[], id: string, delta: -1 | 1): string[] {
  const from = order.indexOf(id);
  const to = from + delta;
  if (from < 0 || to < 0 || to >= order.length) return [...order];
  const next = [...order];
  const [moved] = next.splice(from, 1);
  next.splice(to, 0, moved);
  return next;
}

/**
 * The accounts as the rules will try them: the ids the person ordered first (an
 * id of a removed account is skipped), then every other account in the order the
 * backend listed them, built-in first.
 */
export function orderedAccounts<T extends { id: string }>(accounts: readonly T[], order: readonly string[]): T[] {
  const byId = new Map(accounts.map((account) => [account.id, account] as const));
  const seen = new Set<string>();
  const out: T[] = [];
  for (const id of order) {
    const account = byId.get(id);
    if (account && !seen.has(id)) {
      seen.add(id);
      out.push(account);
    }
  }
  for (const account of accounts) if (!seen.has(account.id)) out.push(account);
  return out;
}

/** The order to save after moving one account, written out in full from what is on screen. */
export function movedOrder(accounts: readonly AgentAccount[], order: readonly string[], id: string, delta: -1 | 1): string[] {
  return moveId(orderedAccounts(accounts, order).map((account) => account.id), id, delta);
}

export type Tone = "ok" | "warning" | "danger";

/** "in 42m" / "in 3h" / "in 2d". */
export function inWords(atSeconds: number, nowMs: number): string {
  const minutes = Math.max(0, Math.round((atSeconds * 1000 - nowMs) / 60_000));
  if (minutes < 90) return `in ${minutes}m`;
  const hours = Math.round(minutes / 60);
  return hours < 48 ? `in ${hours}h` : `in ${Math.round(hours / 24)}d`;
}

/** "just now" / "3m ago" / "5h ago" / "4d ago", for a Unix-seconds time. */
export function agoText(atSeconds: number, nowMs: number): string {
  const seconds = Math.max(0, nowMs / 1000 - atSeconds);
  if (seconds < 60) return "just now";
  const minutes = Math.round(seconds / 60);
  if (minutes < 90) return `${minutes}m ago`;
  const hours = Math.round(minutes / 60);
  return hours < 48 ? `${hours}h ago` : `${Math.round(hours / 24)}d ago`;
}

export type UsageRow = {
  key: string;
  label: string;
  /** 0–100, what the meter fills to. */
  percent: number;
  text: string;
  tone?: Tone;
};

/**
 * One row per window the vendor reported. A window whose reset time has passed
 * since the reading starts over, so it is drawn empty and says so; a reading with
 * no reset time stays as it was seen.
 */
export function usageRows(account: Pick<AgentAccount, "usage">, nowMs: number): UsageRow[] {
  return account.usage.map((window, index) => {
    const reset = window.resetsAt > 0 && window.resetsAt * 1000 <= nowMs;
    const percent = reset ? 0 : Math.min(100, Math.max(0, Math.round(window.usedPercent)));
    const parts = [reset ? "reset since the last reading" : `${percent}%`];
    if (!reset && window.resetsAt > 0) parts.push(resetsIn(window.resetsAt, nowMs));
    return {
      key: `${index}:${window.label}`,
      label: window.label,
      percent,
      text: parts.join(" · "),
      tone: reset ? undefined : window.status === "reached" ? "danger" : window.status === "warning" ? "warning" : undefined,
    };
  });
}

/** "Observed 3m ago", "Checked 3m ago", or that there is no reading yet. */
export function readingText(account: Pick<AgentAccount, "observedAt" | "usageSource">, nowMs: number): string {
  if (!account.observedAt) return "No usage reading yet";
  const verb = account.usageSource === "checked" ? "Checked" : "Observed";
  return `${verb} ${agoText(account.observedAt, nowMs)}`;
}

export type AccountNote = { key: string; text: string; tone?: Tone };

/**
 * What keeps a signed-in account from being used right now. A time at or before
 * now is a past value nothing cleared, so it says nothing.
 */
export function accountNotes(
  account: Pick<AgentAccount, "exhaustedUntil" | "cooldownUntil">,
  nowMs: number,
): AccountNote[] {
  const notes: AccountNote[] = [];
  if (account.exhaustedUntil * 1000 > nowMs) {
    notes.push({ key: "limit", text: `Limit reached · available ${inWords(account.exhaustedUntil, nowMs)}`, tone: "danger" });
  }
  if (account.cooldownUntil * 1000 > nowMs) {
    notes.push({
      key: "cooldown",
      text: `Left out for now: it could not be opened · retry ${inWords(account.cooldownUntil, nowMs)}`,
      tone: "warning",
    });
  }
  return notes;
}

/**
 * How a Refresh usage ended. The backend answers with what it already has when
 * asked too soon, so the only sign that nothing was read is an unchanged time.
 */
export function refreshNote(before: Pick<AgentAccount, "observedAt">, after: Pick<AgentAccount, "observedAt">): string {
  return after.observedAt !== before.observedAt
    ? ""
    : "Nothing new: DevKit asks the vendor at most every few seconds, and this reading is already the latest.";
}

/** The one line a transcript notice says. */
export function noticeText(notice: AccountNotice, fromLabel: string, toLabel: string): string {
  if (notice.kind === "failed") {
    return `Could not switch from ${fromLabel} to ${toLabel}${notice.reason ? `: ${notice.reason}` : ""}`;
  }
  return `Switched from ${fromLabel} to ${toLabel}${notice.reason ? ` · ${notice.reason}` : ""}`;
}

/** The toast when a session moves. */
export function switchedToast(toLabel: string, reason: string): { title: string; description: string } {
  return { title: `Moved to ${toLabel}`, description: reason };
}
