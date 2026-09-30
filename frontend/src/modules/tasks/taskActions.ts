import { toast } from "sonner";
import {
  addComment,
  archiveTask,
  deleteTask,
  setTaskFields,
  transitionTask,
  unarchiveTask,
  updateTask,
  TASK_STATUS_CHANGED,
  type Comment,
  type Task,
  type TaskFieldsPatch,
} from "@/bridge/tasks";
import { errorMessage, toAppError } from "@/bridge/errors";
import { queryClient } from "@/lib/query";
import { useTasksStore } from "./tasksStore";

/**
 * Every write the board and the task detail make, in one place, each one
 * optimistic: the board is local data (Go + SQLite on the same machine), so a
 * change is drawn in the frame of the click and the bridge's answer only
 * confirms it (smoothness §10, "Optimistic update").
 *
 * Two copies of a task are on screen and both are written: the task detail's
 * query cache (the full task, description included) and tasksStore (the
 * board's summaries, which never hold a description). A refusal puts both
 * back by reading the truth again, rather than by replaying an inverse that
 * could be wrong if something else moved in between.
 */

export const taskKeys = {
  task: (id: string) => ["tasks", "task", id] as const,
  comments: (id: string) => ["tasks", "comments", id] as const,
  runs: (id: string) => ["tasks", "runs", id] as const,
};

/**
 * The bridge's answer, written into both copies. `description` is the one
 * field a metadata write does not carry back (List/SetFields leave it out and
 * omitempty drops an empty one), so the cache keeps what it had unless the
 * caller knows the new value.
 */
function settle(next: Task, description?: string): void {
  // Only a task the detail has already read: seeding the cache from a board
  // move would hand the next detail tab a summary with no description, and it
  // would draw "Add a description" for a frame before the real read landed.
  queryClient.setQueryData<Task>(taskKeys.task(next.id), (old) =>
    old ? { ...next, description: description ?? old.description ?? next.description } : old,
  );
  useTasksStore.getState().upsertTask(next);
}

/** Draw `task` as it is about to be, in both copies. */
function preview(task: Task, change: Partial<Task>): void {
  const next = { ...task, ...change };
  queryClient.setQueryData<Task>(taskKeys.task(task.id), (old) => (old ? { ...old, ...change } : old));
  useTasksStore.getState().upsertTask(next);
}

/** A refusal: read both copies again and say why. */
function revert(task: Task, error: unknown): void {
  void queryClient.invalidateQueries({ queryKey: taskKeys.task(task.id) });
  void useTasksStore.getState().ensureBoardTasks(task.boardId);
  if (toAppError(error).code === TASK_STATUS_CHANGED) {
    // Not a failure: two people moved one card and the other move landed
    // first. The board now shows where it went.
    toast.info("This task was moved somewhere else in the meantime.");
    return;
  }
  toast.error(errorMessage(error));
}

export function setFields(task: Task, patch: TaskFieldsPatch): Promise<void> {
  preview(task, patch);
  return setTaskFields(task.id, patch).then(
    (next) => settle(next),
    (error) => revert(task, error),
  );
}

/**
 * Title and description travel together — the service reseals the payload
 * whole — so a title edit sends the description it was showing.
 */
export function saveText(task: Task, title: string, description: string): Promise<void> {
  preview(task, { title, description });
  return updateTask(task.id, title, description).then(
    (next) => settle(next, description),
    (error) => revert(task, error),
  );
}

/**
 * Moves a task, carrying the status it was drawn in: a card someone else
 * moved is refused (TASK_STATUS_CHANGED) rather than overwritten.
 */
export function moveTo(task: Task, to: string): Promise<void> {
  if (task.status === to) return Promise.resolve();
  preview(task, { status: to });
  return transitionTask(task.id, to, task.status).then(
    (result) => settle(result.task),
    (error) => revert(task, error),
  );
}

export function archive(task: Task): Promise<void> {
  const at = new Date().toISOString();
  preview(task, { archivedAt: at });
  return archiveTask(task.id).then(
    (next) => {
      settle(next);
      toast.success(`${task.key} archived`, {
        action: { label: "Undo", onClick: () => void unarchive(next) },
      });
    },
    (error) => revert(task, error),
  );
}

export function unarchive(task: Task): Promise<void> {
  preview(task, { archivedAt: "" });
  return unarchiveTask(task.id).then(
    (next) => settle(next),
    (error) => revert(task, error),
  );
}

/** Deletes for good. The caller asks first; this does not. */
export function remove(task: Task): Promise<boolean> {
  return deleteTask(task.id).then(
    () => {
      useTasksStore.getState().removeTask(task.boardId, task.id);
      queryClient.removeQueries({ queryKey: taskKeys.task(task.id) });
      return true;
    },
    (error) => {
      toast.error(errorMessage(error));
      return false;
    },
  );
}

/**
 * Posts a comment and puts it in the conversation at once. The author is
 * stamped by the service from the caller, never sent — so the placeholder
 * says "user", which is what the service will say too.
 */
export function comment(taskId: string, body: string): Promise<boolean> {
  const pending: Comment = {
    id: `pending:${Date.now()}`,
    taskId,
    author: "user",
    kind: "human",
    body,
    createdAt: new Date().toISOString(),
  };
  const key = taskKeys.comments(taskId);
  queryClient.setQueryData<Comment[]>(key, (old) => [...(old ?? []), pending]);
  return addComment(taskId, body).then(
    (saved) => {
      queryClient.setQueryData<Comment[]>(key, (old) =>
        (old ?? []).map((candidate) => (candidate.id === pending.id ? saved : candidate)),
      );
      return true;
    },
    (error) => {
      queryClient.setQueryData<Comment[]>(key, (old) =>
        (old ?? []).filter((candidate) => candidate.id !== pending.id),
      );
      toast.error(errorMessage(error));
      return false;
    },
  );
}
