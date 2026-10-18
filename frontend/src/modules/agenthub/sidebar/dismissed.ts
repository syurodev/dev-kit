import type { AgentSession } from "@/bridge/agenthub";

/**
 * A failed session the person said they no longer need ("Dismiss from Needs
 * you"). Only the failure is dismissed: the session is not closed, and a
 * permission or a question is never dismissed, because it is actionable on the
 * card and an agent is blocked on it.
 *
 * The map is `sessionId → the row's updatedAt when it was dismissed`
 * (stores/agentPins `dismissedFailures`). A failure stays dismissed while the
 * row is not newer than that, so the session returns to Needs you only by
 * failing again. Pure, so every rule is a test.
 */
export type DismissedFailures = Readonly<Record<string, number>>;

/** The failure on this session was dismissed and nothing has happened to it since. */
export function failureDismissed(
  session: Pick<AgentSession, "id" | "status" | "updatedAt">,
  dismissed: DismissedFailures | undefined,
): boolean {
  if (session.status !== "error") return false;
  const at = dismissed?.[session.id];
  return at !== undefined && (session.updatedAt ?? 0) <= at;
}

/** What to remember when the person dismisses `session`'s failure. */
export function dismissalAt(session: Pick<AgentSession, "updatedAt">): number {
  return session.updatedAt ?? 0;
}

/**
 * Drops the entries that no longer mean anything: the session is gone, or it is
 * no longer failed (it recovered or was resumed — its next failure is a new
 * one, whatever the row's time says). Returns the same object when nothing
 * changes, so a store holding it publishes nothing. Call it only once the
 * session list has loaded: an empty list is "not loaded yet", not "all gone".
 */
export function pruneDismissed(
  dismissed: DismissedFailures,
  sessions: Readonly<Record<string, Pick<AgentSession, "status">>>,
): DismissedFailures {
  let next: Record<string, number> | null = null;
  for (const id of Object.keys(dismissed)) {
    if (sessions[id]?.status === "error") continue;
    next ??= { ...dismissed };
    delete next[id];
  }
  return next ?? dismissed;
}
