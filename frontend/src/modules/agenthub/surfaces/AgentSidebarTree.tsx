import * as React from "react";
import { Ban, Bot, Copy, Eye, EyeOff, FileText, Folder, FolderOpen, FolderX, Layers, Pencil, Pin, PinOff, Plus, Square, Trash2 } from "lucide-react";

import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { ActionTooltip } from "@/components/ui/action-tooltip";
import { SidebarInset, SidebarSection } from "@/components/ui/sidebar-shell";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuLabel,
  ContextMenuSeparator,
  ContextMenuTrigger,
} from "@/components/ui/context-menu";
import { TreeRowAction, TreeView, type TreeNodeData } from "@/components/tree-view";
import { Shimmer } from "@/components/ui/shimmer";
import { AgentState, type AgentStatus } from "./AgentState";
import { heldChoice } from "./AgentComposer";
import { VendorIcon } from "./VendorIcon";
import type { AgentSession } from "@/bridge/agenthub";
import type { Subagent } from "../types";

/**
 * A workspace as the rail groups by it: its services' folders, primary first.
 * A session opened in any of them belongs to the workspace, not to the one
 * folder its process happens to start in.
 */
export type SidebarWorkspace = { id: string; name: string; paths: string[] };

export type AgentSidebarTreeProps = {
  sessions: AgentSession[];
  /**
   * Workspaces to group by, the current one first — a folder in two
   * workspaces goes to the one the person is in. Sessions outside all of
   * them are grouped by their folder, as before workspaces existed.
   */
  workspaces?: SidebarWorkspace[];
  /**
   * Where "no project" sessions get their folders. Every session under it is
   * one "No project" group, not a project per timestamped scratch folder.
   */
  scratchRoot?: string;
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
  /**
   * A subagent's row: show what it did. Without it, the row selects the
   * session it belongs to, as it used to.
   */
  onSelectSubagent?: (session: AgentSession, subagent: Subagent) => void;
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
   * its context menu. The folder is already known, so nothing is asked. On a
   * workspace row it also gets the workspace's folders, primary first, so the
   * session is the whole workspace's.
   */
  onNewSessionIn?: (projectPath: string, workspaceId?: string, workspacePaths?: string[]) => void;
  /** Spawns a child of this session — the context menu's only creating action. */
  onNewChildSession?: (parent: AgentSession) => void;
  onCancelTurn?: (session: AgentSession) => void;
  onCloseSession?: (session: AgentSession) => void;
  /** Opens the rename dialog for this row's session. */
  onRenameSession?: (session: AgentSession) => void;
  /** Copies this row's whole conversation as Markdown. */
  onCopyTranscript?: (session: AgentSession) => void;
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
 * Its rows are one line, which is what moved the project out to its own level —
 * a second line per session repeated the folder for every one of them. Row
 * height, icons and type are the tree's own, the same as every other rail.
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

/**
 * The last segment of a path — the folder a developer calls the project — or
 * the last `segments` of them, to tell two folders of one name apart.
 */
function projectLabel(path: string, segments = 1): string {
  const parts = path.split("/").filter(Boolean);
  return parts.length ? parts.slice(-segments).join("/") : path;
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



/**
 * One project and the root sessions opened in it, pinned ones first. A
 * workspace group's path is its primary folder; `label` is what the row says.
 */
export type ProjectGroup = {
  path: string;
  label: string;
  roots: AgentSession[];
  total: number;
  workspace?: SidebarWorkspace;
};

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
  scratchRoot = "",
  workspaces: SidebarWorkspace[] = [],
): ProjectGroup[] {
  const workspaceByKey = new Map<string, SidebarWorkspace>();
  // Every scratch folder is the same "no project". A session belongs only
  // to its chosen workspace, even when its folder is shared by several.
  const groupOf = (session: AgentSession) => {
    const path = session.projectPath;
    if (scratchRoot && path.startsWith(`${scratchRoot}/`)) return scratchRoot;
    const workspace = workspaces.find((w) => w.id === session.workspaceId);
    if (!workspace) return path;
    const key = `workspace:${workspace.id}`;
    workspaceByKey.set(key, workspace);
    return key;
  };
  const byId = new Set(sessions.map((session) => session.id));
  const groups = new Map<string, AgentSession[]>();
  for (const session of sessions) {
    if (session.parentSessionId && byId.has(session.parentSessionId)) continue;
    const key = groupOf(session);
    const list = groups.get(key) ?? [];
    list.push(session);
    groups.set(key, list);
  }

  const pinRank = (session: AgentSession) => {
    const at = pinnedIds.indexOf(session.id);
    return at === -1 ? Number.MAX_SAFE_INTEGER : at;
  };
  const descendants = countDescendants(sessions);

  // Two folders with the same name (an old clone and a new one) would be two
  // rows that read the same; those say which parent they are in.
  const folderNames = new Map<string, number>();
  for (const key of groups.keys()) {
    if (workspaceByKey.has(key) || key === scratchRoot) continue;
    const name = projectLabel(key);
    folderNames.set(name, (folderNames.get(name) ?? 0) + 1);
  }
  const labelOf = (key: string) => {
    const workspace = workspaceByKey.get(key);
    if (workspace) return workspace.name;
    const name = projectLabel(key);
    return (folderNames.get(name) ?? 0) > 1 ? projectLabel(key, 2) : name;
  };

  return [...groups.entries()]
    .map(([key, roots]) => {
      const workspace = workspaceByKey.get(key);
      return {
        path: workspace ? (workspace.paths[0] ?? "") : key,
        label: labelOf(key),
        roots: [...roots].sort((a, b) => pinRank(a) - pinRank(b)),
        total: roots.reduce((sum, root) => sum + 1 + (descendants[root.id] ?? 0), 0),
        ...(workspace ? { workspace } : {}),
      };
    })
    // No project last: the named ones are what the rail is organised by.
    .sort((a, b) =>
      Number(a.path === scratchRoot && !!scratchRoot && !a.workspace) -
        Number(b.path === scratchRoot && !!scratchRoot && !b.workspace) || a.label.localeCompare(b.label),
    );
}

// A stable default: a fresh [] per render would rebuild the tree every time.
const NO_WORKSPACES: SidebarWorkspace[] = [];

export function AgentSidebarTree({
  sessions: allSessions,
  workspaces = NO_WORKSPACES,
  scratchRoot = "",
  activeSessionId,
  pendingCounts = {},
  turnCounts = {},
  subagents = {},
  onSelectSession,
  onSelectSubagent,
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
  onRenameSession,
  onCopyTranscript,
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
  // Delete and rename open a dialog from the context menu, so they wait for
  // the menu to close; see heldChoice.
  const [heldDialog] = React.useState(heldChoice);

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
        <ContextMenuContent className="w-56" onCloseAutoFocus={() => heldDialog.release((apply) => apply())}>
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

          {onRenameSession ? (
            <ContextMenuItem icon={<Pencil />} onSelect={() => heldDialog.hold(() => onRenameSession(session))}>
              Rename…
            </ContextMenuItem>
          ) : null}

          {onCopy || onCopyTranscript ? <ContextMenuSeparator /> : null}

          {onCopyTranscript ? (
            <ContextMenuItem icon={<FileText />} onSelect={() => onCopyTranscript(session)}>
              Copy transcript as Markdown
            </ContextMenuItem>
          ) : null}

          {onCopy ? (
            <>
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
              onSelect={() => heldDialog.hold(() => onDeleteSession(session, descendantCounts[session.id] ?? 0))}
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
      icon: <Bot />,
      // No session of DevKit's to open: the row opens the session that owns
      // it, with the subagent's own work beside it (onSelectSubagent).
      onSelect: () => (onSelectSubagent ? onSelectSubagent(session, agent) : onSelectSession(session.id)),
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
          <VendorIcon vendor={session.agentType} working={session.status === "running"} />
        ),
        count: turnCounts[session.id],
        trailing: (
          <span className="flex shrink-0 items-center gap-1">
            {pinnedIds.includes(session.id) ? (
              <Pin aria-label="Pinned" data-testid="agent-row-pin" />
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
        // Double-click renames, as in a file tree.
        onActivate: onRenameSession ? () => onRenameSession(session) : undefined,
        expandable: children.length > 0,
        expanded: !collapsed.has(key),
        onToggle: children.length ? () => onToggleCollapsed?.(key) : undefined,
        children,
        wrap: menu(session),
      };
    };

    return groupByProject(sessions, pinnedIds, scratchRoot, workspaces).map((group) => {
      const workspace = group.workspace;
      const key = workspace ? `workspace:${workspace.id}` : `project:${group.path}`;
      const scratch = !workspace && Boolean(scratchRoot) && group.path === scratchRoot;
      const name = scratch ? "No project" : group.label || "No project";
      const newIn = () => onNewSessionIn?.(group.path, workspace?.id, workspace?.paths);
      return {
        id: key,
        label: name,
        title: scratch ? undefined : workspace ? workspace.paths.join("\n") : group.path || undefined,
        icon: scratch ? (
          <FolderX />
        ) : workspace ? (
          <Layers />
        ) : (
          <Folder />
        ),
        count: group.total,
        // On hover, in the count's place: starting another agent in a project
        // you are looking at should not mean picking that folder again.
        actions: onNewSessionIn ? (
          <ActionTooltip label={scratch ? "New session without a project" : `New session in ${name}`}>
            <TreeRowAction data-testid="agent-tree-project-new" onClick={newIn}>
              <Plus aria-hidden="true" />
            </TreeRowAction>
          </ActionTooltip>
        ) : undefined,
        wrap: (row: React.ReactNode) => (
          <ContextMenu>
            <ContextMenuTrigger asChild>{row}</ContextMenuTrigger>
            <ContextMenuContent className="w-56">
              <ContextMenuLabel className="truncate">{name}</ContextMenuLabel>
              <ContextMenuSeparator />
              {onNewSessionIn ? (
                <ContextMenuItem icon={<Plus />} onSelect={newIn}>
                  New session
                </ContextMenuItem>
              ) : null}
              {onCopy && !scratch ? (
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
    workspaces,
    scratchRoot,
    subagents,
    turnCounts,
    pendingCounts,
    pinnedIds,
    activeSessionId,
    collapsed,
    descendantCounts,
    onSelectSession,
    onSelectSubagent,
    onToggleCollapsed,
    onTogglePin,
    onNewSessionIn,
    onNewChildSession,
    onCancelTurn,
    onCloseSession,
    onRenameSession,
    onCopyTranscript,
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
      {/* The shared rail header — a section label and its actions — the same
          shape as the Notes rail and Database's consoles, rather than a row of
          its own with a bordered "+ New" pill. */}
      <SidebarInset className="shrink-0">
        <SidebarSection
          first
          label="Swarm"
          actions={
            <>
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
              <ActionTooltip label="New session">
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-xs"
                  data-testid="agent-tree-new"
                  aria-label="New session"
                  onClick={onNewSession}
                >
                  <Plus aria-hidden="true" />
                </Button>
              </ActionTooltip>
            </>
          }
        />
      </SidebarInset>

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
