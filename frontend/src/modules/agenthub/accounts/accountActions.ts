import type { QueryClient } from "@tanstack/react-query";
import type { AccountPolicy, AgentAccount, VendorPolicy } from "@/bridge/agentAccounts";
import { errorMessage } from "@/bridge/errors";
import { movedOrder, refreshNote } from "./accountModel";
import { vendorPolicyOf, withVendorPolicy } from "./accountPolicy";
import { AGENT_ACCOUNT_POLICY_KEY } from "./useAgentAccounts";

/**
 * What the accounts page does around a bridge call, apart from React so the
 * rules can be tested with a fake client.
 */

type ClientLike = Pick<QueryClient, "setQueryData" | "invalidateQueries">;
type Key = readonly unknown[];

export type Outcome<T> = { ok: true; value: T } | { ok: false; message: string };

/**
 * Runs a change. A refusal comes back as its sentence (Go's, as the bridge
 * published it) and the list is read again, because a refusal is often a stale
 * view: the account was removed elsewhere, or a session now runs as it.
 */
export async function attempt<T>(client: ClientLike, key: Key, run: () => Promise<T>): Promise<Outcome<T>> {
  try {
    return { ok: true, value: await run() };
  } catch (cause) {
    void client.invalidateQueries({ queryKey: key });
    return { ok: false, message: errorMessage(cause) };
  }
}

/** Puts an account the backend answered with into the cached list, in its place. */
export function replaceAccount(client: ClientLike, key: Key, next: AgentAccount): void {
  client.setQueryData<AgentAccount[]>(key, (current) => current?.map((account) => (account.id === next.id ? next : account)));
}

/** Takes a removed account out of the cached list. */
export function dropAccount(client: ClientLike, key: Key, id: string): void {
  client.setQueryData<AgentAccount[]>(key, (current) => current?.filter((account) => account.id !== id));
}

/** The bridge call that stores the rules, passed in so the flow can be tested without it. */
export type SetPolicy = (policy: AccountPolicy, acknowledgeRisk: boolean) => Promise<AccountPolicy>;

/**
 * Stores one vendor's rules: the whole preference, the other vendors kept, at
 * the revision it was read at. The answer replaces the cached rules; a refusal
 * (a stale revision above all) comes back as its sentence and reads them again.
 */
export async function saveVendorPolicy(
  client: ClientLike,
  current: AccountPolicy,
  vendor: string,
  next: VendorPolicy,
  acknowledgeRisk: boolean,
  set: SetPolicy,
): Promise<Outcome<AccountPolicy>> {
  const outcome = await attempt(client, AGENT_ACCOUNT_POLICY_KEY, () => set(withVendorPolicy(current, vendor, next), acknowledgeRisk));
  if (outcome.ok) client.setQueryData(AGENT_ACCOUNT_POLICY_KEY, outcome.value);
  return outcome;
}

/** Moves an account one place in the order, writing the whole order as shown; it never carries the terms flag. */
export function moveVendorAccount(
  client: ClientLike,
  current: AccountPolicy,
  vendor: string,
  accounts: readonly AgentAccount[],
  id: string,
  delta: -1 | 1,
  set: SetPolicy,
): Promise<Outcome<AccountPolicy>> {
  const rules = vendorPolicyOf(current, vendor);
  return saveVendorPolicy(client, current, vendor, { ...rules, order: movedOrder(accounts, rules.order, id, delta) }, false, set);
}

/** Puts a refreshed account into the list and answers the note to show: quiet when the reading did not move. */
export function applyUsageRefresh(client: ClientLike, key: Key, before: AgentAccount | undefined, after: AgentAccount): string {
  replaceAccount(client, key, after);
  return before ? refreshNote(before, after) : "";
}
