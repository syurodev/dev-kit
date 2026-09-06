import { CornerDownRight } from "lucide-react";
import { cn } from "@/lib/utils";
import type { AgentSession, SessionStatus } from "@/bridge/agenthub";

export type AgentRowState = "rest" | "hover" | "selected";

export type AgentRowProps = {
  session: AgentSession;
  selected?: boolean;
  pendingCount?: number;
  onClick?: () => void;
  className?: string;
};

function StatusIndicator({ status }: { status: SessionStatus }) {
  switch (status) {
    case "running":
      return (
        <span
          data-testid="status-dot-running"
          className="relative flex size-2 shrink-0"
          title="Running"
        >
          <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-status-info opacity-75" />
          <span className="relative inline-flex size-2 rounded-full bg-status-info" />
        </span>
      );
    case "awaiting_permission":
      return (
        <span
          data-testid="status-dot-awaiting"
          className="relative flex size-2 shrink-0"
          title="Awaiting permission"
        >
          <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-status-warning opacity-75" />
          <span className="relative inline-flex size-2 rounded-full bg-status-warning" />
        </span>
      );
    case "error":
      return (
        <span
          data-testid="status-dot-error"
          className="size-2 shrink-0 rounded-full bg-status-danger"
          title="Error"
        />
      );
    case "closed":
      return (
        <span
          data-testid="status-dot-closed"
          className="size-2 shrink-0 rounded-full border border-text-tertiary"
          title="Closed"
        />
      );
    case "idle":
    default:
      return (
        <span
          data-testid="status-dot-idle"
          className="size-2 shrink-0 rounded-full bg-text-tertiary"
          title="Idle"
        />
      );
  }
}

/**
 * Agent / Row (Figma 264×56)
 * Single agent entry in the swarm hierarchy with depth indentation and status.
 */
export function AgentRow({
  session,
  selected = false,
  pendingCount = 0,
  onClick,
  className,
}: AgentRowProps) {
  const depth = session.depth ?? 0;
  const hasParent = Boolean(session.parentSessionId);
  const displayTitle = session.title || `Session ${session.id.slice(0, 8)}`;

  return (
    <div
      role="button"
      tabIndex={0}
      data-testid={`agent-row-${session.id}`}
      data-selected={selected ? "true" : "false"}
      onClick={onClick}
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          onClick?.();
        }
      }}
      style={{ paddingLeft: `${Math.min(depth * 16, 64) + 8}px` }}
      className={cn(
        "group relative flex h-14 w-full select-none items-center justify-between pr-3 transition-colors cursor-pointer border-b border-border-subtle/50",
        selected
          ? "bg-surface-tinted border-l-2 border-l-action-primary"
          : "hover:bg-surface-raised",
        className,
      )}
    >
      <div className="flex items-center gap-2 min-w-0 flex-1">
        {hasParent ? (
          <CornerDownRight
            className="size-3 text-text-tertiary shrink-0 -ml-1"
            aria-hidden="true"
          />
        ) : null}

        <StatusIndicator status={session.status} />

        <div className="flex flex-col min-w-0 flex-1">
          <div className="flex items-center gap-1.5 min-w-0">
            <span
              className={cn(
                "truncate text-body-13-medium",
                selected ? "text-text-primary" : "text-text-secondary group-hover:text-text-primary",
              )}
            >
              {displayTitle}
            </span>
            <span className="rounded-sm bg-surface-base px-1 py-0.2 text-code-11 uppercase text-text-tertiary shrink-0">
              {session.agentType}
            </span>
          </div>

          <span className="truncate text-caption-11 text-text-tertiary">
            {session.projectPath || "."}
          </span>
        </div>
      </div>

      {pendingCount > 0 ? (
        <span
          data-testid="agent-row-pending-badge"
          className="ml-1 inline-flex h-4 min-w-4 items-center justify-center rounded-full bg-status-warning px-1 text-caption-11-medium text-palette-onyx"
          title={`${pendingCount} pending permissions`}
        >
          {pendingCount}
        </span>
      ) : null}
    </div>
  );
}
