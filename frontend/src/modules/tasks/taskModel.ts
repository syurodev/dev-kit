import type { Board, Status, Task } from "@/bridge/tasks";
import { matchesTask } from "./tasksTree";

/**
 * The pure half of the board and the task detail: which card goes in which
 * column, what a filter keeps, which tasks a picker offers. No React and no
 * bridge, so every rule here is tested without a DOM (taskModel.test.ts) —
 * the same split tasksTree.ts makes for the rail.
 */

/**
 * The priorities the UI offers. Priority is a free string on the record (an
 * agent may have written anything), so an unknown one is shown as written and
 * sorts after these — it is not dropped, and it is not rewritten.
 */
export const PRIORITIES = [
  { id: "urgent", label: "Urgent" },
  { id: "high", label: "High" },
  { id: "medium", label: "Medium" },
  { id: "low", label: "Low" },
] as const;

export type PriorityId = (typeof PRIORITIES)[number]["id"];

export function priorityLabel(priority: string): string {
  if (!priority) return "No priority";
  return PRIORITIES.find((p) => p.id === priority)?.label ?? priority;
}

/** Urgent first, "" last, an unknown value between the known ones and none. */
export function priorityRank(priority: string): number {
  if (!priority) return PRIORITIES.length + 1;
  const index = PRIORITIES.findIndex((p) => p.id === priority);
  return index === -1 ? PRIORITIES.length : index;
}

export function statusOf(board: Board, statusId: string): Status | undefined {
  return board.workflow.statuses.find((status) => status.id === statusId);
}

/** A status's name, or its id when the board's workflow no longer has it. */
export function statusLabel(board: Board, statusId: string): string {
  return statusOf(board, statusId)?.name ?? statusId;
}

export function isTerminal(board: Board, statusId: string): boolean {
  return statusOf(board, statusId)?.category === "terminal";
}

/**
 * The blockers still standing: listed, on this board, and not in a terminal
 * column. A blocker that is gone (deleted, archived, on another board) is not
 * counted — it cannot be waited on, and a card that says "blocked" by
 * something nobody can find is a card nobody can unblock.
 */
export function openBlockers(task: Task, byId: ReadonlyMap<string, Task>, board: Board): Task[] {
  const out: Task[] = [];
  for (const id of task.blockedBy ?? []) {
    const blocker = byId.get(id);
    if (blocker && !isTerminal(board, blocker.status)) out.push(blocker);
  }
  return out;
}

export function indexById(tasks: readonly Task[]): Map<string, Task> {
  return new Map(tasks.map((task) => [task.id, task]));
}

export function childrenOf(tasks: readonly Task[], parentId: string): Task[] {
  return tasks.filter((task) => task.parentId === parentId);
}

export type TaskFilter = {
  query: string;
  /** A priority id, "none" for tasks with no priority, "" for any. */
  priority: string;
  /** One label, or "" for any. */
  label: string;
  /** Only tasks an agent session holds. */
  heldByAgent: boolean;
};

export const EMPTY_FILTER: TaskFilter = { query: "", priority: "", label: "", heldByAgent: false };

export function isFiltering(filter: TaskFilter): boolean {
  return !!(filter.query.trim() || filter.priority || filter.label || filter.heldByAgent);
}

/** One task against the filter, on its own fields only. */
export function matchesFilter(task: Task, filter: TaskFilter): boolean {
  if (!matchesTask(task, filter.query.trim())) return false;
  if (filter.priority === "none" ? task.priority !== "" : filter.priority && task.priority !== filter.priority) {
    return false;
  }
  if (filter.label && !(task.labels ?? []).includes(filter.label)) return false;
  if (filter.heldByAgent && !task.sessionId) return false;
  return true;
}

/** Every label used on the board, for the filter's choices. */
export function boardLabels(tasks: readonly Task[]): string[] {
  const seen = new Set<string>();
  for (const task of tasks) for (const label of task.labels ?? []) seen.add(label);
  return [...seen].sort((a, b) => a.localeCompare(b));
}

