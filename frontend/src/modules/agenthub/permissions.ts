import { useCallback, useState } from "react";

import {
  agentHubGetStatus,
  agentHubResolvePermission,
  cancelledPermissionOutcome,
  selectedPermissionOutcome,
  type PendingPermission,
} from "@/bridge/agenthub";
import { errorMessage } from "@/bridge/errors";
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
          upsertSession(await agentHubGetStatus(pending.sessionId));
        } catch {
          // The answer landed; the session may be gone or the hub away. Losing
          // the refreshed row must not look like a failed permission.
        }
      } catch (e) {
        setError(errorMessage(e));
      } finally {
        setBusy(false);
      }
    },
    [pending, dequeuePending, upsertSession],
  );

  return { busy, error, resolve };
}

/** The request this session is waiting on, if any. */
export function pendingForSession(
  queue: PendingPermission[],
  sessionId: string | undefined,
): PendingPermission | null {
  if (!sessionId) return null;
  return queue.find((p) => p.sessionId === sessionId) ?? null;
}
