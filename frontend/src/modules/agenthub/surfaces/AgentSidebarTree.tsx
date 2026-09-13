import * as React from "react";
import { Ban, Copy, FolderOpen, Pin, PinOff, Plus, Square, Trash2 } from "lucide-react";

import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuLabel,
  ContextMenuSeparator,
  ContextMenuTrigger,
} from "@/components/ui/context-menu";
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
  /** Session ids the user pinned; these sort to the top, lineage intact. */
  pinnedIds?: string[];
  onTogglePin?: (sessionId: string) => void;
  /** Spawns a child of this session — the context menu's only creating action. */
  onNewChildSession?: (parent: AgentSession) => void;
  onCancelTurn?: (session: AgentSession) => void;
  onCloseSession?: (session: AgentSession) => void;
  onDeleteSession?: (session: AgentSession, descendants: number) => void;
  onCopy?: (text: string) => void;
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
 * The waiting count lives in the footer, which is why Agent / Row carries no
 * per-row badge: one number for the whole tree, where you look when deciding
 * whether anything needs you at all.
 */

/**
 * Depth-first, parents before their children, so indentation reads as lineage.
 *
 * Pinning reorders roots only. A pinned child hoisted out of its subtree would
 * be a child drawn above its own parent, and the indentation — which Figma's
 * note calls "the boundary an agent may act within" — would then be a lie.
 */
function orderByLineage(sessions: AgentSession[], pinnedIds: string[] = []): AgentSession[] {
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

  const pinRank = (session: AgentSession) => {
    const at = pinnedIds.indexOf(session.id);
    return at === -1 ? Number.MAX_SAFE_INTEGER : at;
  };
  roots.sort((a, b) => pinRank(a) - pinRank(b));

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

/**
 * How many sessions go with each one if it is deleted, itself excluded.
 *
 * Exported for its test: this number is the label on a destructive action, and
 * a wrong one means the user agreed to lose something they were not told about.
 */
export function countDescendants(sessions: AgentSession[]): Record<string, number> {
  const children = new Map<string, string[]>();
  for (const session of sessions) {
    if (!session.parentSessionId) continue;
    const list = children.get(session.parentSessionId) ?? [];
    list.push(session.id);
    children.set(session.parentSessionId, list);
  }
  const counts: Record<string, number> = {};
  const count = (id: string): number => {
    if (counts[id] !== undefined) return counts[id];
    counts[id] = 0; // guards a cycle, which the depth limit should prevent anyway
    let total = 0;
    for (const child of children.get(id) ?? []) total += 1 + count(child);
    counts[id] = total;
    return total;
  };
  for (const session of sessions) count(session.id);
  return counts;
}

export function AgentSidebarTree({
  sessions,
  activeSessionId,
  pendingCounts = {},
  turnCounts = {},
  onSelectSession,
  onNewSession,
  pinnedIds = [],
  onTogglePin,
  onNewChildSession,
  onCancelTurn,
  onCloseSession,
  onDeleteSession,
  onCopy,
  className,
}: AgentSidebarTreeProps) {
  const ordered = React.useMemo(
    () => orderByLineage(sessions, pinnedIds),
    [sessions, pinnedIds],
  );
  const descendantCounts = React.useMemo(() => countDescendants(sessions), [sessions]);

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
      className={cn("flex h-full w-full max-w-66 flex-col", className)}
    >
      <div className="flex w-full shrink-0 items-center gap-2 py-3">
        <span className="min-w-0 flex-1 text-label-12 text-text-secondary">
          Swarm
        </span>
        <Button
          type="button"
          variant="outline"
          size="xs"
          data-testid="agent-tree-new"
          onClick={onNewSession}
          className="h-(--control-height-xs) rounded-md border-border-subtle px-2 text-caption-11-medium text-text-secondary"
        >
          + New
        </Button>
      </div>

      <div className="h-px w-full shrink-0 bg-border-subtle" />

      <div className="flex min-h-0 w-full flex-1 flex-col gap-1 overflow-y-auto py-2">
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
              <ContextMenu>
                <ContextMenuTrigger asChild>
                  <AgentRow
                    session={session}
                    selected={session.id === activeSessionId}
                    turnCount={turnCounts[session.id]}
                    pinned={pinnedIds.includes(session.id)}
                    onClick={() => onSelectSession(session.id)}
                    className="max-w-none flex-1"
                  />
                </ContextMenuTrigger>
                <ContextMenuContent className="w-56">
                  <ContextMenuLabel className="truncate">
                    {session.title || session.id.slice(0, 8)}
                  </ContextMenuLabel>
                  <ContextMenuSeparator />

                  {onTogglePin ? (
                    <ContextMenuItem
                      icon={pinnedIds.includes(session.id) ? <PinOff /> : <Pin />}
                      onSelect={() => onTogglePin(session.id)}
                    >
                      {pinnedIds.includes(session.id) ? "Unpin" : "Pin to top"}
                    </ContextMenuItem>
                  ) : null}

                  {onNewChildSession ? (
                    <ContextMenuItem icon={<Plus />} onSelect={() => onNewChildSession(session)}>
                      New child session
                    </ContextMenuItem>
                  ) : null}

                  {onCopy ? (
                    <>
                      <ContextMenuSeparator />
                      <ContextMenuItem icon={<FolderOpen />} onSelect={() => onCopy(session.projectPath)}>
                        Copy working directory
                      </ContextMenuItem>
                      <ContextMenuItem icon={<Copy />} onSelect={() => onCopy(session.id)}>
                        Copy session ID
                      </ContextMenuItem>
                    </>
                  ) : null}

                  {onCancelTurn || onCloseSession || onDeleteSession ? (
                    <ContextMenuSeparator />
                  ) : null}

                  {/* Cancel stops the turn and leaves the agent running;
                      close ends the process. Both are meaningless once the
                      session is closed, so they say so by being disabled
                      rather than by disappearing — a menu whose items move
                      between visits is a menu you have to read every time. */}
                  {onCancelTurn ? (
                    <ContextMenuItem
                      icon={<Ban />}
                      disabled={session.status !== "running"}
                      onSelect={() => onCancelTurn(session)}
                    >
                      Cancel turn
                    </ContextMenuItem>
                  ) : null}

                  {onCloseSession ? (
                    <ContextMenuItem
                      icon={<Square />}
                      disabled={session.status === "closed"}
                      onSelect={() => onCloseSession(session)}
                    >
                      Close session
                    </ContextMenuItem>
                  ) : null}

                  {onDeleteSession ? (
                    <ContextMenuItem
                      icon={<Trash2 />}
                      variant="destructive"
                      onSelect={() =>
                        onDeleteSession(session, descendantCounts[session.id] ?? 0)
                      }
                    >
                      {/* Says what will actually go. Deleting a parent takes
                          its subtree, and finding that out afterwards is the
                          one thing this action cannot undo. */}
                      {descendantCounts[session.id]
                        ? `Delete session and ${descendantCounts[session.id]} ${
                            descendantCounts[session.id] === 1 ? "child" : "children"
                          }`
                        : "Delete session"}
                    </ContextMenuItem>
                  ) : null}
                </ContextMenuContent>
              </ContextMenu>
            </div>
          );
        })}
      </div>

      {waiting > 0 ? (
        <div
          data-testid="agent-tree-attention"
          className="flex w-full shrink-0 items-center gap-2 bg-(--state-soft) px-2 py-3"
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
