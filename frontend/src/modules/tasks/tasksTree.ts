import { createDebouncedWriter } from "@/lib/debouncedWriter";
import { progressOf, type Board, type Progress, type Status, type Task } from "@/bridge/tasks";
import type { Workspace } from "@/bridge/workspace";
import type { TreeNodeData } from "@/components/tree-view";

/**
 * The pure half of the Tasks sidebar: given the store's data and expanded
 * state, build the rows `TreeView` draws. No React, no bridge calls — the
 * store (tasksStore.ts) owns fetching and persistence, this file only owns
 * shape, ordering and search, which is what lets it be tested without a DOM
 * (see tasksTree.test.ts, the same split databaseTree.ts uses).
 *
 * Shape:
 *   Workspace (count = bound boards)         current workspace first, open by default
 *   └─ Board (count = open tasks)            children absent + loading while tasks aren't fetched yet
 *      └─ Status (workflow order, count)     terminal categories collapsed by default
 *         └─ Task  "KEY title"
 *            └─ Subtask                      one level; never listed anywhere else
 *   "Not linked to a workspace"              only when some board has nowhere to go
 *   └─ Board …
 */

/** A task or subtask matches the needle on its key, title or any label. */
export function matchesTask(
  task: Pick<Task, "key" | "title" | "labels">,
  needle: string,
): boolean {
  if (!needle) return true;
  const n = needle.toLowerCase();
  return (
    task.key.toLowerCase().includes(n) ||
    task.title.toLowerCase().includes(n) ||
    (task.labels ?? []).some((label) => label.toLowerCase().includes(n))
  );
}

export function matchesBoardName(board: Pick<Board, "name">, needle: string): boolean {
  if (!needle) return true;
  return board.name.toLowerCase().includes(needle.toLowerCase());
}

export function matchesWorkspaceName(workspace: Pick<Workspace, "name">, needle: string): boolean {
  if (!needle) return true;
  return workspace.name.toLowerCase().includes(needle.toLowerCase());
}

/** The node id a board's "not bound anywhere" group uses, in the tree and in
 * expandedWorkspaces (it toggles exactly like a workspace does). */
export const UNLINKED_GROUP_ID = "unlinked";

/** Every kind of row a `decorate` callback can be asked to finish. Left to the
 * caller (Task 10) so this file never imports an icon or a context menu:
 * icons, trailing chips (priority, "held by an agent") and `wrap` (context
 * menus) are all UI concerns, not tree-shape concerns. `progress` is passed
 * for a parent task rather than left for the decorator to compute, because
 * `progressOf` needs the board's workflow and the already-filtered subtask
 * list this file already has in hand — recomputing it in the sidebar would be
 * a second, possibly-drifting copy of the same rule. It is null for a task
 * with no subtasks and always null for a subtask itself (subtasks are not
 * shown nested more than one level, so they have nothing to summarize).
 */
export type TasksTreeDecorators = {
  workspace?: (workspace: Workspace, node: TreeNodeData) => TreeNodeData;
  unlinkedGroup?: (node: TreeNodeData) => TreeNodeData;
  board?: (board: Board, node: TreeNodeData) => TreeNodeData;
  status?: (board: Board, status: Status, node: TreeNodeData) => TreeNodeData;
  task?: (task: Task, node: TreeNodeData, progress: Progress | null) => TreeNodeData;
};

/** Every gesture a row needs. Each toggle receives the row's OWN computed
 * `expanded` value (not just the id): the default differs per row — the
 * current workspace opens by default and others don't, a terminal status
 * starts closed and others don't — so only this file, which already worked
 * out that default, can hand back the value to flip. The store's toggles are
 * then pure `!expanded` writes with no knowledge of anyone's default. */
export type TasksTreeCallbacks = {
  toggleWorkspace: (id: string, expanded: boolean) => void;
  toggleBoard: (boardId: string, expanded: boolean) => void;
  toggleStatus: (boardId: string, statusId: string, expanded: boolean) => void;
  toggleTask: (taskId: string, expanded: boolean) => void;
  selectBoard?: (board: Board) => void;
  activateBoard?: (board: Board) => void;
  selectTask?: (task: Task) => void;
  activateTask?: (task: Task) => void;
};

export type TasksTreeInput = {
  boards: Board[] | null;
  workspaces: Workspace[] | null;
  /** The workspace the switcher is showing right now, or null. Sorted first
   * and, absent a stored preference, the only one open by default. */
  currentWorkspaceId: string | null;
  /** A board's tasks, present only once fetched — see tasksStore's lazy load. */
  tasksByBoard: Record<string, Task[] | undefined>;
  /** A board mid-fetch (including a re-fetch of one already loaded). */
  loadingBoards: Record<string, boolean>;
  expandedWorkspaces: Record<string, boolean>;
  expandedBoards: Record<string, boolean>;
  /** Keyed `${boardId}:${statusId}` — a status id is only unique within its board. */
  expandedStatuses: Record<string, boolean>;
  expandedTasks: Record<string, boolean>;
  /** Trimmed by the caller or not; either way only its trimmed form is used. */
  needle?: string;
  selectedBoardId?: string;
  selectedTaskId?: string;
  callbacks: TasksTreeCallbacks;
  decorate?: TasksTreeDecorators;
};

