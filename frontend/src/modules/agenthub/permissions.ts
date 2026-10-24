import { useCallback, useState } from "react";

import {
  agentHubGetStatus,
  agentHubResolvePermission,
  cancelledPermissionOutcome,
  selectedPermissionOutcome,
  type AgentSession,
  type PendingPermission,
} from "@/bridge/agenthub";
import { errorMessage, toAppError } from "@/bridge/errors";
import { useAgentSessionsStore } from "@/stores/agentSessions";

/**
 * Answering a permission request, from whichever surface is showing it — the
 * card in the Agent Hub composer column, the card in the rail, the toast over
 * another module. All three do the same three things in the same order (tell
 * the backend, drop the request from the queue, re-read the session's status),
 * and they were three copies of it.
 *
 * `optionId` null is "deny": the cancelled outcome, which is also what the gate
 * does on its own after 60s.
 */
/** The code Go answers with when the request it was asked to answer is no longer waiting. */
export const PERMISSION_NOT_WAITING = "agenthub.permissionNotWaiting";

export function isPermissionNotWaiting(error: unknown): boolean {
  return toAppError(error).code === PERMISSION_NOT_WAITING;
}

/** Re-read a session after one of its permission cards went away without our answer. */
export async function refreshSessionAfterPermission(sessionId: string): Promise<void> {
  if (!sessionId) return;
  try {
    const fresh = await agentHubGetStatus(sessionId);
    const state = useAgentSessionsStore.getState();
    state.upsertSession(statusAfterAnswer(fresh, state.pendingQueue.some((p) => p.sessionId === sessionId)));
  } catch {
    // The session may be gone or the hub away; the card is down either way.
  }
}

export function useResolvePermission(pending: PendingPermission | null) {
  const dequeuePending = useAgentSessionsStore((s) => s.dequeuePending);
  const upsertSession = useAgentSessionsStore((s) => s.upsertSession);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const resolve = useCallback(
    async (optionId: string | null) => {
      if (!pending) return;
      setBusy(true);
      setError("");
      try {
        await agentHubResolvePermission(
          pending.id,
          optionId === null ? cancelledPermissionOutcome() : selectedPermissionOutcome(optionId),
        );
        dequeuePending(pending.id);
        try {
          const fresh = await agentHubGetStatus(pending.sessionId);
          const stillWaiting = useAgentSessionsStore
            .getState()
            .pendingQueue.some((p) => p.sessionId === pending.sessionId);
          upsertSession(statusAfterAnswer(fresh, stillWaiting));
        } catch {
          // The answer landed; the session may be gone or the hub away. Losing
          // the refreshed row must not look like a failed permission.
        }
      } catch (e) {
        if (isPermissionNotWaiting(e)) {
          // Go withdrew the request before this answer: the card is stale, not
          // failed. Take it down and show the session as it really is.
          dequeuePending(pending.id);
          await refreshSessionAfterPermission(pending.sessionId);
        } else {
          setError(errorMessage(e));
        }
      } finally {
        setBusy(false);
      }
    },
    [pending, dequeuePending, upsertSession],
  );

  return { busy, error, resolve };
}

/**
 * The session as it stands once a permission is answered. Answered either way,
 * the turn goes on, but the status read right after the answer can still say
 * awaiting_permission: the call returns as soon as the answer reaches the
 * waiting agent, and Go puts the session back to running a moment later, on
 * the agent's side. Nothing announces that change, so the stale read stood
 * until the turn ended — and the transcript's running indicator stayed off
 * while the agent kept working. Unless another request for the session has
 * already arrived, it is running.
 */
export function statusAfterAnswer(fresh: AgentSession, stillWaiting: boolean): AgentSession {
  if (fresh.status === "awaiting_permission" && !stillWaiting) return { ...fresh, status: "running" };
  return fresh;
}

/** The request this session is waiting on, if any. */
export function pendingForSession(
  queue: PendingPermission[],
  sessionId: string | undefined,
): PendingPermission | null {
  if (!sessionId) return null;
  return queue.find((p) => p.sessionId === sessionId) ?? null;
}
