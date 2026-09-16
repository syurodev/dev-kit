import { Shimmer } from "@/components/ui/shimmer";
import { Hourglass } from "lucide-react";

import { cn } from "@/lib/utils";
import type { AsyncTask } from "../types";

export type AgentAsyncTaskProps = {
  task: AsyncTask;
  /** `row` is one step inside a turn's work; `card` stands on its own. */
  variant?: "card" | "row";
  className?: string;
};

/**
 * Work the vendor runs in the background, past the end of the turn that
 * started it.
 *
 * DevKit asks for these (the air extension lists asyncTasks), Go parses all
 * three kinds, and until this nothing drew them: a task could run for minutes
 * after the answer landed and the transcript said nothing was happening. The
 * shape follows AgentSubagent — the quietest thing in a turn, one line, the
 * state as a word — because like a subagent it is not a DevKit session: the
 * composer cannot reach it and the swarm tree must not offer it.
 *
 * What it adds over a subagent is what a background task has to answer:
 * what it is doing right now (lastToolName) and what it has cost so far
 * (tool uses and time), since the reason to look at it is to decide whether
 * it is worth waiting for.
 */
const STATE_DOT: Record<string, string> = {
  running: "bg-status-info",
  pending: "bg-text-tertiary",
  completed: "bg-status-success",
  failed: "bg-status-danger",
  killed: "bg-text-tertiary",
  cancelled: "bg-text-tertiary",
};

const RUNNING_STATES = new Set(["running", "pending", ""]);

/** "2 tools · 1m 12s" — what the task has spent, or nothing until it has. */
export function asyncTaskSpent(task: AsyncTask): string {
  const usage = task.usage;
  if (!usage) return "";
  const parts: string[] = [];
  if (usage.toolUses > 0) parts.push(`${usage.toolUses} tool${usage.toolUses === 1 ? "" : "s"}`);
  if (usage.durationMs >= 1000) {
    const total = Math.round(usage.durationMs / 1000);
    const minutes = Math.floor(total / 60);
    const seconds = total % 60;
    parts.push(minutes > 0 ? `${minutes}m ${seconds}s` : `${seconds}s`);
  }
  return parts.join(" · ");
}

export function AgentAsyncTask({ task, variant = "card", className }: AgentAsyncTaskProps) {
  const state = task.state || "running";
  const running = RUNNING_STATES.has(state);
  const name = task.name || task.description || "Background task";
  // While it runs, what it is doing; once it is done, what it found.
  const detail = running ? task.lastToolName : task.summary;
  const spent = asyncTaskSpent(task);

  if (variant === "row") {
    return (
      <div
        data-testid="agent-async-task"
        data-variant="row"
        data-state={state}
        className={cn("flex w-full min-w-0 items-center gap-2 text-body-13", className)}
      >
        <Hourglass className="size-3.5 shrink-0 text-icon-secondary" aria-hidden="true" />
        <span className="shrink-0 text-text-secondary">{name}</span>
        {detail ? <span className="min-w-0 truncate text-text-tertiary">{detail}</span> : null}
        {spent ? (
          <span className="ml-auto shrink-0 text-caption-11 text-text-tertiary whitespace-nowrap">{spent}</span>
        ) : null}
        <Shimmer
          active={running}
          className={cn("shrink-0 text-text-tertiary", !spent && "ml-auto")}
        >
          {state}
        </Shimmer>
      </div>
    );
  }

  return (
    <div
      data-testid="agent-async-task"
      data-state={state}
      className={cn(
        "flex w-full max-w-full flex-col gap-1 rounded-control bg-(--state-soft) px-3 py-2",
        className,
      )}
    >
      <div className="flex w-full items-center gap-2">
        <span
          aria-hidden="true"
          className={cn("size-1.5 shrink-0 rounded-full", STATE_DOT[state] ?? "bg-text-tertiary")}
        />
        <Hourglass className="size-3 shrink-0 text-icon-secondary" aria-hidden="true" />
        <span className="min-w-0 flex-1 truncate text-caption-11-medium text-text-secondary">{name}</span>
        {spent ? (
          <span className="shrink-0 text-caption-11 text-text-tertiary whitespace-nowrap">{spent}</span>
        ) : null}
        <Shimmer active={running} className="shrink-0 text-caption-11 whitespace-nowrap text-text-tertiary">
          {state}
        </Shimmer>
      </div>
      {detail ? <p className="min-w-0 truncate text-caption-11 text-text-tertiary">{detail}</p> : null}
    </div>
  );
}