function categoryOf(board: Board, statusId: string): string | undefined {
  return board.workflow.statuses.find((status) => status.id === statusId)?.category;
}

export function buildTasksTree(input: TasksTreeInput): TreeNodeData[] {
  const {
    boards: rawBoards,
    workspaces: rawWorkspaces,
    currentWorkspaceId,
    tasksByBoard,
    loadingBoards,
    expandedWorkspaces,
    expandedBoards,
    expandedStatuses,
    expandedTasks,
    selectedBoardId,
    selectedTaskId,
    callbacks,
    decorate,
  } = input;

  const boards = rawBoards ?? [];
  const workspaces = rawWorkspaces ?? [];
  const needle = (input.needle ?? "").trim();
  const filtering = needle.length > 0;

  const workspaceIds = new Set(workspaces.map((w) => w.id));
  const byWorkspace = new Map<string, Board[]>();
  const unlinkedBoards: Board[] = [];
  for (const board of boards) {
    if (board.workspaceId && workspaceIds.has(board.workspaceId)) {
      const list = byWorkspace.get(board.workspaceId);
      if (list) list.push(board);
      else byWorkspace.set(board.workspaceId, [board]);
    } else {
      unlinkedBoards.push(board);
    }
  }

  /** One task row, and — one level down, never further — its own subtasks. */
  function taskNode(task: Task, board: Board, subtasks: Task[]): TreeNodeData {
    const childNodes = subtasks.map((subtask) => taskNode(subtask, board, []));
    const expanded = filtering ? childNodes.length > 0 : !!expandedTasks[task.id];
    let node: TreeNodeData = {
      id: `task:${task.id}`,
      label: `${task.key} ${task.title}`.trim(),
      expandable: subtasks.length > 0,
      expanded,
      onToggle: () => callbacks.toggleTask(task.id, expanded),
      children: subtasks.length > 0 ? childNodes : undefined,
      emptyLabel: "No subtasks",
      selected: !!selectedTaskId && task.id === selectedTaskId,
      onSelect: callbacks.selectTask ? () => callbacks.selectTask?.(task) : undefined,
      onActivate: callbacks.activateTask ? () => callbacks.activateTask?.(task) : undefined,
    };
    const progress = subtasks.length > 0 ? progressOf(board, subtasks) : null;
    if (decorate?.task) node = decorate.task(task, node, progress);
    return node;
  }

  function boardNode(board: Board): TreeNodeData | null {
    const tasks = tasksByBoard[board.id];
    const loaded = tasks !== undefined;
    const nameMatches = matchesBoardName(board, needle);
    // An unfetched board has nothing to search inside — a hit can only be its
    // own name, same rule the sidebar spells out for a search over a rail
    // that has not read everything yet.
    if (filtering && !nameMatches && !loaded) return null;

    const liveTasks = loaded ? tasks.filter((task) => !task.archivedAt) : [];
    const topLevel = liveTasks.filter((task) => !task.parentId);
    const openCount = topLevel.filter((task) => categoryOf(board, task.status) !== "terminal").length;

    const statusNodes: TreeNodeData[] = [];
    if (loaded) {
      for (const status of board.workflow.statuses) {
        const tasksInStatus = topLevel.filter((task) => task.status === status.id);
        const taskNodes: TreeNodeData[] = [];
        for (const task of tasksInStatus) {
          const allSubtasks = liveTasks.filter((candidate) => candidate.parentId === task.id);
          if (!filtering) {
            taskNodes.push(taskNode(task, board, allSubtasks));
            continue;
          }
          const selfMatches = matchesTask(task, needle);
          const matchingSubtasks = allSubtasks.filter((subtask) => matchesTask(subtask, needle));
          // Dropped: neither this task nor anything under it is why we're here.
          if (!selfMatches && matchingSubtasks.length === 0) continue;
          taskNodes.push(taskNode(task, board, matchingSubtasks));
        }
        // An empty column reads as "nothing here" under a search — same call
        // databaseTree.ts's filterNamespace makes for a schema with no
        // matching tables.
        if (filtering && taskNodes.length === 0) continue;
        const statusKey = `${board.id}:${status.id}`;
        const expanded = filtering
          ? true
          : (expandedStatuses[statusKey] ?? status.category !== "terminal");
        let statusNode: TreeNodeData = {
          id: `status:${statusKey}`,
          label: status.name,
          expandable: true,
          expanded,
          onToggle: () => callbacks.toggleStatus(board.id, status.id, expanded),
          children: taskNodes,
          emptyLabel: filtering ? "No tasks match" : "No tasks",
          count: tasksInStatus.length,
        };
        if (decorate?.status) statusNode = decorate.status(board, status, statusNode);
        statusNodes.push(statusNode);
      }
    }

    const keepBoard = !filtering || nameMatches || statusNodes.length > 0;
    if (!keepBoard) return null;

    const expanded = filtering ? true : !!expandedBoards[board.id];
    let node: TreeNodeData = {
      id: `board:${board.id}`,
      label: board.name,
      // Expandable even before the fetch lands — opening it is what starts
      // the fetch (tasksStore.toggleBoard), and a leaf with no chevron would
      // have no way to ask for it.
      expandable: loaded ? statusNodes.length > 0 : true,
      expanded,
      onToggle: () => callbacks.toggleBoard(board.id, expanded),
      children: loaded ? statusNodes : undefined,
      loading: !loaded || !!loadingBoards[board.id],
      emptyLabel: filtering ? "No tasks match" : "No tasks",
      count: loaded ? openCount : undefined,
      selected: !!selectedBoardId && board.id === selectedBoardId,
      onSelect: callbacks.selectBoard ? () => callbacks.selectBoard?.(board) : undefined,
      onActivate: callbacks.activateBoard ? () => callbacks.activateBoard?.(board) : undefined,
    };
    if (decorate?.board) node = decorate.board(board, node);
    return node;
  }

  function groupNode(
    id: string,
    label: string,
    isCurrent: boolean,
    nameMatches: boolean,
    boardsHere: Board[],
    decorateGroup: ((node: TreeNodeData) => TreeNodeData) | undefined,
  ): TreeNodeData | null {
    const kept = boardsHere.map(boardNode).filter((node): node is TreeNodeData => node !== null);
    // Every workspace shows, even an empty one — it is a container the user
    // made (like a database folder) and "New board" in it is how it stops
    // being empty. A search is the exception: nothing pads out a result list
    // that found nothing in it and no reason to look at it either.
    if (filtering && !nameMatches && kept.length === 0) return null;
    const expanded = filtering ? true : (expandedWorkspaces[id] ?? isCurrent);
    const node: TreeNodeData = {
      id,
      label,
      expandable: kept.length > 0,
      expanded,
      onToggle: () => callbacks.toggleWorkspace(id, expanded),
      children: kept.length > 0 ? kept : undefined,
      emptyLabel: filtering ? "No boards match" : "No boards yet",
      count: boardsHere.length,
    };
    return decorateGroup ? decorateGroup(node) : node;
  }

  // Current workspace first; Array#sort is stable, so everyone else keeps the
  // order the bridge gave them.
  const orderedWorkspaces = [...workspaces].sort((a, b) => {
    if (a.id === currentWorkspaceId) return -1;
    if (b.id === currentWorkspaceId) return 1;
    return 0;
  });

  const workspaceNodes = orderedWorkspaces
    .map((workspace) =>
      groupNode(
        `ws:${workspace.id}`,
        workspace.name,
        workspace.id === currentWorkspaceId,
        matchesWorkspaceName(workspace, needle),
        byWorkspace.get(workspace.id) ?? [],
        decorate?.workspace ? (node) => decorate.workspace!(workspace, node) : undefined,
      ),
    )
    .filter((node): node is TreeNodeData => node !== null);

  const unlinkedNode =
    unlinkedBoards.length > 0
      ? groupNode(
          UNLINKED_GROUP_ID,
          "Not linked to a workspace",
          false,
          // The group has no name of its own to match against the needle —
          // it only survives a search through boards kept below it.
          false,
          unlinkedBoards,
          decorate?.unlinkedGroup,
        )
      : null;

  return unlinkedNode ? [...workspaceNodes, unlinkedNode] : workspaceNodes;
}

/**
 * Coalesces a burst of `tasks:changed` events into one call per board, ~150ms
 * after the last one for that board — an agent moving five subtasks in a row
 * would otherwise re-list the board five times for one glance at the result.
 * Pure and DOM-free like the rest of this file (its only side effect is a
 * timer), which is what makes it testable with a real short delay rather than
 * a mocked clock; tasksStore.ts wires `onFire` to `refreshBoard`/`refreshBoards`.
 */
export type BoardChangeDebouncer = { trigger: (boardId: string) => void };

export function createBoardChangeDebouncer(
  onFire: (boardId: string) => void,
  delayMs = 150,
): BoardChangeDebouncer {
  // createDebouncedWriter already does exactly this — one timer per key,
  // last write in the window wins — so triggering it with the board id as
  // both the key and its own payload reuses that instead of a second copy.
  const writer = createDebouncedWriter<string>((_key, boardId) => onFire(boardId), delayMs);
  return {
    trigger: (boardId: string) => writer.schedule(boardId, boardId),
  };
}
