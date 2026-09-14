import { cn } from "@/lib/utils";

export type DockBadgeState = "waiting" | "running" | "success";

export type AgentDockBadgeProps = {
  state: DockBadgeState;
  count?: number;
  className?: string;
};

/**
 * Agent / Dock Badge — Figma `933:150`, variants `933:147` Waiting and
 * `933:149` Running.
 *
 * Figma's note: "Ambient signal on the dock's agent item, for when the rail is
 * closed and no toast is on screen — a request that arrived while you were away
 * leaves nothing else behind. Waiting shows a count and must be answered;
 * Running is a dot and needs nothing."
 *
 * Both wear a 2px background/app ring: on a light dock icon a warning-yellow
 * pill has no edge without it, and reads as part of the glyph.
 *
 * Running does not pulse. It is the "needs nothing" state, and an animation
 * that never stops is the one thing on the dock you cannot ignore.
 */
export function AgentDockBadge({ state, count = 1, className }: AgentDockBadgeProps) {
  if (state === "running") {
    return (
      <span
        data-testid="agent-dock-badge-running"
        data-state="running"
        title="Agent swarm running"
        className={cn(
          "absolute -top-0.5 -right-0.5 size-2 rounded-full border-2 border-bg-app bg-status-info select-none",
          className,
        )}
      />
    );
  }

  if (state === "success") {
    return (
      <span
        data-testid="agent-dock-badge-success"
        data-state="success"
        title={`${count} agent ${count === 1 ? "turn" : "turns"} ready to review`}
        className={cn(
          "absolute -top-1 -right-1 flex size-4 min-w-4 items-center justify-center rounded-full px-1",
          "border-2 border-bg-app bg-status-success",
          "text-caption-11-medium whitespace-nowrap text-action-primary-foreground select-none",
          className,
        )}
      >
        {count}
      </span>
    );
  }

  return (
    <span
      data-testid="agent-dock-badge-waiting"
      data-state="waiting"
      title={`${count} agent ${count === 1 ? "turn" : "turns"} waiting for permission`}
      className={cn(
        "absolute -top-1 -right-1 flex size-4 min-w-4 items-center justify-center rounded-full px-1",
        "border-2 border-bg-app bg-status-warning",
        "text-caption-11-medium whitespace-nowrap text-action-primary-foreground select-none",
        className,
      )}
    >
      {count}
    </span>
  );
}
