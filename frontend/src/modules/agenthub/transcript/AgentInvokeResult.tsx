import { CheckCircle2 } from "lucide-react";
import { cn } from "@/lib/utils";
import type { Usage } from "../types";
import { AgentFileChip } from "./AgentFileChip";

export type AgentInvokeResultProps = {
  /**
   * Summary result of an invokeAgent run.
   * Figma node: 948:2223 (Agent / Invoke Result)
   */
  summary: string;
  filesChanged?: string[];
  cost?: Partial<Usage> | null;
  duration?: string;
  className?: string;
};

export function AgentInvokeResult({
  summary,
  filesChanged = [],
  cost,
  duration,
  className,
}: AgentInvokeResultProps) {
  const costLabel =
    cost && typeof cost.amount === "number"
      ? `$${cost.amount.toFixed(4)}`
      : null;

  return (
    <div
      className={cn(
        "flex flex-col w-full max-w-full rounded-md border border-border-subtle bg-surface-base p-3 gap-2.5",
        className,
      )}
      data-testid="agent-invoke-result"
    >
      <div className="flex items-center justify-between gap-2">
        <div className="flex items-center gap-2">
          <CheckCircle2 className="size-(--icon-16) text-status-success shrink-0" />
          <span className="text-title-16 text-text-primary">Task Complete</span>
        </div>

        <div className="flex items-center gap-2 text-caption-11 text-text-tertiary">
          {duration && <span>{duration}</span>}
          {duration && costLabel && <span>·</span>}
          {costLabel && <span>{costLabel}</span>}
        </div>
      </div>

      <div className="text-body-13 text-text-secondary select-text whitespace-pre-wrap break-words">
        {summary}
      </div>

      {filesChanged.length > 0 && (
        <div className="flex flex-col gap-1.5 pt-1 border-t border-border-subtle/50">
          <span className="text-caption-11-medium text-text-tertiary">
            {`Files Modified (${filesChanged.length})`}
          </span>
          <div className="flex flex-wrap gap-1.5">
            {filesChanged.map((path) => (
              <AgentFileChip key={path} path={path} />
            ))}
          </div>
        </div>
      )}
    </div>
  );
}
