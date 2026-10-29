import { Shimmer } from "@/components/ui/shimmer";
import { cn } from "@/lib/utils";
import type { AgentSession } from "@/bridge/agenthub";
import { isWorking } from "../sessionActivity";

export type AgentStatus =
  | "idle"
  | "running"
  | "awaiting"
  | "error"
  | "closed";

/**
 * The status to draw for a session: its stored one, except that an idle session
 * whose agent runs a turn of its own is running. Waiting, failed and closed are not
 * overridden.
 */
export function agentStatusOf(session: Pick<AgentSession, "status" | "activity">, stored: AgentStatus): AgentStatus {
  return stored === "idle" && isWorking(session) ? "running" : stored;
}

/**
 * Agent / State — Figma `904:23`, five statuses.
 *
 * A 6px dot and a lowercase word. Figma's note: "Colour is meaningful here:
 * awaiting needs the human, error stopped the turn." So the dot carries the
 * meaning and the label only names it — which is why the label stays
 * text/secondary in every state and never takes the status colour.
 *
 * The dot reads a token per state (Mono chrome):
 *
 *   idle     --text-tertiary (--status-idle)
 *   running  --status-running, the primary text colour: unmistakably brighter
 *            than idle (it was the info grey, ~1.2:1 from idle)
 *   awaiting --status-warning
 *   error    --status-danger
 *   closed   --text-tertiary at 50%
 *
 * (Figma's original fills were lime/sky pixels; they were retired with the
 * Mono chrome.) A closed session is a dimmed idle, not a colour of its own.
 */

const DOT_CLASS: Record<AgentStatus, string> = {
  idle: "bg-text-tertiary",
  running: "bg-(--status-running)",
  awaiting: "bg-status-warning",
  error: "bg-status-danger",
  closed: "bg-text-tertiary/50",
};

export type AgentStateProps = {
  status: AgentStatus;
  /** Hides the word, for places that only have room for the dot. */
  labelHidden?: boolean;
  /**
   * Beats the dot. For a row in the swarm tree that is waiting on the user:
   * five still dots down a rail all read as decoration, and this one is the
   * only one with a deadline behind it.
   */
  pulse?: boolean;
  className?: string;
  id?: string;
};

export function AgentState({
  status,
  labelHidden = false,
  pulse = false,
  className,
  id,
}: AgentStateProps) {
  return (
    <span
      id={id}
      data-slot="agent-state"
      data-status={status}
      className={cn("inline-flex shrink-0 items-center gap-2", className)}
    >
      <span
        aria-hidden="true"
        className={cn(
          "size-1.5 shrink-0 rounded-full",
          DOT_CLASS[status],
          pulse && "animate-pulse motion-reduce:animate-none",
        )}
      />
      {labelHidden ? (
        <span className="sr-only">{status}</span>
      ) : (
        <Shimmer active={status === "running"} className="text-body-12 text-text-secondary">{status}</Shimmer>
      )}
    </span>
  );
}
