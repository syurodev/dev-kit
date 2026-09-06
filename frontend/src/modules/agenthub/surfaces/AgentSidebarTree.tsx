import * as React from "react";

import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { AgentRow } from "./AgentRow";
import type { AgentSession } from "@/bridge/agenthub";

export type AgentSidebarTreeProps = {
  sessions: AgentSession[];
  activeSessionId?: string;
  /** Per-session pending permissions; summed into the attention footer. */
  pendingCounts?: Record<string, number>;
  turnCounts?: Record<string, number>;
  onSelectSession: (sessionId: string) => void;
  onNewSession: () => void;
  className?: string;
};

/**
 * Agent Hub / Sidebar Content — Figma `917:101`, 264x836.
 *
 * Figma's note: "The swarm as a tree: indentation is the parent→child chain,
 * which is also the boundary an agent may act within."
 *
 * So depth is not decoration. A child is inset by space/4 AND gets a 1px guide
 * line down its left edge — the line is what makes the chain readable when two
 * siblings sit next to each other, and padding alone does not draw it.
 *
 * The footer is where the waiting count lives. That is why Agent / Row carries
 * no per-row badge: one number for the whole tree, in the place you look when
 * you are deciding whether anything needs you at all.
 */

/** Depth-first, parents before their children, so indentation reads as lineage. */
function orderByLineage(sessions: AgentSession[]): AgentSession[] {
  const children = new Map<string, AgentSession[]>();
  const roots: AgentSession[] = [];

  for (const session of sessions) {
    if (session.parentSessionId) {
      const list = children.get(session.parentSessionId) ?? [];
      list.push(session);
      children.set(session.parentSessionId, list);
    } else {
      roots.push(session);
    }
  }

  const out: AgentSession[] = [];
  const walk = (node: AgentSession) => {
    out.push(node);
    for (const child of children.get(node.id) ?? []) {
      walk(child);
    }
  };
  roots.forEach(walk);

  // A child whose parent is not in the list would vanish entirely, so anything
  // unvisited is appended rather than dropped.
  const seen = new Set(out.map((s) => s.id));
  for (const session of sessions) {
    if (!seen.has(session.id)) out.push(session);
  }
  return out;
}

export function AgentSidebarTree({
  sessions,
  activeSessionId,
  pendingCounts = {},
  turnCounts = {},
  onSelectSession,
  onNewSession,
  className,
}: AgentSidebarTreeProps) {
  const ordered = React.useMemo(() => orderByLineage(sessions), [sessions]);

  const waiting = React.useMemo(
    () =>
      ordered.filter(
        (s) => s.status === "awaiting_permission" || (pendingCounts[s.id] ?? 0) > 0,
      ).length,
    [ordered, pendingCounts],
  );

  return (
    <div
      data-testid="agent-sidebar-tree"
      className={cn("flex h-full w-full max-w-[264px] flex-col", className)}
    >
      <div className="flex w-full shrink-0 items-center gap-2 p-3">
        <span className="min-w-0 flex-1 text-label-12 text-text-secondary">
          Swarm
        </span>
        <Button
          type="button"
          variant="outline"
          size="xs"
          data-testid="agent-tree-new"
          onClick={onNewSession}
          className="h-6 rounded-[6px] border-border-subtle px-2 text-caption-11-medium text-text-secondary"
        >
          + New
        </Button>
      </div>

      <div className="h-px w-full shrink-0 bg-border-subtle" />

      <div className="flex min-h-0 w-full flex-1 flex-col gap-1 overflow-y-auto p-2">
        {ordered.map((session) => {
          const child = Boolean(session.parentSessionId);
          return (
            <div
              key={session.id}
              className={cn("flex w-full items-start", child && "gap-2 pl-4")}
            >
              {/* The guide is the chain. Without it, two siblings at the same
                  indent read as two roots that happen to be pushed right. */}
              {child ? (
                <span
                  aria-hidden="true"
                  data-testid="agent-tree-guide"
                  className="w-px shrink-0 self-stretch bg-border-subtle"
                />
              ) : null}
              <AgentRow
                session={session}
                selected={session.id === activeSessionId}
                turnCount={turnCounts[session.id]}
                onClick={() => onSelectSession(session.id)}
                className="max-w-none flex-1"
              />
            </div>
          );
        })}
      </div>

      {waiting > 0 ? (
        <div
          data-testid="agent-tree-attention"
          className="flex w-full shrink-0 items-center gap-2 bg-(--state-soft) p-3"
        >
          <span
            aria-hidden="true"
            className="size-1.5 shrink-0 rounded-full bg-status-warning"
          />
          <p className="min-w-0 flex-1 text-caption-11-medium text-text-primary">
            {`${waiting} agent${waiting === 1 ? "" : "s"} waiting on you`}
          </p>
        </div>
      ) : null}
    </div>
  );
}
