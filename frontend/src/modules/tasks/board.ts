import type { Board, Task } from "@/bridge/tasks";

/**
 * Turning a flat list of tasks into the board's columns.
 *
 * It is separate from the view because of the last rule here: a task whose
 * status is not in the workflow used to vanish from the board entirely, with
 * nothing saying it existed. A column that cannot be reached is worse than an
 * untidy one — the card is still there, still assigned, and the only way to
 * move it was a status dropdown on a card nobody could see.
 */

export type BoardColumnModel = {
  id: string;
  name: string;
  tasks: Task[];
  /**
   * True for the bucket holding tasks whose status the workflow no longer
   * has. It is not a status: dropping a card INTO it would mean writing a
   * status that does not exist.
   */
  orphaned?: boolean;
};

/** The heading a bucket of unreachable tasks gets. */
export const ORPHAN_COLUMN = "Not in this workflow";

/**
 * Columns in workflow order, holding top-level tasks only — a subtask belongs
 * to its parent's card, not to a column of its own.
 */
export function columnsOf(board: Board, tasks: readonly Task[]): BoardColumnModel[] {
  const known = new Set(board.workflow.statuses.map((status) => status.id));
  const top = tasks.filter((task) => task.parentId === "");

  const columns: BoardColumnModel[] = board.workflow.statuses.map((status) => ({
    id: status.id,
    name: status.name,
    tasks: top.filter((task) => task.status === status.id),
  }));

  const orphans = top.filter((task) => !known.has(task.status));
  if (orphans.length > 0) {
    columns.push({ id: "", name: ORPHAN_COLUMN, tasks: orphans, orphaned: true });
  }
  return columns;
}

/**
 * The status a drop should move a card to, or "" for a drop that changes
 * nothing.
 *
 * Dropping a card back where it came from is the commonest drop of all — a
 * drag somebody thought better of — and sending it as a transition would burn
 * a verify run, an audit line and possibly a confirmation on a move that is
 * not a move.
 */
export function dropTarget(from: string, over: string | null): string {
  if (!over || over === from) return "";
  return over;
}
