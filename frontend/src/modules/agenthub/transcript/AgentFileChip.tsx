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
        "inline-flex h-(--control-height-xs) max-w-full shrink-0 items-center gap-1.5 rounded-sm border border-border-subtle bg-surface-raised px-2 transition-colors hover:bg-surface-tinted cursor-pointer select-none",
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
