import { Shimmer } from "@/components/ui/shimmer";
import { cn } from "@/lib/utils";

export type AgentStatus =
  | "idle"
  | "running"
  | "awaiting"
  | "error"
  | "closed";

/**
 * Agent / State — Figma `904:23`, five statuses.
 *
 * A 6px dot and a lowercase word. Figma's note: "Colour is meaningful here:
 * awaiting needs the human, error stopped the turn." So the dot carries the
 * meaning and the label only names it — which is why the label stays
 * text/secondary in every state and never takes the status colour.
 *
 * Figma exports the dot as an image, so the fills were read out of the rendered
 * node pixel by pixel rather than from the node's own properties:
 *
 *   idle     rgb(139,142,138)  text/tertiary
 *   running  rgb(178,213,229)  was status/info; now --status-running (text/primary)
 *   awaiting rgb(241,254,200)  status/warning
 *   error    rgb(216,120,134)  status/danger
 *   closed   rgb(84,85,83)     text/tertiary at 50% over surface/base
 *
 * Closed is the one that is not a token on its own: 0.5x139 + 0.5x29 = 84 on
 * every channel. A closed session is a dimmed idle, not a colour of its own.
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
