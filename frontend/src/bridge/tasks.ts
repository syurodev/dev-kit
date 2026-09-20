import { AppService } from "../../bindings/devkit/internal/bridge";
import { callBridge } from "./client";

export type Category = "open" | "active" | "terminal";

export type Status = { id: string; name: string; category: Category };
export type Workflow = { statuses: Status[] };

/**
 * The command DevKit runs when an agent asks to close a task. It lives on the
 * board because it must be the user's: an agent can trigger it and can never
 * change it.
 */
export type Verify = { command: string; timeout_sec: number };

export type Board = {
  id: string;
  name: string;
  keyPrefix: string;
  color: string;
  workflow: Workflow;
  verify?: Verify | null;
  branchTemplate?: string;
  projectPath?: string;
};

export type Transition = { from: string; to: string; by: string; at: string };

export type Task = {
  id: string;
  boardId: string;
  key: string;
  title: string;
  status: string;
  priority: string;
  labels: string[] | null;
  assignee: string;
  parentId: string;
  blockedBy: string[] | null;
  planNodeId: string;
  sessionId: string;
  /** The branch this task's work is on — the one git fact a task stores. */
  branch: string;
  archivedAt: string;
  history: Transition[] | null;
  createdAt: string;
  updatedAt: string;
  description?: string;
};

export type Comment = {
  id: string;
  taskId: string;
  author: string;
  kind: "human" | "agent" | "system";
  body: string;
  createdAt: string;
};

/** A close in flight: the task has NOT moved until the run lands. */
export type TransitionResult = { task: Task; verifying: boolean; runId?: string };

export type Progress = { done: number; total: number };

export type Run = {
  id: string;
  taskId: string;
  target: string;
  command: string;
  state: "queued" | "running" | "passed" | "failed" | "cancelled" | "stale";
  exitCode: number;
  output: string;
  requestedAt: string;
};

export function listBoards(): Promise<Board[]> {
  return callBridge(async () => ((await AppService.TasksBoardList()) ?? []) as Board[]);
}

export function createBoard(name: string, keyPrefix: string, color: string): Promise<Board> {
  return callBridge(
    async () => (await AppService.TasksBoardCreate(name, keyPrefix, color)) as Board,
  );
}

export function setBoardVerify(
  boardId: string,
  command: string,
  timeoutSec: number,
  projectPath: string,
): Promise<Board> {
  return callBridge(
    async () =>
      (await AppService.TasksBoardSetVerify(boardId, command, timeoutSec, projectPath)) as Board,
  );
}

export function listTasks(boardId: string, includeArchived = false): Promise<Task[]> {
  return callBridge(
    async () =>
      ((await AppService.TasksList(boardId, "", "", "", includeArchived, 0)) ?? []) as Task[],
  );
}

export function getTask(id: string): Promise<Task> {
  return callBridge(async () => (await AppService.TasksGet(id)) as Task);
}

export function createTask(input: {
  boardId: string;
  title: string;
  description?: string;
  priority?: string;
  parentId?: string;
  labels?: string[];
}): Promise<Task> {
  return callBridge(
    async () =>
      (await AppService.TasksCreate(
        input.boardId,
        input.title,
        input.description ?? "",
        input.priority ?? "",
        input.parentId ?? "",
        input.labels ?? [],
      )) as Task,
  );
}

export function updateTask(id: string, title: string, description: string): Promise<Task> {
  return callBridge(async () => (await AppService.TasksUpdate(id, title, description)) as Task);
}

/** Metadata-only: never reseals the description. */
export function setTaskFields(
  id: string,
  fields: { priority?: string; assignee?: string; parentId?: string; branch?: string; labels?: string[] },
): Promise<Task> {
  return callBridge(
    async () =>
      (await AppService.TasksSetFields(
        id,
        fields.priority ?? "",
        fields.assignee ?? "",
        fields.parentId ?? "",
        fields.branch ?? "",
        fields.labels ?? null,
      )) as Task,
  );
}

