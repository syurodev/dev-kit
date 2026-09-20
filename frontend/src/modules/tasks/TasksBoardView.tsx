import { useCallback, useEffect, useMemo, useState } from "react";
import {
  DndContext,
  DragOverlay,
  PointerSensor,
  pointerWithin,
  useSensor,
  useSensors,
} from "@dnd-kit/core";
import { KanbanSquare, Plus, RefreshCw, Workflow } from "lucide-react";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import {
  createBoard,
  createTask,
  createTasksFromPlan,
  listBoards,
  listTasks,
  setBoardVerify,
  transitionTask,
  type Board,
  type Task,
} from "@/bridge/tasks";
import { BoardVerifyDialog } from "./BoardVerifyDialog";
import { BoardColumn, TaskCard } from "./BoardColumn";
import { columnsOf, dropTarget } from "./board";

/**
 * The board. Columns come from the workflow in declaration order, and every
 * rule reads a status's category rather than its name — renaming a column
 * cannot move a gate.
 */
export function TasksBoardView() {
  const [boards, setBoards] = useState<Board[] | null>(null);
  const [boardID, setBoardID] = useState("");
  // The board whose close rule is being edited, or null.
  const [editingVerify, setEditingVerify] = useState<Board | null>(null);
  // The card under the cursor, for the copy that follows it.
  const [dragging, setDragging] = useState<Task | null>(null);
  // Four pixels before a drag starts, so a click on a card's dropdown is
  // still a click and not a one-pixel drag that swallows it.
  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 4 } }));
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

  // One path for both ways of moving a card. The status the card was SHOWING
  // goes along, so a card somebody else already moved is refused rather than
  // overwritten — which matters more for a drag, where the card under the
  // cursor may have been stale since before the drag began.
  function move(task: Task, to: string) {
    transitionTask(task.id, to, task.status)
      .then(reloadTasks)
      .catch((e: Error) => setError(e.message));
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
        {/* tasks:admin and UI-only: an agent can trigger the command and can
            never choose it. With no UI at all it was unreachable rather than
            protected, which is not the same thing. */}
        <Button
          variant="ghost"
          size="sm"
          className="text-fg-muted"
          title={
            board.verify
              ? `DevKit runs ${board.verify.command} and the exit code decides`
              : "Choose a command to close tasks with"
          }
          onClick={() => setEditingVerify(board)}
        >
          {board.verify ? (
            <span className="max-w-64 truncate font-mono">closes on {board.verify.command}</span>
          ) : (
            "closing asks every time"
          )}
        </Button>
        <div className="flex-1" />
        {/* Only offered for a board that names a project: the plan belongs to
            the project Code Graph has open, and a board pointed somewhere else
            would build a backlog out of somebody else's plan. */}
        {board.projectPath ? (
          <Button
            variant="ghost"
            size="sm"
            title="One task per node of the approved plan, each linked to its node"
            onClick={() => {
              createTasksFromPlan(board.id)
                .then((created) => {
                  if (created.length === 0) {
                    // Nothing created is the normal answer on a second run,
                    // not a failure — say which it was.
                    setError("Every node of the plan already has a task.");
                  } else {
                    setError("");
                  }
                  reloadTasks();
                })
                .catch((e: Error) => setError(e.message));
            }}
          >
            <Workflow className="size-4" aria-hidden />
            From plan
          </Button>
        ) : null}
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
      <BoardVerifyDialog
        board={editingVerify}
        onClose={() => setEditingVerify(null)}
        onSave={(command, timeoutSec, projectPath) => {
          setBoardVerify(editingVerify?.id ?? "", command, timeoutSec, projectPath)
            .then(() => {
              setEditingVerify(null);
              reloadBoards();
            })
            .catch((e: Error) => setError(e.message));
        }}
      />
      {error ? <p className="px-3 py-1 text-xs text-red-500">{error}</p> : null}
      <DndContext
        sensors={sensors}
        // pointerWithin rather than closestCenter: a column is a large
        // rectangle, and what is under the cursor is the answer — not the
        // column whose centre happens to be nearest to a card's own centre.
        collisionDetection={pointerWithin}
        onDragStart={({ active }) =>
          setDragging(tasks.find((task) => task.id === active.id) ?? null)
        }
        onDragCancel={() => setDragging(null)}
        onDragEnd={({ active, over }) => {
          const task = tasks.find((candidate) => candidate.id === active.id);
          setDragging(null);
          if (!task) return;
          const to = dropTarget(task.status, over ? String(over.id) : null);
          if (!to) return;
          move(task, to);
        }}
      >
        <div className="flex min-h-0 flex-1 gap-3 overflow-x-auto p-3">
          {columnsOf(board, tasks).map((column) => (
            <BoardColumn
              key={column.id || "orphans"}
              column={column}
              board={board}
              allTasks={tasks}
              onMove={move}
            />
          ))}
        </div>
        <DragOverlay>
          {dragging ? (
            <ul className="w-64">
              <TaskCard
                task={dragging}
                board={board}
                siblings={tasks}
                onMove={() => undefined}
                overlay
              />
            </ul>
          ) : null}
        </DragOverlay>
      </DndContext>
    </div>
  );
}
