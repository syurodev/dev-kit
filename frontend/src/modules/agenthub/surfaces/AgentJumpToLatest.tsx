import { ArrowDown } from "lucide-react";

import { cn } from "@/lib/utils";

export type AgentJumpToLatestProps = {
  onClick: () => void;
  visible?: boolean;
  className?: string;
};

/**
 * Agent / Jump To Latest — Figma `1044:3196`.
 *
 * An arrow, not a count. The transcript changes on every streamed chunk, so
 * "new messages" counted updates, not messages — 210 of them for one answer.
 * Where the bottom is, is all the button has to say.
 *
 * Floating over the transcript, so it takes surface/glass-floating (72%) rather
 * than a solid raised fill — the text under it should stay legible enough to
 * show you are not at the bottom.
 */
export function AgentJumpToLatest({ onClick, visible = true, className }: AgentJumpToLatestProps) {
  if (!visible) return null;

  return (
    <button
      type="button"
      onClick={onClick}
      aria-label="Scroll to bottom"
      title="Scroll to bottom"
      data-testid="agent-jump-to-latest"
      className={cn(
        "inline-flex size-7 items-center justify-center rounded-full",
        "border border-border-subtle bg-surface-glass-floating backdrop-blur-md",
        "text-text-secondary",
        "hover:text-text-primary focus-visible:ring-ring focus-visible:ring-1 focus-visible:outline-none",
        "cursor-pointer select-none",
        className,
      )}
    >
      <ArrowDown className="size-3.5" aria-hidden="true" />
    </button>
  );
}
