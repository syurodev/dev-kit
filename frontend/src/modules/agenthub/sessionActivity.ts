import type { AgentSession } from "../../bridge/agenthub";

/**
 * Whether a session is doing something right now: a turn of ours is running
 * (the stored status), or the vendor runs a turn of its own after a background
 * task finished (activity.agentTurn, from internal/agenthub/bgwork.go).
 *
 * "Is this session active" reads this, not `status === "running"`: the row says
 * idle through an agent-started turn, because only Invoke writes a status. What
 * stays on the stored status is what depends on DevKit's own turn — whether a
 * prompt queues or steers, whether the queue drains, a permission wait.
 */
export function isWorking(session: Pick<AgentSession, "status" | "activity"> | null | undefined): boolean {
  return session?.status === "running" || session?.activity?.agentTurn === true;
}

/**
 * Background tasks of a session that is otherwise quiet, for a line that says so
 * without drawing a spinner. Zero while it is working (the card then says that),
 * and for a closed session, whose process and tasks are gone.
 */
export function backgroundTasksOnly(session: Pick<AgentSession, "status" | "activity"> | null | undefined): number {
  if (!session || session.status === "closed" || isWorking(session)) return 0;
  return session.activity?.tasks ?? 0;
}

export function backgroundTasksLabel(count: number): string {
  return `${count} background ${count === 1 ? "task" : "tasks"}`;
}

/**
 * Whether nothing can still finish this session's latest tool calls: the session is
 * loaded, nothing is working on it (isWorking), no turn of ours is in flight and no
 * permission waits. Only then does the transcript settle a call of the latest turn
 * (transcript/toolSettle.tsx). A session that is not loaded is not quiet: nothing is
 * known, so nothing is settled.
 */
export function isSessionQuiet(
  session: Pick<AgentSession, "status" | "activity"> | null | undefined,
  turnInFlight: boolean,
): boolean {
  return Boolean(session) && !isWorking(session) && !turnInFlight && session?.status !== "awaiting_permission";
}
