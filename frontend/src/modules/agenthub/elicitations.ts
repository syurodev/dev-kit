import { useCallback, useState } from "react";

import {
  agentHubRespondElicitation,
  type ElicitationResponse,
  type PendingElicitation,
} from "@/bridge/agenthub";
import { errorMessage } from "@/bridge/errors";
import { useAgentSessionsStore } from "@/stores/agentSessions";

/**
 * Answering an agent's question, from whichever surface shows it — the card in
 * the Agent Hub, the card in the rail, the toast. Mirrors useResolvePermission,
 * minus the status refresh: a question never changed the session's status.
 */
export function useRespondElicitation(pending: PendingElicitation | null) {
  const dequeueElicitation = useAgentSessionsStore((s) => s.dequeueElicitation);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const respond = useCallback(
    async (response: ElicitationResponse) => {
      if (!pending) return;
      setBusy(true);
      setError("");
      try {
        await agentHubRespondElicitation(pending.id, response);
        dequeueElicitation(pending.id);
      } catch (e) {
        const message = errorMessage(e);
        // Already answered elsewhere, taken back, or its turn stopped: nothing
        // is waiting for this card any more, so it goes rather than erroring.
        if (/no pending elicitation|invalid elicitation id/.test(message)) dequeueElicitation(pending.id);
        else setError(message);
      } finally {
        setBusy(false);
      }
    },
    [pending, dequeueElicitation],
  );

  return { busy, error, respond };
}

/** The oldest question this session is waiting on, if any. */
export function elicitationForSession(
  queue: PendingElicitation[],
  sessionId: string | undefined,
): PendingElicitation | null {
  if (!sessionId) return null;
  return queue.find((e) => e.sessionId === sessionId) ?? null;
}
