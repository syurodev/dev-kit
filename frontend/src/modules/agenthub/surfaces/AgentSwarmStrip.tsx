import { cn } from "@/lib/utils";
import type { AgentSession } from "@/bridge/agenthub";

/** Matches AgentState's dot colours; the strip has room for a dot, not a word. */
const DOT_CLASS: Record<string, string> = {
  running: "bg-status-info",
  awaiting_permission: "bg-status-warning",
  error: "bg-status-danger",
  closed: "bg-text-tertiary/50",
};

export type AgentSwarmStripProps = {
  sessions: AgentSession[];
  activeSessionId?: string;
  onSelectSession?: (id: string) => void;
  /** How many chips fit before the rest collapse into "+N". */
  visible?: number;
  className?: string;
};

/**
 * The swarm strip at the top of Agent / Rail — Figma `913:13`.
 *
 * The rail travels across modules, so it needs its own way to change agent
 * without the Agent Hub sidebar being on screen. A head line counts the swarm
 * and what is waiting; a row of chips switches between them, with the overflow
 * collapsed into a "+N" chip rather than wrapping a 320px rail into three rows.
 *
 * Three chip shapes, and the difference is weight rather than colour: an idle
 * chip has no ground at all, the selected one takes surface/raised with a
 * border/subtle hairline and steps its label up to Medium/primary, and "+N" is
 * the hairline alone.
 */
export function AgentSwarmStrip({
  sessions,
  activeSessionId,
  onSelectSession,
  visible = 2,
  className,
}: AgentSwarmStripProps) {
  const waiting = sessions.filter((s) => s.status === "awaiting_permission").length;
  const shown = sessions.slice(0, visible);
  const overflow = sessions.length - shown.length;

  return (
    <div
      data-testid="agent-swarm-strip"
      className={cn("flex w-full flex-col gap-2 px-2 py-3", className)}
    >
      <div className="flex w-full items-center gap-2 text-caption-11 text-text-tertiary">
        <span className="min-w-0 flex-1 text-caption-11-medium">Swarm</span>
        <span className="shrink-0 whitespace-nowrap">
          {`${sessions.length} agent${sessions.length === 1 ? "" : "s"} · ${waiting} waiting`}
        </span>
      </div>

      <div className="flex w-full items-start gap-2">
        {shown.map((session) => {
          const selected = session.id === activeSessionId;
          return (
            <button
              key={session.id}
              type="button"
              data-testid="agent-swarm-chip"
              data-selected={selected}
              onClick={() => onSelectSession?.(session.id)}
              className={cn(
                "flex shrink-0 items-center gap-2 rounded-full px-2 py-1 cursor-pointer",
                selected
                  ? "border border-border-subtle bg-surface-raised text-caption-11-medium text-text-primary"
                  : "border border-transparent text-caption-11 text-text-secondary hover:bg-(--state-soft)",
              )}
            >
              <span
                aria-hidden="true"
                className={cn(
                  "size-1.5 shrink-0 rounded-full",
                  DOT_CLASS[session.status] ?? "bg-text-tertiary",
                )}
              />
              <span className="whitespace-nowrap">
                {session.title || session.id.slice(0, 8)}
              </span>
            </button>
          );
        })}
        {overflow > 0 ? (
          <span
            data-testid="agent-swarm-more"
            className="shrink-0 rounded-full border border-border-subtle px-2 py-1 text-caption-11 text-text-tertiary"
          >
            {`+${overflow}`}
          </span>
        ) : null}
      </div>
    </div>
  );
}
