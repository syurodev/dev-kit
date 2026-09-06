import { ChevronDown } from "lucide-react";

import { cn } from "@/lib/utils";

export type AgentJumpToLatestProps = {
  onClick: () => void;
  visible?: boolean;
  /** How many arrived while you were reading further up. */
  count?: number;
  className?: string;
};

/**
 * Agent / Jump To Latest — Figma `1044:3196`.
 *
 * The label is the count, not the action: "3 new messages" tells you whether
 * scrolling back down is worth it, where "Jump to latest" only repeats what the
 * arrow already says. Falls back to the plain label when nothing has been
 * counted, so the control is never mute.
 *
 * Floating over the transcript, so it takes surface/glass-floating (72%) rather
 * than a solid raised fill — the text under it should stay legible enough to
 * show you are not at the bottom.
 */
export function AgentJumpToLatest({
  onClick,
  visible = true,
  count,
  className,
}: AgentJumpToLatestProps) {
  if (!visible) return null;

  return (
    <button
      type="button"
      onClick={onClick}
      aria-label="Jump to latest"
      data-testid="agent-jump-to-latest"
      className={cn(
        "inline-flex items-center gap-1 rounded-full px-3 py-1",
        "border border-border-subtle bg-surface-glass-floating backdrop-blur-[12px]",
        "text-caption-11-medium whitespace-nowrap text-text-secondary",
        "hover:text-text-primary focus-visible:ring-ring focus-visible:ring-1 focus-visible:outline-none",
        "cursor-pointer select-none",
        className,
      )}
    >
      <ChevronDown className="size-3 shrink-0" aria-hidden="true" />
      <span>
        {typeof count === "number" && count > 0
          ? `${count} new message${count === 1 ? "" : "s"}`
          : "Jump to latest"}
      </span>
    </button>
  );
}