export type Column = {
  /** The status id, or UNKNOWN_COLUMN for tasks whose status the workflow lost. */
  id: string;
  name: string;
  status: Status | null;
  tasks: Task[];
};

export const UNKNOWN_COLUMN = "__unknown__";

/**
 * The board's columns, in workflow order, holding top-level cards only.
 *
 * A card stays when it matches the filter OR one of its subtasks does —
 * subtasks are drawn on their parent's card, so dropping the parent would
 * hide the very match that was searched for.
 *
 * A task whose status the workflow no longer has gets a trailing column of its
 * own instead of disappearing: columns used to be drawn by status id alone, so
 * such a task was on the board, counted nowhere and reachable from nothing.
 */
export function columnsOf(board: Board, tasks: readonly Task[], filter: TaskFilter): Column[] {
  const filtering = isFiltering(filter);
  const matchingParents = new Set<string>();
  if (filtering) {
    for (const task of tasks) {
      if (task.parentId && matchesFilter(task, filter)) matchingParents.add(task.parentId);
    }
  }
  const known = new Set(board.workflow.statuses.map((status) => status.id));
  const byStatus = new Map<string, Task[]>();
  for (const task of tasks) {
    if (task.parentId) continue;
    if (filtering && !matchesFilter(task, filter) && !matchingParents.has(task.id)) continue;
    const key = known.has(task.status) ? task.status : UNKNOWN_COLUMN;
    const list = byStatus.get(key);
    if (list) list.push(task);
    else byStatus.set(key, [task]);
  }
  for (const list of byStatus.values()) list.sort(byPriorityThenAge);

  const columns: Column[] = board.workflow.statuses.map((status) => ({
    id: status.id,
    name: status.name,
    status,
    tasks: byStatus.get(status.id) ?? [],
  }));
  const unknown = byStatus.get(UNKNOWN_COLUMN);
  if (unknown?.length) {
    columns.push({ id: UNKNOWN_COLUMN, name: "No column", status: null, tasks: unknown });
  }
  return columns;
}

function byPriorityThenAge(a: Task, b: Task): number {
  return priorityRank(a.priority) - priorityRank(b.priority) || a.createdAt.localeCompare(b.createdAt);
}

/**
 * The tasks that can be a parent of `task`: top-level, not itself. One level
 * is one level — a task that already has subtasks cannot become one, so it is
 * offered nothing rather than a list the service will refuse.
 */
export function parentCandidates(tasks: readonly Task[], task: Task): Task[] {
  if (tasks.some((candidate) => candidate.parentId === task.id)) return [];
  return tasks.filter((candidate) => candidate.id !== task.id && candidate.parentId === "");
}

/**
 * Whether `from` waits on `target`, directly or through the tasks it waits on.
 * The same walk the service does before storing a dependency, done here so a
 * picker never offers the choice it would refuse.
 */
export function dependsOn(byId: ReadonlyMap<string, Task>, from: string, target: string): boolean {
  const visited = new Set<string>();
  const stack = [from];
  while (stack.length > 0) {
    const current = stack.pop()!;
    if (current === target) return true;
    if (visited.has(current)) continue;
    visited.add(current);
    stack.push(...(byId.get(current)?.blockedBy ?? []));
  }
  return false;
}

/**
 * The tasks `task` can be made to depend on: any other task on the board not
 * already listed, and not one that already waits on `task` — that would be a
 * loop, which the service refuses (tasks.dependencyCycle).
 */
export function blockerCandidates(tasks: readonly Task[], task: Task): Task[] {
  const listed = new Set(task.blockedBy ?? []);
  const byId = indexById(tasks);
  return tasks.filter(
    (candidate) =>
      candidate.id !== task.id && !listed.has(candidate.id) && !dependsOn(byId, candidate.id, task.id),
  );
}

