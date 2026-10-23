import type { QueryClient } from "@tanstack/react-query";
import type { AgentAccount } from "@/bridge/agentAccounts";
import { errorMessage } from "@/bridge/errors";

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
