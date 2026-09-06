import { cn } from "@/lib/utils";
import type { PlanEntry } from "../types";

export type AgentPlanProps = {
  entries: PlanEntry[];
  className?: string;
};

/**
 * Agent / Plan — Figma `947:2239`.
 *
 * Figma's note: "FR-3 lists plan alongside text chunk and tool call as a
 * session/update kind, and it had no surface at all. Steps carry ACP's own
 * statuses (pending / in_progress / completed) so the transcript shows where
 * the agent is, not merely that it made a plan."
 *
 * So the status word is printed, per step, in ACP's own vocabulary. The
 * previous implementation replaced it with a progress bar plus line-through —
 * a bar says how far, not where, and strike-through on a completed step reads
 * as cancelled.
 *
 * Marks, read off the exported SVGs rather than guessed:
 *   completed    filled #99E1D9 — status/success
 *   in_progress  filled #B2D5E5 — status/info, matching Tool Call running
 *   pending      1px ring #8B8E8A — text/tertiary, hollow because nothing
 *                has happened yet
 *
 * Only the in-progress step steps up to Medium/primary. That is the line you
 * are looking for when you glance at a plan.
 */
const STEP: Record<string, { mark: string; label: string; word: string }> = {
  completed: {
    mark: "bg-status-success",
    label: "text-caption-11 text-text-secondary",
    word: "completed",
  },
  in_progress: {
    mark: "bg-status-info",
    label: "text-caption-11-medium text-text-primary",
    word: "in progress",
  },
  pending: {
    mark: "border border-text-tertiary",
    label: "text-caption-11 text-text-tertiary",
    word: "pending",
  },
};

export function AgentPlan({ entries, className }: AgentPlanProps) {
  const completed = entries.filter((e) => e.status === "completed").length;

  return (
    <div
      data-testid="agent-plan"
      className={cn(
        "flex w-full max-w-full flex-col gap-3 rounded-control bg-(--state-soft) p-3",
        className,
      )}
    >
      <div className="flex w-full items-center gap-2 text-caption-11 text-text-tertiary">
        <span className="min-w-0 flex-1 text-caption-11-medium">Plan</span>
        {entries.length > 0 ? (
          <span className="shrink-0 whitespace-nowrap">
            {`${completed} of ${entries.length}`}
          </span>
        ) : null}
      </div>

      {entries.map((entry, idx) => {
        const step = STEP[entry.status ?? "pending"] ?? STEP.pending;
        return (
          <div
            key={idx}
            data-testid="agent-plan-step"
            data-status={entry.status ?? "pending"}
            className="flex w-full items-center gap-2"
          >
            <span
              aria-hidden="true"
              className={cn("size-1.5 shrink-0 rounded-full", step.mark)}
            />
            <span className={cn("min-w-0 flex-1 break-words", step.label)}>
              {entry.content}
            </span>
            <span className="shrink-0 text-caption-11 whitespace-nowrap text-text-tertiary">
              {step.word}
            </span>
          </div>
        );
      })}
    </div>
  );
}
