import { Shimmer } from "@/components/ui/shimmer";
import { ChevronRight, GitBranch } from "lucide-react";

import { cn } from "@/lib/utils";
import type { Subagent } from "../types";

export type AgentSubagentProps = {
  subagent: Subagent;
  /** `row` is one step inside a turn's work: its name, what it was told, how it ended. */
  variant?: "card" | "row";
  /** How many steps it took, when its activity is known. Row variant only. */
  steps?: number;
  /**
   * Makes the row the toggle for what the subagent did, which the caller
   * draws underneath. Row variant only; without it the row is a plain line.
   */
  expanded?: boolean;
  onToggle?: () => void;
  className?: string;
};

/**
 * An agent the vendor ran inside its own process.
 *
 * There is no Figma node for this: the Agent Hub design covers DevKit's own
 * sessions, which have lineage, depth and a composer. A vendor subagent has
 * none of those — nothing here created it, the composer cannot address it, and
 * closing it is not ours to do. So this is deliberately the quietest thing in
 * the transcript rather than an invented card: state/soft, one line of what it
 * was asked to do, and the state.
 *
 * Not in the swarm tree beside real sessions: that would have the sidebar claim
 * you can click into something which is not a session.
 *
 * Two updates, one card: spawn carries the name and the task, the state update
 * carries only the id and the new state.
 */
const STATE_DOT: Record<string, string> = {
  running: "bg-(--status-running)",
  completed: "bg-status-success",
  failed: "bg-status-danger",
  cancelled: "bg-text-tertiary",
};

export function AgentSubagent({
  subagent,
  variant = "card",
  steps,
  expanded = false,
  onToggle,
  className,
}: AgentSubagentProps) {
  const state = subagent.state ?? "running";

  if (variant === "row") {
    const contents = (
      <>
        <GitBranch className="size-3.5 shrink-0 text-icon-secondary" aria-hidden="true" />
        <span className="min-w-0 truncate text-text-secondary">{subagent.name || "Subagent"}</span>
        {/* The task is a whole brief — "Repo: …, branch …" is its first line,
            and a first line is all a row has room for. Where the row opens,
            the brief is inside it in full, so it is not repeated here. */}
        {!onToggle && subagent.task ? (
          <span className="min-w-0 truncate text-text-tertiary">{subagent.task}</span>
        ) : null}
        {steps ? (
          <span className="shrink-0 text-text-tertiary">
            {steps === 1 ? "1 step" : `${steps} steps`}
          </span>
        ) : null}
        {/* The state as a word, not a dot: a coloured dot on its own is a
            meaning only some people can read. */}
        <Shimmer active={state === "running"} className="ml-auto shrink-0 text-text-tertiary">{state}</Shimmer>
        {onToggle ? (
          <ChevronRight
            aria-hidden="true"
            className={cn(
              "size-3.5 shrink-0 text-text-tertiary transition-transform duration-(--motion-fast)",
              expanded && "rotate-90",
            )}
          />
        ) : null}
      </>
    );
    const rowClass = cn("flex w-full min-w-0 items-center gap-2 text-left text-body-13", className);
    return onToggle ? (
      <button
        type="button"
        data-testid="agent-subagent"
        data-variant="row"
        data-state={state}
        aria-expanded={expanded}
        onClick={onToggle}
        className={cn(rowClass, "cursor-pointer hover:text-text-primary")}
      >
        {contents}
      </button>
    ) : (
      <div data-testid="agent-subagent" data-variant="row" data-state={state} className={rowClass}>
        {contents}
      </div>
    );
  }

  return (
    <div
      data-testid="agent-subagent"
      data-state={state}
      className={cn(
        "flex w-full max-w-full flex-col gap-1 rounded-control bg-(--state-soft) px-3 py-2",
        className,
      )}
    >
      <div className="flex w-full items-center gap-2">
        <span
          aria-hidden="true"
          className={cn(
            "size-1.5 shrink-0 rounded-full",
            STATE_DOT[state] ?? "bg-text-tertiary",
          )}
        />
        <GitBranch className="size-3 shrink-0 text-icon-secondary" aria-hidden="true" />
        <span className="min-w-0 flex-1 truncate text-caption-11-medium text-text-secondary">
          {subagent.name || "Subagent"}
        </span>
        <Shimmer active={state === "running"} className="shrink-0 text-caption-11 whitespace-nowrap text-text-tertiary">
          {state}
        </Shimmer>
      </div>
      {subagent.task ? (
        // What it was actually told to do. Without this the row says a
        // subagent existed and nothing about why, which is not worth a row.
        <p className="w-full text-caption-11 wrap-anywhere text-text-tertiary">
          {subagent.task}
        </p>
      ) : null}
    </div>
  );
}
