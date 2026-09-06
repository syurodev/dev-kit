import { CheckCircle2, Circle, ListChecks, Loader2 } from "lucide-react";
import { cn } from "@/lib/utils";
import type { PlanEntry } from "../types";

export type AgentPlanProps = {
  /**
   * Execution plan checklist.
   * Figma node: 947:2239 (Agent / Plan)
   */
  entries: PlanEntry[];
  className?: string;
};

export function AgentPlan({ entries, className }: AgentPlanProps) {
  const total = entries.length;
  const completed = entries.filter((e) => e.status === "completed").length;
  const progressPercent = total > 0 ? Math.round((completed / total) * 100) : 0;

  return (
    <div
      className={cn(
        "flex flex-col w-full max-w-full rounded-md border border-border-subtle bg-surface-base p-3 gap-2.5",
        className,
      )}
      data-testid="agent-plan"
    >
      <div className="flex items-center justify-between gap-2">
        <div className="flex items-center gap-2">
          <ListChecks className="size-(--icon-14) text-text-secondary shrink-0" />
          <span className="text-body-13-medium text-text-primary">Plan</span>
        </div>

        {total > 0 && (
          <span className="text-caption-11 text-text-tertiary">
            {`${completed} of ${total} completed`}
          </span>
        )}
      </div>

      {total > 0 && (
        <div className="h-1 w-full rounded-full bg-surface-raised overflow-hidden">
          <div
            className="h-full bg-status-success transition-all duration-300"
            style={{ width: `${progressPercent}%` }}
          />
        </div>
      )}

      <div className="flex flex-col gap-2 pt-0.5">
        {entries.map((entry, idx) => {
          const isDone = entry.status === "completed";
          const isRunning = entry.status === "in_progress";

          return (
            <div key={idx} className="flex items-start gap-2 min-w-0">
              {isDone ? (
                <CheckCircle2 className="size-(--icon-14) text-status-success shrink-0 mt-0.5" />
              ) : isRunning ? (
                <Loader2 className="size-(--icon-14) text-status-info shrink-0 animate-spin mt-0.5" />
              ) : (
                <Circle className="size-(--icon-14) text-text-tertiary shrink-0 mt-0.5" />
              )}

              <div className="flex flex-1 items-baseline justify-between gap-2 min-w-0">
                <span
                  className={cn(
                    "text-body-13 break-words",
                    isDone && "text-text-tertiary line-through",
                    isRunning && "text-text-primary",
                    !isDone && !isRunning && "text-text-secondary",
                  )}
                >
                  {entry.content}
                </span>

                {entry.priority && (
                  <span className="text-caption-11 text-text-tertiary shrink-0 uppercase">
                    {entry.priority}
                  </span>
                )}
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}
