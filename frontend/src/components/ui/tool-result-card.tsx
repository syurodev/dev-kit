import * as React from "react";
import { cn } from "@/lib/utils";

export type ToolResultRowProps = {
  /** Leading glyph on line 1. Omit for a row inside a ToolResultList whose
   * own header already carries the icon — a plain list doesn't repeat it. */
  icon?: React.ComponentType<{ className?: string }>;
  /** Line 1, left — the thing itself. Truncates before `trailing` gives an inch. */
  label: React.ReactNode;
  /** Line 1, right — short, never wraps: a status, a duration, a SHA. */
  trailing?: React.ReactNode;
  /** Line 2 — muted detail, already joined (e.g. by " · "). */
  detail?: React.ReactNode;
  onClick?: () => void;
  className?: string;
};

/**
 * One row: icon, primary label, optional trailing meta on the same line,
 * optional muted detail line under it. Figma "Agent / Row" (906:35, DevKit
 * design file `3dOzSZBYY7QtzANFTCfEe8`) — an agent in the swarm list,
 * status right-aligned on line 1, its project · turn count muted on line 2
 * — reused here for a ToolResultView result: single (TaskResultCard,
 * GitCommitResultCard, …) or repeated (SymbolMatchResultCard's hits). No
 * background at rest, same as the Figma row; a click target lifts to
 * `bg-(--state-soft)` on hover/focus — verified against the actual Hover
 * variant on canvas, not guessed.
 */
export function ToolResultRow({ icon: Icon, label, trailing, detail, onClick, className }: ToolResultRowProps) {
  const Tag = onClick ? "button" : "div";
  return (
    <Tag
      type={onClick ? "button" : undefined}
      onClick={onClick}
      data-testid="tool-result-row"
      className={cn(
        "flex w-full min-w-0 flex-col gap-0.5 rounded-md px-3 py-2 text-left transition-colors duration-(--motion-fast)",
        onClick &&
          "cursor-pointer hover:bg-(--state-soft) focus-visible:bg-(--state-soft) focus-visible:outline-none",
        className,
      )}
    >
      <span className="flex w-full min-w-0 items-center gap-2">
        {Icon ? <Icon className="size-(--icon-12) shrink-0 text-icon-secondary" aria-hidden="true" /> : null}
        <span className="min-w-0 flex-1 truncate text-body-13-medium text-text-primary">{label}</span>
        {trailing ? <span className="shrink-0 text-caption-11 text-text-tertiary">{trailing}</span> : null}
      </span>
      {detail ? (
        // 4.5 = 18px: the 12px icon plus the 2 (8px) gap beside it on line 1 —
        // an indent lined up with the label above it, not a spacing step.
        <span className={cn("block w-full min-w-0 truncate text-caption-11 text-text-tertiary", Icon && "pl-4.5")}>
          {detail}
        </span>
      ) : null}
    </Tag>
  );
}

export type ToolResultListProps = {
  icon: React.ComponentType<{ className?: string }>;
  title: React.ReactNode;
  children: React.ReactNode;
  className?: string;
};

/**
 * Group chrome for several ToolResultRows — SymbolMatchResultCard,
 * EndpointListResultCard. Same ground as AgentFileChip: bg-(--state-soft),
 * rounded-sm; the rows inside carry their own hover, so the group itself
 * never darkens further. Not the Figma "Shared / Entity Chip" (not
 * designed per-kind yet) — the same temporary role AgentRefChip plays for
 * `agent.attach`.
 */
export function ToolResultList({ icon: Icon, title, children, className }: ToolResultListProps) {
  return (
    <div
      data-testid="tool-result-list"
      className={cn("flex w-full min-w-0 flex-col gap-0.5 rounded-sm bg-(--state-soft) p-1.5", className)}
    >
      <div className="flex min-w-0 items-center gap-1.5 px-1.5 py-1">
        <Icon className="size-(--icon-12) shrink-0 text-text-tertiary" aria-hidden="true" />
        <span className="min-w-0 truncate text-caption-11-medium text-text-tertiary">{title}</span>
      </div>
      {children}
    </div>
  );
}
