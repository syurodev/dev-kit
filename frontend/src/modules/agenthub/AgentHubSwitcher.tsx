import { MENU_ROW, MenuRow } from "@/components/ui/menu-row";
import { cn } from "@/lib/utils";
import type { ModulePeekProps } from "../registry";
import type { AgentSession } from "../../bridge/agenthub";
import { useAgentSessionList, useAgentSessionsStore } from "../../stores/agentSessions";
import { openAgentSession } from "./documents";
import { isWorking } from "./sessionActivity";
import { AgentDockBadge } from "./surfaces/AgentDockBadge";

/**
 * What Agent Hub contributes to its title-bar button (ModuleManifest.peek and
 * .badge). Every session, not only the open tabs the shell's peek would list:
 * they are the things you jump between, and picking one opens its tab.
 */

function statusColorClass(status: AgentSession["status"]) {
  switch (status) {
    case "running":
      return "bg-(--status-running)";
    case "awaiting_permission":
      return "bg-(--status-permission)";
    case "error":
      return "bg-(--status-error)";
    default:
      return "bg-(--status-idle)";
  }
}

function sessionLabel(session: AgentSession) {
  return session.title?.trim() || `${session.agentType} · ${session.id.slice(0, 8)}`;
}

export function AgentHubPeek({ focusModule }: ModulePeekProps) {
  const sessions = useAgentSessionList();
  if (sessions.length === 0) {
    return <p className="px-2 pb-1.5 text-body-12 text-text-tertiary">No sessions</p>;
  }
  return (
    <div className="flex max-h-80 flex-col overflow-y-auto">
      {sessions.map((session) => (
        <button
          key={session.id}
          type="button"
          className={cn(MENU_ROW, "min-w-0 text-left")}
          onClick={() => {
            focusModule();
            openAgentSession(session);
          }}
        >
          <MenuRow
            icon={<span className={cn("size-1.5 rounded-full", statusColorClass(session.status))} />}
          >
            {sessionLabel(session)}
          </MenuRow>
        </button>
      ))}
    </div>
  );
}

/** Waiting on the user — a permission or a question — outranks running: one must be answered, the other needs nothing. */
export function AgentHubBadge() {
  const sessions = useAgentSessionList();
  const pendingCount = useAgentSessionsStore((state) => state.pendingQueue.length + state.elicitationQueue.length);
  const unreadCompleted = useAgentSessionsStore(
    (state) => Object.keys(state.unreadCompletedBySession).length,
  );
  const waiting = pendingCount || sessions.filter((s) => s.status === "awaiting_permission").length;
  const running = useAgentSessionsStore((state) =>
    state.sessionIds.some((id) => isWorking(state.sessionsById[id])),
  );
  if (waiting > 0) return <AgentDockBadge state="waiting" count={waiting} />;
  if (unreadCompleted > 0) return <AgentDockBadge state="success" count={unreadCompleted} />;
  if (running) return <AgentDockBadge state="running" />;
  return null;
}
