import { cn } from "@/lib/utils";
import type { Usage } from "../types";

export type AgentInvokeResultProps = {
  summary: string;
  filesChanged?: string[];
  cost?: Partial<Usage> | null;
  duration?: string;
  /** Which agent finished. Figma leads with the name: "refund-worker returned". */
  agentName?: string;
  /** ACP's stop reason, e.g. end_turn / max_tokens / cancelled. */
  stopReason?: string;
  onOpenDiff?: (path: string) => void;
  className?: string;
};

/**
 * Agent / Invoke Result — Figma `948:2223`.
 *
 * Figma's note: "FR-5: invokeAgent hands back summary and filesChanged, and
 * neither reached the screen. The file list is the part that matters — it is
 * what a master agent hands you to review after a worker finishes."
 *
 * So the files are full-width rows with an Open diff affordance, not the little
 * inline chips a tool call uses. A chip says a file was touched in passing; a
 * row is a queue of things to review.
 *
 * The head is "{agent} returned", not "Task Complete" — with a swarm running,
 * an unattributed completion tells you nothing, the same reason the permission
 * request names who is asking.
 */
export function AgentInvokeResult({
  summary,
  filesChanged = [],
  cost,
  duration,
  agentName = "Agent",
  stopReason,
  onOpenDiff,
  className,
}: AgentInvokeResultProps) {
  const meta = [
    stopReason,
    duration,
    // Figma shows 2dp ($0.08). Sub-cent turns are common on short tool turns,
    // and rounding $0.0025 to $0.00 makes a running total look free.
    cost && typeof cost.amount === "number"
      ? `$${cost.amount.toFixed(cost.amount < 0.01 ? 4 : 2)}`
      : null,
  ]
    .filter(Boolean)
    .join(" · ");

  return (
    <div
      data-testid="agent-invoke-result"
      className={cn(
        "flex w-full max-w-full flex-col gap-3 rounded-[10px] p-3",
        "border border-border-subtle bg-surface-raised",
        className,
      )}
    >
      <div className="flex w-full items-center gap-2">
        <span
          aria-hidden="true"
          className="size-1.5 shrink-0 rounded-full bg-status-success"
        />
        <p className="min-w-0 flex-1 text-label-12 text-text-primary">
          {`${agentName} returned`}
        </p>
        {meta ? (
          <p className="shrink-0 text-caption-11 whitespace-nowrap text-text-tertiary">
            {meta}
          </p>
        ) : null}
      </div>

      <p className="w-full text-body-12 break-words whitespace-pre-wrap text-text-secondary select-text">
        {summary}
      </p>

      {filesChanged.length > 0 ? (
        <>
          <div className="flex w-full items-start gap-2 text-caption-11 text-text-tertiary">
            <span className="text-caption-11-medium">Files changed</span>
            <span>{filesChanged.length}</span>
          </div>
          {filesChanged.map((path) => (
            <div
              key={path}
              data-testid="invoke-result-file"
              className="flex w-full items-center gap-2 rounded-[6px] bg-(--state-soft) px-2 py-1 text-caption-11"
            >
              <span className="min-w-0 flex-1 truncate text-text-secondary">{path}</span>
              {onOpenDiff ? (
                <button
                  type="button"
                  onClick={() => onOpenDiff(path)}
                  className="shrink-0 whitespace-nowrap text-text-tertiary hover:text-text-primary cursor-pointer"
                >
                  Open diff
                </button>
              ) : null}
            </div>
          ))}
        </>
      ) : null}
    </div>
  );
}
