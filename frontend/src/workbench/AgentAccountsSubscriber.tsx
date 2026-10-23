import { useEffect } from "react";
import { useQueryClient, type QueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { agentAccountsList, onAgentAccountSwitched, type AccountSwitchedEvent } from "../bridge/agentAccounts";
import type { AgentSession } from "../bridge/agenthub";
import { accountLabelOf, switchedToast } from "../modules/agenthub/accounts/accountModel";
import { agentAccountsKey, watchAgentAccounts } from "../modules/agenthub/accounts/useAgentAccounts";
import { agentSessionOnScreen } from "../modules/agenthub/documents";
import { useAgentSessionsStore } from "../stores/agentSessions";

/** What a move needs from the outside, so the rules can be tested without a bridge. */
export type SwitchDeps = {
  session: (id: string) => Pick<AgentSession, "agentType" | "accountId"> | undefined;
  setAccount: (sessionId: string, accountId: string) => void;
  onScreen: (sessionId: string) => boolean;
  labelOf: (vendor: string, accountId: string) => Promise<string>;
  notify: (title: string, description: string) => void;
};

/**
 * A session moved to another account at the start of a turn. The row in the
 * window's store learns it at once (the summary card's Account line would
 * otherwise say the old account until the session is read again), and the person
 * is told in a toast only when that session is the one in front of them: the
 * transcript carries the same fact as a line, for every other session, where it
 * happened. A session this window does not know is none of its business.
 */
export function handleAccountSwitched(event: AccountSwitchedEvent, deps: SwitchDeps): void {
  const session = deps.session(event.sessionId);
  if (!session) return;
  if (session.accountId !== event.to) deps.setAccount(event.sessionId, event.to);
  if (!deps.onScreen(event.sessionId)) return;
  void deps.labelOf(session.agentType, event.to).then((label) => {
    const { title, description } = switchedToast(label, event.reason);
    deps.notify(title, description);
  });
}

function labelThroughQueries(client: QueryClient) {
  return async (vendor: string, accountId: string) => {
    try {
      const accounts = await client.fetchQuery({
        queryKey: agentAccountsKey(vendor),
        queryFn: () => agentAccountsList(vendor),
        staleTime: 10_000,
      });
      return accountLabelOf(accounts, vendor, accountId);
    } catch {
      // Nothing to name it by: the built-in account still has its own name.
      return accountLabelOf(undefined, vendor, accountId);
    }
  };
}

/**
 * Keeps the accounts lists current (a change event from Go, and the window
 * regaining focus) and reacts to a session moving. Renders nothing; mounted once,
 * beside AgentSessionsSubscriber, after the vault is open.
 */
export function AgentAccountsSubscriber() {
  const client = useQueryClient();
  useEffect(() => watchAgentAccounts(client), [client]);
  useEffect(() => {
    const deps: SwitchDeps = {
      session: (id) => useAgentSessionsStore.getState().sessionsById[id],
      setAccount: (sessionId, accountId) => {
        const known = useAgentSessionsStore.getState().sessionsById[sessionId];
        if (known) useAgentSessionsStore.getState().upsertSession({ ...known, accountId });
      },
      onScreen: (sessionId) => agentSessionOnScreen(sessionId),
      labelOf: labelThroughQueries(client),
      notify: (title, description) => toast.info(title, { description: description || undefined }),
    };
    return onAgentAccountSwitched((event) => handleAccountSwitched(event, deps));
  }, [client]);
  return null;
}
