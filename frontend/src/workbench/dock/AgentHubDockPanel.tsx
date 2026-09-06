import { cn } from "@/lib/utils";
import type { AgentSession } from "../../bridge/agenthub";

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

type AgentHubDockPanelProps = {
  sessions: AgentSession[];
  onSelect: (sessionId: string) => void;
};

export function AgentHubDockPanel({ sessions, onSelect }: AgentHubDockPanelProps) {
  return (
    <div className="flex w-56 flex-col gap-0.5 p-1.5">
      <p className="px-2 py-1 text-caption-11-medium text-text-tertiary">
        Agent Hub sessions
      </p>
      {sessions.map((session) => (
        <button
          key={session.id}
          type="button"
          onClick={() => onSelect(session.id)}
          className="flex items-center gap-2 rounded-control px-2 py-1.5 text-left text-body-13 text-text-primary hover:bg-surface-tinted cursor-pointer"
        >
          <span
            className={cn("size-1.5 shrink-0 rounded-full", statusColorClass(session.status))}
          />
          <span className="truncate">{sessionLabel(session)}</span>
        </button>
      ))}
    </div>
  );
}
