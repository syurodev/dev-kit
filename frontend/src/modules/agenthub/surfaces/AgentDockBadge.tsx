import { cn } from "@/lib/utils";

export type DockBadgeState = "waiting" | "running";

export type AgentDockBadgeProps = {
  state: DockBadgeState;
  count?: number;
  className?: string;
};

/**
 * Agent / Dock Badge (Figma 933:150)
 * Badge rendered on the Agent Hub dock item.
 * Variants:
 * - State Waiting (16px): shows pending permission count needing attention.
 * - State Running (8px dot): indicates background agent activity.
 */
export function AgentDockBadge({
  state,
  count = 1,
  className,
}: AgentDockBadgeProps) {
  if (state === "waiting") {
    return (
      <span
        data-testid="agent-dock-badge-waiting"
        data-state="waiting"
        className={
          cn(
            "absolute -top-1 -right-1 flex h-4 min-w-4 items-center justify-center rounded-full bg-status-warning px-1 text-palette-onyx shadow-sm select-none",
            className,
          ) + " text-caption-11-medium"
        }
        title={`${count} agent ${count === 1 ? "turn" : "turns"} waiting for permission`}
      >
        {count}
      </span>
    );
  }

  return (
    <span
      data-testid="agent-dock-badge-running"
      data-state="running"
      className={cn(
        "absolute -top-0.5 -right-0.5 flex size-2 items-center justify-center select-none",
        className,
      )}
      title="Agent swarm running"
    >
      <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-status-info opacity-75" />
      <span className="relative inline-flex size-2 rounded-full bg-status-info" />
    </span>
  );
}