/**
 * Moves a task. `expectedStatus` makes it optimistic: a card someone else
 * moved is refused rather than overwritten.
 */
export function transitionTask(
  id: string,
  to: string,
  expectedStatus = "",
): Promise<TransitionResult> {
  return callBridge(
    async () => (await AppService.TasksTransition(id, to, expectedStatus)) as TransitionResult,
  );
}

export function archiveTask(id: string): Promise<Task> {
  return callBridge(async () => (await AppService.TasksArchive(id)) as Task);
}

export function deleteTask(id: string): Promise<void> {
  return callBridge(async () => {
    await AppService.TasksDelete(id);
  });
}

export function listComments(taskId: string): Promise<Comment[]> {
  return callBridge(
    async () => ((await AppService.TasksCommentList(taskId)) ?? []) as Comment[],
  );
}

export function addComment(taskId: string, body: string): Promise<Comment> {
  return callBridge(async () => (await AppService.TasksCommentAdd(taskId, body)) as Comment);
}

/**
 * Finds tasks by key, title or label. Not by description: that is the
 * encrypted half, and searching it would mean decrypting the whole board.
 */
export function searchTasks(boardId: string, query: string): Promise<Task[]> {
  return callBridge(async () => ((await AppService.TasksSearch(boardId, query)) ?? []) as Task[]);
}

/**
 * Points a task at a Code Graph plan node, or clears it with "".
 *
 * The link lives on the task and only there: `plan_nodes` is rewritten
 * wholesale on every proposal, so a back-reference on that side would be wiped
 * each time an agent re-proposed the plan.
 */
export function linkPlanNode(id: string, planNodeId: string): Promise<Task> {
  return callBridge(async () => (await AppService.TasksLinkPlanNode(id, planNodeId)) as Task);
}

/** A board's tasks grouped by the plan node they implement, for canvas chips. */
export function tasksByPlanNode(boardId: string): Promise<Record<string, Task[]>> {
  return callBridge(
    async () => ((await AppService.TasksByPlanNode(boardId)) ?? {}) as Record<string, Task[]>,
  );
}

/**
 * Turns the project's APPROVED plan into a backlog, one task per node. A draft
 * is refused — it is a proposal under discussion, not agreed work. Safe to run
 * twice: a node that already has a task is skipped.
 */
export function createTasksFromPlan(boardId: string): Promise<Task[]> {
  return callBridge(
    async () => ((await AppService.TasksCreateFromPlan(boardId)) ?? []) as Task[],
  );
}

export function taskProgress(parentId: string): Promise<Progress> {
  return callBridge(async () => (await AppService.TasksProgress(parentId)) as Progress);
}

export function verifyRuns(taskId: string, limit = 20): Promise<Run[]> {
  return callBridge(async () => ((await AppService.TasksVerifyRuns(taskId, limit)) ?? []) as Run[]);
}

export function cancelVerify(runId: string): Promise<void> {
  return callBridge(async () => {
    await AppService.TasksVerifyCancel(runId);
  });
}

/**
 * Splits comments into the conversation and the Activity tab. System comments
 * are the service's own record and are not part of what people said.
 */
export function splitComments(comments: Comment[]): { conversation: Comment[]; activity: Comment[] } {
  return {
    conversation: comments.filter((comment) => comment.kind !== "system"),
    activity: comments.filter((comment) => comment.kind === "system"),
  };
}

/**
 * Subtask progress, counted by category so a board that renamed its Done
 * column still counts. Counting is not blocking: a parent closes with children
 * open, the same rule blockedBy follows.
 */
export function progressOf(board: Board, children: Task[]): Progress {
  const terminal = new Set(
    board.workflow.statuses.filter((status) => status.category === "terminal").map((s) => s.id),
  );
  return {
    done: children.filter((child) => terminal.has(child.status)).length,
    total: children.length,
  };
}