/** The tasks waiting on `taskId` — the other side of blocked_by. */
export function dependentsOf(tasks: readonly Task[], taskId: string): Task[] {
  return tasks.filter((candidate) => (candidate.blockedBy ?? []).includes(taskId));
}

/**
 * The tasks that can be made to wait on `task`: not itself, not already
 * waiting on it, and not one `task` itself waits on (that would be a loop).
 */
export function dependentCandidates(tasks: readonly Task[], task: Task): Task[] {
  const byId = indexById(tasks);
  return tasks.filter(
    (candidate) =>
      candidate.id !== task.id &&
      !(candidate.blockedBy ?? []).includes(task.id) &&
      !dependsOn(byId, task.id, candidate.id),
  );
}

/** `tasks` with one task moved to `to`, for an optimistic board. */
export function withStatus(tasks: readonly Task[], taskId: string, to: string): Task[] {
  return tasks.map((task) => (task.id === taskId ? { ...task, status: to } : task));
}

/**
 * `tasks` with `task` written in: replaced where it was, appended when new,
 * dropped when it left the board or was archived — the list a board holds is
 * the non-archived one (listTasks' default).
 */
export function upsert(tasks: readonly Task[], boardId: string, task: Task): Task[] {
  const stays = task.boardId === boardId && !task.archivedAt;
  // The list half never carries a description; keep it that way here so a
  // store of summaries does not start holding the decrypted half.
  const { description: _description, ...summary } = task;
  const index = tasks.findIndex((candidate) => candidate.id === task.id);
  if (index === -1) return stays ? [...tasks, summary] : [...tasks];
  if (!stays) return tasks.filter((candidate) => candidate.id !== task.id);
  const next = [...tasks];
  next[index] = summary;
  return next;
}

/** "just now", "5m ago", "3h ago", "2d ago", then the date. */
export function relativeTime(iso: string, now: number = Date.now()): string {
  const at = Date.parse(iso);
  if (Number.isNaN(at)) return "";
  const seconds = Math.max(0, Math.round((now - at) / 1000));
  if (seconds < 45) return "just now";
  const minutes = Math.round(seconds / 60);
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.round(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.round(hours / 24);
  if (days < 7) return `${days}d ago`;
  return new Date(at).toLocaleDateString(undefined, { year: "numeric", month: "short", day: "numeric" });
}

export type BoardSummary = {
  /** Every task on the board, subtasks included — what "12 tasks" counts. */
  total: number;
  /** In a terminal column. */
  done: number;
  /** In an active column. */
  active: number;
  /** Not finished and still waiting on an unfinished task. */
  blocked: number;
  /** Not finished and held by an agent session. */
  heldByAgent: number;
};

/**
 * The board at a glance, counted by category so a renamed column still
 * counts. Finished work is neither blocked nor held: a closed task an agent
 * once claimed is not something anybody needs to look at.
 */
export function boardSummary(board: Board, tasks: readonly Task[]): BoardSummary {
  const byId = indexById(tasks);
  const summary: BoardSummary = { total: tasks.length, done: 0, active: 0, blocked: 0, heldByAgent: 0 };
  for (const task of tasks) {
    const category = statusOf(board, task.status)?.category;
    if (category === "terminal") {
      summary.done++;
      continue;
    }
    if (category === "active") summary.active++;
    if (openBlockers(task, byId, board).length > 0) summary.blocked++;
    if (task.sessionId) summary.heldByAgent++;
  }
  return summary;
}

/** Initials for an assignee chip: "syuro" → "SY", "Ada Lovelace" → "AL". */
export function initials(name: string): string {
  const words = name.trim().split(/[\s._-]+/).filter(Boolean);
  if (words.length === 0) return "";
  if (words.length === 1) return words[0].slice(0, 2).toUpperCase();
  return (words[0][0] + words[words.length - 1][0]).toUpperCase();
}

export type TaskView = "board" | "list";
