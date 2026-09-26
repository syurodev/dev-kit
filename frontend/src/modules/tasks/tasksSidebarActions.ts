import type { Board, Task } from "@/bridge/tasks";
import type { Workspace } from "@/bridge/workspace";

/**
 * The decisions behind the Tasks rail's menus and buttons — which actions a
 * row gets, which of them are off and why — kept apart from TasksSidebar.tsx
 * so they can be tested without a DOM (tasksSidebarActions.test.ts), the same
 * split Database makes with tabActions.ts. Nothing here imports an icon or
 * calls the bridge: an action is an id and a label, and the sidebar decides
 * what the id draws and does.
 */

export type SidebarActionId =
  | "workspace.newBoard"
  | "board.open"
  | "board.newTask"
  | "board.verify"
  | "board.fromPlan"
  | "board.linkCurrent"
  | "board.linkTo"
  | "board.unlink"
  | "task.open"
  | "task.openBeside"
  | "task.copyKey"
  | "task.archive";

export type SidebarAction =
  | {
      kind: "item";
      id: SidebarActionId;
      label: string;
      disabled?: boolean;
      /** For an action aimed at something other than the row — a workspace to link to. */
      target?: string;
    }
  | { kind: "separator" }
  | { kind: "submenu"; label: string; items: SidebarAction[] };

/** Why "New board" in the header is off, or "" when it is on. */
export function newBoardBlockedReason(
  currentWorkspaceId: string | null,
  workspaces: Workspace[] | null,
): string {
  // A board made from the header lands in the current workspace — with none,
  // it would land in the unlinked group, where no agent can see it (fail
  // closed, tasks.mdx § Ai thấy board nào). Saying so beats a board that
  // quietly goes nowhere useful. A current id the list does not know is a
  // workspace since removed: the bind would be refused, so it counts as none.
  const known =
    !!currentWorkspaceId &&
    (workspaces === null || workspaces.some((workspace) => workspace.id === currentWorkspaceId));
  return known ? "" : "Open a workspace first — a new board goes in the current one";
}

/**
 * The board "New task" in the header aims at: the first candidate that is
 * still a board. Candidates in order of how deliberately they were chosen —
 * the board tab in front, the board of the task tab in front, then the last
 * board or task row clicked in the rail. A candidate that names a board since
 * deleted is skipped rather than trusted.
 */
export function newTaskTarget(boards: Board[] | null, candidates: string[]): Board | null {
  if (!boards) return null;
  for (const id of candidates) {
    if (!id) continue;
    const board = boards.find((candidate) => candidate.id === id);
    if (board) return board;
  }
  return null;
}

/** The board a task id belongs to, among the boards whose tasks are loaded. */
export function boardOfTask(tasksByBoard: Record<string, Task[] | undefined>, taskId: string): string {
  if (!taskId) return "";
  for (const [boardId, tasks] of Object.entries(tasksByBoard)) {
    if (tasks?.some((task) => task.id === taskId)) return boardId;
  }
  return "";
}

export function workspaceActions(): SidebarAction[] {
  return [{ kind: "item", id: "workspace.newBoard", label: "New board" }];
}

/**
 * A board's menu. The link actions follow where the board is: an unlinked
 * board leads with "Link to current workspace" — the one gesture that brings
 * a board synced from another machine back into view — and a linked one
 * offers "Move to workspace" and "Unlink" instead. Linking is tasks:admin and
 * UI-only (a binding decides which agents see the board), which is why it
 * lives here and has no MCP tool.
 */
export function boardActions(
  board: Board,
  context: { currentWorkspaceId: string | null; workspaces: Workspace[] | null },
): SidebarAction[] {
  const workspaces = context.workspaces ?? [];
  const linkedTo = workspaces.find((workspace) => workspace.id === board.workspaceId);
  const linked = !!linkedTo;
  const others = workspaces.map(
    (workspace): SidebarAction => ({
      kind: "item",
      id: "board.linkTo",
      label: workspace.name,
      target: workspace.id,
      disabled: workspace.id === board.workspaceId,
    }),
  );

  const actions: SidebarAction[] = [
    { kind: "item", id: "board.open", label: "Open" },
    { kind: "item", id: "board.newTask", label: "New task" },
    { kind: "separator" },
    { kind: "item", id: "board.verify", label: "Verify settings…" },
  ];
  // The plan belongs to a project, and only a linked board has one: an
  // unlinked board would build its backlog out of whatever plan happens to be
  // open, which is somebody else's.
  if (linked) actions.push({ kind: "item", id: "board.fromPlan", label: "Create tasks from plan" });
  actions.push({ kind: "separator" });

  if (linked) {
    if (workspaces.length > 1) {
      actions.push({ kind: "submenu", label: "Move to workspace", items: others });
    }
    actions.push({ kind: "item", id: "board.unlink", label: "Unlink from workspace" });
  } else {
    actions.push({
      kind: "item",
      id: "board.linkCurrent",
      label: "Link to current workspace",
      disabled: !context.currentWorkspaceId,
    });
    if (workspaces.length > 0) {
      actions.push({ kind: "submenu", label: "Link to workspace", items: others });
    }
  }
  return actions;
}

/**
 * A task's menu. No Delete: a destructive action does not sit next to a
 * mis-click in a list you scroll through — it lives in the task's own view,
 * where you are looking at what you are about to lose. Archive stays, because
 * it is reversible and is how a list is kept short.
 */
export function taskActions(task: Pick<Task, "archivedAt">): SidebarAction[] {
  return [
    { kind: "item", id: "task.open", label: "Open" },
    { kind: "item", id: "task.openBeside", label: "Open beside" },
    { kind: "item", id: "task.copyKey", label: "Copy key" },
    { kind: "separator" },
    { kind: "item", id: "task.archive", label: "Archive", disabled: !!task.archivedAt },
  ];
}

/**
 * Priority is free text (FR-3 names the field, not its values), so only the
 * words that plainly mean "drop everything" get the danger tag — the rest stay
 * quiet so a list of them does not shout.
 */
const URGENT = new Set(["urgent", "critical", "blocker", "highest", "p0"]);

export function isUrgentPriority(priority: string): boolean {
  return URGENT.has(priority.trim().toLowerCase());
}

/**
 * A key prefix suggested from a board's name: the initials of a multi-word
 * name ("Dev Kit App" → "DKA"), or the first letters of a single word
 * ("Payments" → "PAY"). Only a suggestion — the dialog shows it in a field
 * that can be typed over — and the service upper-cases whatever it is given.
 */
export function suggestKeyPrefix(name: string): string {
  const words = name
    .trim()
    .split(/[^A-Za-z0-9]+/)
    .filter(Boolean);
  if (words.length === 0) return "";
  const prefix = words.length > 1 ? words.map((word) => word[0]).join("") : words[0].slice(0, 3);
  return prefix.slice(0, 5).toUpperCase();
}

/**
 * What the rail's body shows. `hidden` while the first read is in flight — an
 * empty state that flashes and is replaced is a claim the rail then takes
 * back; `empty` when there is nothing to put in a tree at all; `noMatch` when
 * a search emptied one that has something in it.
 */
export function sidebarBody(input: {
  boards: Board[] | null;
  workspaces: Workspace[] | null;
  treeLength: number;
  needle: string;
}): "hidden" | "empty" | "noMatch" | "tree" {
  if (input.boards === null) return "hidden";
  if (input.boards.length === 0 && (input.workspaces ?? []).length === 0) return "empty";
  if (input.treeLength === 0) return input.needle ? "noMatch" : "empty";
  return "tree";
}
