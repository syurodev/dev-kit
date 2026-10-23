import { useEffect, useState } from "react";
import { useQuery, type QueryClient } from "@tanstack/react-query";
import { agentAccountPolicyGet, agentAccountsList, onAgentAccountsChanged } from "@/bridge/agentAccounts";

/** Every vendor's account list; one entry per vendor under it. */
export const AGENT_ACCOUNTS_KEY = ["agenthub", "accounts"] as const;
export const agentAccountsKey = (vendor: string) => [...AGENT_ACCOUNTS_KEY, vendor] as const;
export const AGENT_ACCOUNT_POLICY_KEY = ["agenthub", "accountPolicy"] as const;

/**
 * A vendor's accounts, its built-in one first. Kept fresh by events, never by
 * polling: `watchAgentAccounts` (mounted once, by AgentAccountsSubscriber)
 * invalidates this key when Go says something changed and when the window
 * regains focus.
 */
export function useAgentAccounts(vendor: string, enabled = true) {
  return useQuery({
    queryKey: agentAccountsKey(vendor),
    queryFn: () => agentAccountsList(vendor),
    enabled: enabled && vendor !== "",
    staleTime: 30_000,
  });
}

/** The switching rules and their revision (what a save has to quote). */
export function useAgentAccountPolicy() {
  return useQuery({ queryKey: AGENT_ACCOUNT_POLICY_KEY, queryFn: agentAccountPolicyGet });
}

type FocusTarget = Pick<Window, "addEventListener" | "removeEventListener">;

/**
 * Refetch the lists when `agentaccounts:changed` fires and when the window gets
 * focus. Both are needed: readings seen in a session's traffic are announced at
 * most once every two seconds with no trailing event, so the newest reading can
 * go unannounced. `subscribe` and `focus` are the test seams. Returns unsubscribe.
 */
export function watchAgentAccounts(
  client: Pick<QueryClient, "invalidateQueries">,
  subscribe: (cb: () => void) => () => void = onAgentAccountsChanged,
  focus: FocusTarget | null = typeof window === "undefined" ? null : window,
): () => void {
  const refetch = () => {
    void client.invalidateQueries({ queryKey: AGENT_ACCOUNTS_KEY });
  };
  const off = subscribe(refetch);
  focus?.addEventListener("focus", refetch);
  return () => {
    off();
    focus?.removeEventListener("focus", refetch);
  };
}

/** The current time, moved on once a minute so "3m ago" and "resets in 2h" do not stand still on an open page. */
export function useNow(intervalMs = 60_000): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), intervalMs);
    return () => window.clearInterval(id);
  }, [intervalMs]);
  return now;
}
