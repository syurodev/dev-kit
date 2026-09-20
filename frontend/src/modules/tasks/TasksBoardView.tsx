import { useCallback, useEffect, useMemo, useState } from "react";
import { KanbanSquare, Plus, RefreshCw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import {
  createBoard,
  createTask,
  listBoards,
  listTasks,
  progressOf,
  transitionTask,
  type Board,
  type Task,
} from "@/bridge/tasks";

function TaskCard({
  task,
  board,
  siblings,
  onMove,
}: {
  task: Task;
  board: Board;
  siblings: Task[];
  onMove: (to: string) => void;
}) {
  const children = siblings.filter((candidate) => candidate.parentId === task.id);
  const progress = children.length > 0 ? progressOf(board, children) : null;

  return (
    <li className="rounded-md border border-border-subtle bg-surface-well p-2">
      <div className="flex items-baseline gap-2">
        <code className="text-xs text-fg-muted">{task.key}</code>
        {task.sessionId ? (
          <span
            className="size-1.5 rounded-full bg-emerald-500"
            title="An agent session holds this task"
            aria-label="Held by an agent"
          />
        ) : null}
      </div>
      <div className="pt-0.5 text-sm">{task.title}</div>
      <div className="flex items-center gap-2 pt-1 text-xs text-fg-muted">
        {progress ? (
          // Counted by category, so a board that renamed its Done column still
          // counts. It counts without blocking: the parent can still close.
          <span>
            {progress.done}/{progress.total}
          </span>
        ) : null}
        {task.branch ? <span className="truncate font-mono">{task.branch}</span> : null}
        <div className="flex-1" />
        <select
          className="bg-transparent text-xs"
          value={task.status}
          aria-label={`Status of ${task.key}`}
          onChange={(event) => onMove(event.target.value)}
        >
          {board.workflow.statuses.map((status) => (
            <option key={status.id} value={status.id}>
              {status.name}
            </option>
          ))}
        </select>
      </div>
    </li>
  );
}

/**
 * The board. Columns come from the workflow in declaration order, and every
 * rule reads a status's category rather than its name — renaming a column
 * cannot move a gate.
 */
export function TasksBoardView() {
  const [boards, setBoards] = useState<Board[] | null>(null);
  const [boardID, setBoardID] = useState("");
  // null until the first load answers: an empty array here cannot tell
  // "not loaded yet" from "this board is empty", and the columns would
  // flash zero counts on every open.
  const [tasks, setTasks] = useState<Task[] | null>(null);
  const [error, setError] = useState("");

  const board = useMemo(
    () => boards?.find((candidate) => candidate.id === boardID) ?? null,
    [boards, boardID],
  );

  const reloadBoards = useCallback(() => {
    listBoards()
      .then((next) => {
        setBoards(next);
        setBoardID((current) => current || next[0]?.id || "");
        setError("");
      })
      .catch((e: Error) => setError(e.message));
  }, []);

  const reloadTasks = useCallback(() => {
    if (!boardID) {
      setTasks(null);
      return;
    }
    listTasks(boardID)
      .then(setTasks)
      .catch((e: Error) => setError(e.message));
  }, [boardID]);

  useEffect(reloadBoards, [reloadBoards]);
  useEffect(reloadTasks, [reloadTasks]);

  if (error && !boards) {
    return <EmptyState icon={KanbanSquare} title="Tasks are unavailable" body={error} />;
  }
  if (!boards) {
    return null;
  }
  if (boards.length === 0) {
    return (
      <EmptyState icon={KanbanSquare} title="No board yet" body="A board holds the work, and an agent can move it.">
        <Button
          size="sm"
          onClick={() => {
            createBoard("DevKit", "DEV", "blue").then(reloadBoards).catch((e: Error) => setError(e.message));
          }}
        >
          Create a board
        </Button>
      </EmptyState>
    );
  }
  if (!board || !tasks) {
    return null;
  }

  return (
    <div className="flex h-full min-h-0 flex-col">
      <header className="flex items-center gap-2 border-b border-border-subtle px-3 py-2">
        <select
          className="bg-transparent text-sm"
          value={boardID}
          aria-label="Board"
          onChange={(event) => setBoardID(event.target.value)}
        >
          {boards.map((candidate) => (
            <option key={candidate.id} value={candidate.id}>
              {candidate.name}
            </option>
          ))}
        </select>
        {board.verify ? (
          <span className="text-xs text-fg-muted" title={board.verify.command}>
            closes on `{board.verify.command}`
          </span>
        ) : (
          <span className="text-xs text-fg-muted">closing asks every time</span>
        )}
        <div className="flex-1" />
        <Button
          variant="ghost"
          size="sm"
          onClick={() => {
            createTask({ boardId: board.id, title: "New task" })
              .then(reloadTasks)
              .catch((e: Error) => setError(e.message));
          }}
        >
          <Plus className="size-4" aria-hidden />
          New task
        </Button>
        <Button variant="ghost" size="icon" aria-label="Reload" onClick={reloadTasks}>
          <RefreshCw className="size-4" aria-hidden />
        </Button>
      </header>
      {error ? <p className="px-3 py-1 text-xs text-red-500">{error}</p> : null}
      <div className="flex min-h-0 flex-1 gap-3 overflow-auto p-3">
        {board.workflow.statuses.map((status) => {
          const column = tasks.filter(
            (task) => task.status === status.id && task.parentId === "",
          );
          return (
            <section key={status.id} className="flex w-64 shrink-0 flex-col gap-2">
              <h3 className="text-xs font-medium text-fg-muted">
                {status.name} ({column.length})
              </h3>
              <ul className="flex flex-col gap-2">
                {column.map((task) => (
                  <TaskCard
                    key={task.id}
                    task={task}
                    board={board}
                    siblings={tasks}
                    onMove={(to) => {
                      // The status the card was showing goes along, so a card
                      // somebody else moved is refused rather than overwritten.
                      transitionTask(task.id, to, task.status)
                        .then(reloadTasks)
                        .catch((e: Error) => setError(e.message));
                    }}
                  />
                ))}
              </ul>
            </section>
          );
        })}
      </div>
    </div>
  );
}
