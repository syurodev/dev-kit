import { ArrowDown } from "lucide-react";
import { cn } from "@/lib/utils";

export type AgentJumpToLatestProps = {
  onClick: () => void;
  visible?: boolean;
  className?: string;
};

/**
 * Agent / Jump To Latest (Figma 128×24)
 * Floating pill button that appears when the transcript viewport has scrolled up.
 */
export function AgentJumpToLatest({
  onClick,
  visible = true,
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
        "inline-flex h-6 w-32 items-center justify-center gap-1.5 rounded-full border border-border-subtle bg-surface-raised px-2.5 text-caption-11 text-text-primary shadow-md transition-colors hover:bg-surface-tinted focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring select-none cursor-pointer",
        className,
      )}
    >
      <ArrowDown className="size-3 text-text-tertiary shrink-0" aria-hidden="true" />
      <span>Jump to latest</span>
    </button>
  );
}
