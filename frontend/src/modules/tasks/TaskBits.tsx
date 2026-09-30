import { Bot, Minus, OctagonAlert, SignalHigh, SignalLow, SignalMedium, type LucideIcon } from "lucide-react";
import type { Category } from "@/bridge/tasks";
import { cn } from "@/lib/utils";
import { priorityLabel } from "./taskModel";

/**
 * The small marks the board and the task detail share. Each one has a fixed
 * box (`size-*` on every glyph) so a card with a priority and a card without
 * one are the same height, and nothing in a row shifts when a value arrives.
 */

const PRIORITY_ICON: Record<string, LucideIcon> = {
  urgent: OctagonAlert,
  high: SignalHigh,
  medium: SignalMedium,
  low: SignalLow,
};

export function PriorityIcon({ priority, className }: { priority: string; className?: string }) {
  const Icon = PRIORITY_ICON[priority] ?? Minus;
  return (
    <Icon
      aria-label={priorityLabel(priority)}
      className={cn(
        "size-(--icon-14) shrink-0",
        priority === "urgent" ? "text-status-danger" : priority ? "text-text-secondary" : "text-text-tertiary",
        className,
      )}
    />
  );
}

/** A status's category as a dot: open is hollow, active is info, terminal is success. */
export function StatusDot({ category, className }: { category: Category | undefined; className?: string }) {
  return (
    <span
      aria-hidden
      className={cn(
        "inline-block size-2 shrink-0 rounded-full border transition-colors duration-(--motion-fast)",
        category === "terminal"
          ? "border-status-success bg-status-success"
          : category === "active"
            ? "border-status-info bg-status-info"
            : "border-text-tertiary bg-transparent",
        className,
      )}
    />
  );
}

/** An agent session holds the task. */
export function AgentHeld({ className }: { className?: string }) {
  return (
    <Bot
      aria-label="Held by an agent session"
      className={cn("size-(--icon-14) shrink-0 text-status-success", className)}
    />
  );
}
