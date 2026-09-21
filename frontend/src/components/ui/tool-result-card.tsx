import * as React from "react";
import { ArrowUpRight } from "lucide-react";
import { cn } from "@/lib/utils";

export type ToolResultCardProps = {
  icon: React.ComponentType<{ className?: string }>;
  title: React.ReactNode;
  /** One line under the title — a status summary, a method + path. */
  subtitle?: React.ReactNode;
  /** The body: whatever detail the renderer wants — a step list, fields. */
  children?: React.ReactNode;
  actionLabel?: string;
  onAction?: () => void;
  className?: string;
};

/**
 * Shared chrome for a ToolResultView card
 * (docs/dev-kit/tool-result-view.mdx § Cách thể hiện trên UI). Lives in the
 * shared UI kit, not under modules/agenthub, so a renderer another module
 * registers (Code Graph, API Client, Tasks…) doesn't have to import Agent
 * Hub to build its card — the same reason EntityRef lives in
 * frontend/src/workbench rather than in any one module.
 *
 * Same visual language as AgentFileChip/AgentRefChip: bg-(--state-soft),
 * rounded-sm, a 12px muted icon — not the Figma "Shared / Entity Chip" (not
 * designed per-kind yet), the same temporary role AgentRefChip plays for
 * `agent.attach`.
 *
 * Renders only what it's given: a renderer that has nothing worth showing
 * returns null before this ever mounts (see toolResultViews.ts,
 * renderToolResult) rather than this component inventing empty chrome.
 */
export function ToolResultCard({
  icon: Icon,
  title,
  subtitle,
  children,
  actionLabel,
  onAction,
  className,
}: ToolResultCardProps) {
  return (
    <div
      data-testid="tool-result-card"
      className={cn("flex w-full min-w-0 flex-col gap-1.5 rounded-sm bg-(--state-soft) p-2", className)}
    >
      <div className="flex min-w-0 items-center gap-1.5">
        <Icon className="size-(--icon-12) shrink-0 text-text-tertiary" aria-hidden="true" />
        <span className="min-w-0 truncate text-body-12 text-text-secondary">{title}</span>
      </div>
      {subtitle ? (
        // 4.5 = 18px: the 12px icon plus the 1.5 (6px) gap beside it, same
        // measure AgentToolCall.tsx's row variant takes off its own icon —
        // not a spacing step, an indent lined up with the icon above it.
        <div className="min-w-0 truncate pl-4.5 text-caption-11 text-text-tertiary">{subtitle}</div>
      ) : null}
      {children ? <div className="min-w-0 pl-4.5">{children}</div> : null}
      {actionLabel && onAction ? (
        <button
          type="button"
          onClick={onAction}
          data-testid="tool-result-card-action"
          className={cn(
            "ml-4.5 inline-flex w-fit items-center gap-1 text-caption-11 text-text-tertiary",
            "underline decoration-border-subtle hover:text-text-secondary",
            "cursor-pointer focus-visible:ring-ring focus-visible:ring-1 focus-visible:outline-none",
          )}
        >
          {actionLabel}
          <ArrowUpRight className="size-(--icon-12)" aria-hidden="true" />
        </button>
      ) : null}
    </div>
  );
}
