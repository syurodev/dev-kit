import * as React from "react";
import { Ban, Bot, Copy, Eye, EyeOff, Folder, FolderOpen, Pin, PinOff, Plus, Square, Trash2 } from "lucide-react";

import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { ActionTooltip } from "@/components/ui/action-tooltip";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuLabel,
  ContextMenuSeparator,
  ContextMenuTrigger,
} from "@/components/ui/context-menu";
import { TreeView, type TreeNodeData } from "@/components/tree-view";
import { Shimmer } from "@/components/ui/shimmer";
import { AgentState, type AgentStatus } from "./AgentState";
import { heldChoice } from "./AgentComposer";
import { VendorIcon } from "./VendorIcon";
import type { AgentSession } from "@/bridge/agenthub";
import type { Subagent } from "../types";

export type AgentSidebarTreeProps = {
  sessions: AgentSession[];
  activeSessionId?: string;
  /** Per-session pending permissions; summed into the attention footer. */
  pendingCounts?: Record<string, number>;
  turnCounts?: Record<string, number>;
  /**
   * The agents Claude started inside a session of its own, by session id. Not
   * sessions: DevKit did not create them and cannot open them, so their rows
   * select the session they belong to.
   */
  subagents?: Record<string, Subagent[]>;
  onSelectSession: (sessionId: string) => void;
  onNewSession: () => void;
  /** Session ids the user pinned; these sort to the top of their project. */
  pinnedIds?: string[];
  onTogglePin?: (sessionId: string) => void;
  /** Node keys the user folded shut. Absent means open — a new project shows. */
  collapsedKeys?: string[];
  onToggleCollapsed?: (key: string) => void;
  /**
   * Closed sessions are left out of the tree. The selected one always shows,
   * and so does a closed parent with a live session under it — hiding that
   * would hoist the child to a root and have the indentation say something
   * untrue about who may reach whom.
   */
  hideClosed?: boolean;
  onToggleHideClosed?: () => void;
  /**
   * Starts a session in this project's folder: the "+" on a project row and
   * its context menu. The folder is already known, so nothing is asked.
   */
  onNewSessionIn?: (projectPath: string) => void;
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
 * Three things nest here, and they are not the same kind of thing:
 *
 *   project     where the agents were opened. Sessions carry a projectPath and
 *               a developer runs several repos at once; without this level the
 *               rail is one flat list where the only clue which repo a session
 *               belongs to is a truncated path on its second line.
 *   session     a DevKit session. Selectable, and the only row that is.
 *   subagent    an agent Claude started inside a session of its own. Shown
 *               because a swarm that only exists in the transcript cannot be
 *               seen from the rail at all.
 *
 * Grouping applies to roots only. A child session keeps its place under its
 * parent even when it was opened on a different path: lineage is the boundary
 * an agent may act within, and hoisting a child into another folder would make
 * the indentation say something untrue.
 *
 * Rows come from components/tree-view, the same tree the database and Kafka
 * rails use: guides, sticky headers, keyboard semantics and windowing are
 * already there and were previously hand-rolled here as a 1px span and a pl-4.
 * Its rows are one line of 24px, which is what moved the project out to its own
 * level — a second line per session repeated the folder for every one of them.
 *
 * The waiting count lives in the footer: one number for the whole tree, where
 * you look when deciding whether anything needs you at all.
 */

/**
 * The bridge's SessionStatus and Figma's Agent / State are two vocabularies:
 * the backend says `awaiting_permission`, the design says `awaiting`.
 */
function toAgentStatus(status: string): AgentStatus {
  switch (status) {
    case "running":
      return "running";
    case "awaiting_permission":
      return "awaiting";
    case "error":
      return "error";
    case "closed":
      return "closed";
    default:
      return "idle";
  }
}

/**
 * A subagent reports four end states and nothing at all while it works, since
 * the spawn notification carries no state. No state means it is still going.
 *
 * Only two of the four reach a row: a killed subagent leaves the tree rather
 * than sitting in it greyed out (stores/agentSessions, KILLED_STATES).
 */
function toSubagentStatus(state: string | undefined): AgentStatus {
  switch (state) {
    case "completed":
      return "closed";
    case "failed":
      return "error";
    default:
      return "running";
  }
}

/** The last segment of a path — the folder a developer calls the project. */
function projectLabel(path: string): string {
  return path.split("/").filter(Boolean).pop() ?? path;
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

/**
 * The next session waiting on the user, after the one selected — so the
 * footer's "N agents waiting on you" is a place to go, not just a count, and
 * pressing it again walks on to the next one.
 */
export function nextWaiting(waitingIds: string[], activeSessionId?: string): string | undefined {
  if (waitingIds.length === 0) return undefined;
  const at = activeSessionId ? waitingIds.indexOf(activeSessionId) : -1;
  return waitingIds[(at + 1) % waitingIds.length];
}

/**
 * Which sessions the tree draws when closed ones are hidden. A closed row
 * stays if it is selected, or if anything alive descends from it.
 */
export function visibleSessions(
  sessions: AgentSession[],
  hideClosed: boolean,
  activeSessionId?: string,
): AgentSession[] {
  if (!hideClosed) return sessions;
  const byId = new Map(sessions.map((session) => [session.id, session]));
  const liveAncestors = new Set<string>();
  for (const session of sessions) {
    if (session.status === "closed") continue;
    let parent = session.parentSessionId;
    while (parent && !liveAncestors.has(parent)) {
      liveAncestors.add(parent);
      parent = byId.get(parent)?.parentSessionId ?? null;
    }
  }
  return sessions.filter(
    (session) =>
      session.status !== "closed" || session.id === activeSessionId || liveAncestors.has(session.id),
  );
}

/** One project and the root sessions opened in it, pinned ones first. */
export type ProjectGroup = { path: string; roots: AgentSession[]; total: number };

/**
 * Roots grouped by project, with each group's sessions counted through their
 * whole lineage — the count on a folder should say how many agents are in it,
 * not how many of them happen to have no parent.
 *
 * A child whose parent is missing from the list would otherwise disappear with
 * it, so it is treated as a root of its own project rather than dropped.
 */
export function groupByProject(
  sessions: AgentSession[],
  pinnedIds: string[] = [],
): ProjectGroup[] {
  const byId = new Set(sessions.map((session) => session.id));
  const groups = new Map<string, AgentSession[]>();
  for (const session of sessions) {
    if (session.parentSessionId && byId.has(session.parentSessionId)) continue;
    const list = groups.get(session.projectPath) ?? [];
    list.push(session);
    groups.set(session.projectPath, list);
  }

  const pinRank = (session: AgentSession) => {
    const at = pinnedIds.indexOf(session.id);
    return at === -1 ? Number.MAX_SAFE_INTEGER : at;
  };
  const descendants = countDescendants(sessions);

  return [...groups.entries()]
    .map(([path, roots]) => ({
      path,
      roots: [...roots].sort((a, b) => pinRank(a) - pinRank(b)),
      total: roots.reduce((sum, root) => sum + 1 + (descendants[root.id] ?? 0), 0),
    }))
    .sort((a, b) => projectLabel(a.path).localeCompare(projectLabel(b.path)));
}

export function AgentSidebarTree({
  sessions: allSessions,
  activeSessionId,
  pendingCounts = {},
  turnCounts = {},
  subagents = {},
  onSelectSession,
  onNewSession,
  pinnedIds = [],
  onTogglePin,
  collapsedKeys = [],
  onToggleCollapsed,
  hideClosed = false,
  onToggleHideClosed,
  onNewSessionIn,
  onNewChildSession,
  onCancelTurn,
  onCloseSession,
  onDeleteSession,
  onCopy,
  className,
}: AgentSidebarTreeProps) {
  const sessions = React.useMemo(
    () => visibleSessions(allSessions, hideClosed, activeSessionId),
    [allSessions, hideClosed, activeSessionId],
  );
  const hiddenCount = allSessions.length - sessions.length;
  // Descendants are counted on everything, hidden or not: the label on Delete
  // says what will go, and a hidden child still goes.
  const descendantCounts = React.useMemo(() => countDescendants(allSessions), [allSessions]);
  const collapsed = React.useMemo(() => new Set(collapsedKeys), [collapsedKeys]);
  // Delete opens a confirmation from the context menu, so it waits for the
  // menu to close; see heldChoice.
  const [heldDelete] = React.useState(heldChoice);

  const nodes = React.useMemo<TreeNodeData[]>(() => {
    const childrenOf = new Map<string, AgentSession[]>();
    for (const session of sessions) {
      if (!session.parentSessionId) continue;
      const list = childrenOf.get(session.parentSessionId) ?? [];
      list.push(session);
      childrenOf.set(session.parentSessionId, list);
    }

    const menu = (session: AgentSession) => (row: React.ReactNode) => (
      <ContextMenu>
        <ContextMenuTrigger asChild>{row}</ContextMenuTrigger>
        <ContextMenuContent className="w-56" onCloseAutoFocus={() => heldDelete.release((apply) => apply())}>
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

          {onCancelTurn || onCloseSession || onDeleteSession ? <ContextMenuSeparator /> : null}

          {/* Cancel stops the turn and leaves the agent running; close ends the
              process. Both are meaningless once the session is closed, so they
              say so by being disabled rather than by disappearing — a menu whose
              items move between visits is a menu you have to read every time. */}
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
              onSelect={() => heldDelete.hold(() => onDeleteSession(session, descendantCounts[session.id] ?? 0))}
            >
              {/* Says what will actually go. Deleting a parent takes its
                  subtree, and finding that out afterwards is the one thing
                  this action cannot undo. */}
              {descendantCounts[session.id]
                ? `Delete session and ${descendantCounts[session.id]} ${
                    descendantCounts[session.id] === 1 ? "child" : "children"
                  }`
                : "Delete session"}
            </ContextMenuItem>
          ) : null}
        </ContextMenuContent>
      </ContextMenu>
    );

    const subagentNode = (session: AgentSession, agent: Subagent): TreeNodeData => ({
      id: `subagent:${agent.id}`,
      label: agent.name || agent.id.slice(0, 8),
      title: agent.task || undefined,
      icon: <Bot className="size-3 shrink-0 text-icon-secondary" />,
      // No session of DevKit's to open, so the row hands focus to the session
      // that owns it — where the subagent's own output is, in the transcript.
      onSelect: () => onSelectSession(session.id),
      trailing: <AgentState status={toSubagentStatus(agent.state)} labelHidden />,
    });

    const sessionNode = (session: AgentSession): TreeNodeData => {
      const key = `session:${session.id}`;
      const status = toAgentStatus(session.status);
      // Waiting is the one status with a deadline behind it, so it is the one
      // the row says twice: the dot beats, and the name takes the warning
      // colour. Running shimmers instead — the same sweep the transcript uses
      // for a turn in flight, so "this one is moving" reads the same in both
      // places without a second vocabulary.
      const waitingHere = status === "awaiting" || (pendingCounts[session.id] ?? 0) > 0;
      const children = [
        ...(childrenOf.get(session.id) ?? []).map(sessionNode),
        ...(subagents[session.id] ?? []).map((agent) => subagentNode(session, agent)),
      ];
      return {
        id: key,
        label: (
          <Shimmer
            active={status === "running"}
            className={cn("truncate", waitingHere && "text-status-warning")}
          >
            {session.title || `Session ${session.id.slice(0, 8)}`}
          </Shimmer>
        ),
        title: session.title || undefined,
        icon: (
          <VendorIcon
            vendor={session.agentType}
            working={session.status === "running"}
            className="size-3 text-icon-secondary"
          />
        ),
        count: turnCounts[session.id],
        trailing: (
          <span className="flex shrink-0 items-center gap-1">
            {pinnedIds.includes(session.id) ? (
              <Pin aria-label="Pinned" data-testid="agent-row-pin" className="size-2.5 text-text-tertiary" />
            ) : null}
            <AgentState
              status={waitingHere ? "awaiting" : status}
              labelHidden
              pulse={waitingHere}
            />
          </span>
        ),
        selected: session.id === activeSessionId,
        onSelect: () => onSelectSession(session.id),
        expandable: children.length > 0,
        expanded: !collapsed.has(key),
        onToggle: children.length ? () => onToggleCollapsed?.(key) : undefined,
        children,
        wrap: menu(session),
      };
    };

    return groupByProject(sessions, pinnedIds).map((group) => {
      const key = `project:${group.path}`;
      return {
        id: key,
        label: projectLabel(group.path) || "No project",
        title: group.path || undefined,
        icon: <Folder className="size-3 shrink-0 text-icon-secondary" />,
        count: group.total,
        // On hover, in the count's place: starting another agent in a project
        // you are looking at should not mean picking that folder again.
        actions: onNewSessionIn ? (
          <ActionTooltip label={`New session in ${projectLabel(group.path) || "this folder"}`}>
            <Button
              type="button"
              variant="ghost"
              size="icon-xs"
              data-testid="agent-tree-project-new"
              onClick={() => onNewSessionIn(group.path)}
            >
              <Plus aria-hidden="true" />
            </Button>
          </ActionTooltip>
        ) : undefined,
        wrap: (row: React.ReactNode) => (
          <ContextMenu>
            <ContextMenuTrigger asChild>{row}</ContextMenuTrigger>
            <ContextMenuContent className="w-56">
              <ContextMenuLabel className="truncate">
                {projectLabel(group.path) || "No project"}
              </ContextMenuLabel>
              <ContextMenuSeparator />
              {onNewSessionIn ? (
                <ContextMenuItem icon={<Plus />} onSelect={() => onNewSessionIn(group.path)}>
                  New session
                </ContextMenuItem>
              ) : null}
              {onCopy ? (
                <ContextMenuItem icon={<Copy />} onSelect={() => onCopy(group.path)}>
                  Copy path
                </ContextMenuItem>
              ) : null}
              <ContextMenuSeparator />
              <ContextMenuItem onSelect={() => onToggleCollapsed?.(key)}>
                {collapsed.has(key) ? "Expand" : "Collapse"}
              </ContextMenuItem>
            </ContextMenuContent>
          </ContextMenu>
        ),
        // Pinned to the top of the scroller, so scrolling through a long swarm
        // never leaves you looking at rows without knowing whose repo they are.
        sticky: 0,
        expandable: true,
        expanded: !collapsed.has(key),
        onToggle: () => onToggleCollapsed?.(key),
        children: group.roots.map(sessionNode),
      };
    });
  }, [
    sessions,
    subagents,
    turnCounts,
    pendingCounts,
    pinnedIds,
    activeSessionId,
    collapsed,
    descendantCounts,
    onSelectSession,
    onToggleCollapsed,
    onTogglePin,
    onNewSessionIn,
    onNewChildSession,
    onCancelTurn,
    onCloseSession,
    onDeleteSession,
    onCopy,
  ]);

  // Over every session, not the drawn ones: a hidden agent can still be
  // waiting, and the footer is how you would find out.
  const waitingIds = React.useMemo(
    () =>
      allSessions
        .filter((s) => s.status === "awaiting_permission" || (pendingCounts[s.id] ?? 0) > 0)
        .map((s) => s.id),
    [allSessions, pendingCounts],
  );
  const waiting = waitingIds.length;

  return (
    <div
      data-testid="agent-sidebar-tree"
      // No width of its own: the shell rail is resizable and the rail is what
      // decides. Figma's frame is 264 wide and that number was here as a
      // max-w-66, which stopped the tree growing with the rail — drag the
      // sidebar wider and the rows stayed put with a band of empty rail beside
      // them. The database and Kafka rails never took the cap.
      className={cn("flex h-full w-full flex-col", className)}
    >
      <div className="flex w-full shrink-0 items-center gap-2 py-3">
        <span className="min-w-0 flex-1 text-label-12 text-text-secondary">Swarm</span>
        {onToggleHideClosed ? (
          <ActionTooltip
            label={
              hideClosed
                ? `Show closed sessions${hiddenCount ? ` (${hiddenCount} hidden)` : ""}`
                : "Hide closed sessions"
            }
          >
            <Button
              type="button"
              variant="ghost"
              size="icon-xs"
              data-testid="agent-tree-toggle-closed"
              aria-pressed={hideClosed}
              onClick={onToggleHideClosed}
              className="text-text-tertiary"
            >
              {hideClosed ? <EyeOff aria-hidden="true" /> : <Eye aria-hidden="true" />}
            </Button>
          </ActionTooltip>
        ) : null}
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

      <TreeView nodes={nodes} className="flex-1 scrollbar-gutter-stable" />

      {waiting > 0 ? (
        // A button, not a caption: the count says something needs you, and
        // this is the shortest way to it. Each press walks to the next one.
        <button
          type="button"
          data-testid="agent-tree-attention"
          onClick={() => {
            const next = nextWaiting(waitingIds, activeSessionId);
            if (next) onSelectSession(next);
          }}
          className="flex w-full shrink-0 cursor-pointer items-center gap-2 bg-(--state-soft) px-2 py-3 text-left outline-none hover:bg-surface-raised focus-visible:ring-1 focus-visible:ring-status-info"
        >
          <span
            aria-hidden="true"
            className="size-1.5 shrink-0 animate-pulse rounded-full bg-status-warning motion-reduce:animate-none"
          />
          <span className="min-w-0 flex-1 text-caption-11-medium text-text-primary">
            {`${waiting} agent${waiting === 1 ? "" : "s"} waiting on you`}
          </span>
          <span className="shrink-0 text-caption-11 text-text-tertiary">
            {waiting === 1 ? "Go" : "Next"}
          </span>
        </button>
      ) : null}
    </div>
  );
}
