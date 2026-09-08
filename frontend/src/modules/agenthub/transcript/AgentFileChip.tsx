import { FileCode } from "lucide-react";
import { cn } from "@/lib/utils";

export type AgentFileChipProps = {
  /**
   * File location or resource link chip.
   * Figma node: 1029:2722 (Agent / File Chip)
   */
  path: string;
  line?: number;
  onClick?: () => void;
  className?: string;
};

export function AgentFileChip({
  path,
  line,
  onClick,
  className,
}: AgentFileChipProps) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        // Figma 1029:2722, read off the instance inside the composer
        // (I1048:3173): state/soft fill, no border, radius/6, px-2 py-1 which
        // is the 24 tall the standalone node draws, gap/1, and a Code/11 label.
        // It was border-subtle on surface/raised at radius/2 with gap-1.5.
        "inline-flex h-6 max-w-full shrink-0 items-center gap-1 rounded-sm bg-(--state-soft) px-2 py-1 transition-colors hover:bg-surface-tinted cursor-pointer select-none",
        className,
      )}
      data-testid="agent-file-chip"
    >
      <FileCode className="size-(--icon-12) shrink-0 text-text-tertiary" />
      <span className="truncate text-code-12 text-text-secondary">{path}</span>
      {typeof line === "number" && (
        <span className="shrink-0 text-code-12 text-text-tertiary">{`:${line}`}</span>
      )}
    </button>
  );
}
