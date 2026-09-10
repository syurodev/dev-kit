import { GitBranch } from "lucide-react";

import { cn } from "@/lib/utils";
import type { Subagent } from "../types";

export type AgentSubagentProps = {
  subagent: Subagent;
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
  running: "bg-status-info",
  completed: "bg-status-success",
  failed: "bg-status-danger",
  cancelled: "bg-text-tertiary",
};

export function AgentSubagent({ subagent, className }: AgentSubagentProps) {
  const state = subagent.state ?? "running";

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
        <span className="shrink-0 text-caption-11 whitespace-nowrap text-text-tertiary">
          {state}
        </span>
      </div>
      {subagent.task ? (
        // What it was actually told to do. Without this the row says a
        // subagent existed and nothing about why, which is not worth a row.
        <p className="w-full text-caption-11 break-words text-text-tertiary">
          {subagent.task}
        </p>
      ) : null}
    </div>
  );
}
