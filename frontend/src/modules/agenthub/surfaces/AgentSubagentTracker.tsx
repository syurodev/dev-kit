import * as React from "react";
import { ChevronRight, GitBranch } from "lucide-react";

import { cn } from "@/lib/utils";
import { Shimmer } from "@/components/ui/shimmer";
import { AnimatedCollapse } from "@/components/ui/animated-collapse";
import { currentStep, runningSubagents, stepCount } from "../turns";
import type { Subagent, TranscriptItem } from "../types";
import { sessionRunningSubagents } from "../subagents";
import { SubagentActivity } from "./AgentTurn";

export type AgentSubagentTrackerProps = {
  items: TranscriptItem[];
  /** A turn is running: the transcript's own count of who is running is trusted only then. */
  live: boolean;
  /**
   * The session's tracked subagents (the store's `subagents`): what says one
   * is still running after the lead agent's turn has ended to wait for it.
   */
  tracked?: readonly Subagent[];
  onOpenFile?: (path: string, line?: number) => void;
  /**
   * "tray": the list only, for the composer tray's panel — the tray is the
   * surface and its strip already counts them, so no card and no count line.
   */
  variant?: "card" | "tray";
  className?: string;
};

/**
 * The subagents working right now, pinned above the composer.
 *
 * In the transcript a subagent is one row inside the turn's folded steps,
 * wherever the turn was when it was spawned — fine for reading back, useless
 * for watching: a lead agent that sends two subagents off and keeps working
 * scrolls their rows away, and the rail, which has no sidebar, had nowhere
 * else to say they exist. Here each one is a line saying what it is doing this
 * moment; opening it follows its steps as they land.
 *
 * Nothing is drawn when none is running, so it costs an idle session nothing.
 */
export function AgentSubagentTracker({ items, live, tracked, onOpenFile, variant = "card", className }: AgentSubagentTrackerProps) {
  const { running, activity } = React.useMemo(
    () => ({ running: sessionRunningSubagents(items, tracked, live), activity: runningSubagents(items).activity }),
    [items, tracked, live],
  );
  const [openId, setOpenId] = React.useState<string | null>(null);
  if (running.length === 0) return null;

  return (
    <div
      data-testid="agent-subagent-tracker"
      className={cn(
        "flex w-full min-w-0 flex-col gap-1 p-2",
        variant === "card" && "glass-float glass-float-low rounded-xl",
        variant === "tray" && "max-h-72 overflow-y-auto",
        className,
      )}
    >
      {variant === "card" ? (
        <span className="px-1 text-caption-11 text-text-tertiary">
          {running.length === 1 ? "1 subagent running" : `${running.length} subagents running`}
        </span>
      ) : null}
      {running.map(({ subagent, activity: rows }) => {
        const open = subagent.id === openId;
        const steps = stepCount(rows);
        return (
          <div key={subagent.id} className="flex w-full min-w-0 flex-col">
            {/* Two lines, not one: at the rail's 320px a name and a step on
                one line left a few letters of each. */}
            <button
              type="button"
              data-testid="tracked-subagent"
              aria-expanded={open}
              onClick={() => setOpenId(open ? null : subagent.id)}
              className="flex w-full min-w-0 cursor-pointer transition-control focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-border-focus/50 flex-col rounded-sm px-1 py-0.5 text-left text-body-13 hover:bg-state-soft"
            >
              <span className="flex w-full min-w-0 items-center gap-2">
                <GitBranch className="size-3.5 shrink-0 text-icon-secondary" aria-hidden="true" />
                <span className="min-w-0 flex-1 truncate text-text-secondary">{subagent.name || "Subagent"}</span>
                {steps ? (
                  <span className="shrink-0 text-caption-11 text-text-tertiary">
                    {steps === 1 ? "1 step" : `${steps} steps`}
                  </span>
                ) : null}
                <ChevronRight
                  aria-hidden="true"
                  className={cn(
                    "size-3.5 shrink-0 text-text-tertiary transition-transform duration-(--motion-fast) ease-(--ease-out-strong) motion-reduce:transition-none",
                    open && "rotate-90",
                  )}
                />
              </span>
              {/* What it is doing this moment: the reason to look here at all.
                  Under the name, not under the icon: 14px icon + 8px gap. */}
              <Shimmer active className="block w-full min-w-0 truncate pl-5.5 text-caption-11 text-text-tertiary">
                {currentStep(rows)}
              </Shimmer>
            </button>
            <AnimatedCollapse open={open}>
              <FollowWell rows={rows}>
                <SubagentActivity
                  subagent={subagent}
                  activity={rows}
                  subagents={activity}
                  onOpenFile={onOpenFile}
                  brief={false}
                  live
                />
              </FollowWell>
            </AnimatedCollapse>
          </div>
        );
      })}
    </div>
  );
}

/**
 * A bounded well that stays at its foot while new steps arrive, unless the
 * person scrolled up to read — then it leaves them where they are.
 */
function FollowWell({ rows, children }: { rows: TranscriptItem[]; children: React.ReactNode }) {
  const ref = React.useRef<HTMLDivElement>(null);
  const pinned = React.useRef(true);
  React.useLayoutEffect(() => {
    const el = ref.current;
    if (el && pinned.current) el.scrollTop = el.scrollHeight;
  }, [rows]);
  return (
    <div
      ref={ref}
      data-testid="tracked-subagent-activity"
      onScroll={(event) => {
        const el = event.currentTarget;
        pinned.current = el.scrollHeight - el.scrollTop - el.clientHeight < 24;
      }}
      className="mt-1 max-h-60 overflow-y-auto pt-1 pr-1"
    >
      {children}
    </div>
  );
}
