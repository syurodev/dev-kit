import { useEffect, useMemo, useState } from "react";
import { KanbanSquare, Plus, RefreshCw, Workflow } from "lucide-react";
import { useShallow } from "zustand/react/shallow";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import {
  createTask,
  createTasksFromPlan,
  onTasksChanged,
  progressOf,
  setBoardVerify,
  transitionTask,
  type Board,
  type Task,
} from "@/bridge/tasks";
import { useWorkspaceSwitcherStore } from "@/stores/workspaceSwitcher";
import { BoardVerifyDialog } from "./BoardVerifyDialog";
import { canRunFromPlan, homeBoardOf } from "./boardHome";
import { useTasksStore } from "./tasksStore";

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
        {task.planNodeId ? (
          <span title="Implements a Code Graph plan node">plan</span>
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
 *
 * Which board is a `params.boardId` question now — the sidebar tree is where
 * one is picked and this view no longer keeps its own dropdown or its own
 * "create a board" button. Boards and a shown board's tasks come from
 * tasksStore rather than a fetch of this view's own: the sidebar (mounted for
 * as long as the module is) reads the same state, so a board opened from the
 * tree never pays for a second round trip of what it already has.
 */
export function TasksBoardView({ params }: { params?: Record<string, unknown> }) {
  const boardIdParam = typeof params?.boardId === "string" ? params.boardId : "";
  // The board whose close rule is being edited, or null.
  const [editingVerify, setEditingVerify] = useState<Board | null>(null);
  const [error, setError] = useState("");
  const currentWorkspaceId = useWorkspaceSwitcherStore((s) => s.currentId);

  const { boards, tasksByBoard, storeError, refreshBoards, ensureBoardTasks, refreshBoard } =
    useTasksStore(
      useShallow((s) => ({
        boards: s.boards,
        tasksByBoard: s.tasksByBoard,
        storeError: s.error,
        refreshBoards: s.refreshBoards,
        ensureBoardTasks: s.ensureBoardTasks,
        refreshBoard: s.refreshBoard,
      })),
    );

  // The sidebar reads this on its own mount and stays mounted for the
  // module's life, so this is normally a no-op — but a board tab can be
  // restored (or this view can be someone's first paint of the module)
  // before that read has landed, and asking again costs nothing once it has.
  useEffect(() => {
    void refreshBoards();
  }, [refreshBoards]);

  // With no boardId — the module's `opensHome` tab opens with none — show
  // the current workspace's first board by name rather than nothing: the
  // sidebar tree is where a board is normally chosen, but arriving at Tasks
  // with nothing open should still show something when there is one to show.
  const board = useMemo(() => {
    if (!boards) return null;
    if (boardIdParam) return boards.find((candidate) => candidate.id === boardIdParam) ?? null;
    return homeBoardOf(boards, currentWorkspaceId);
  }, [boards, boardIdParam, currentWorkspaceId]);

  useEffect(() => {
    if (board) void ensureBoardTasks(board.id);
  }, [board, ensureBoardTasks]);

  // Its own subscription rather than leaning on the sidebar's: a board tab
  // outlives whatever else happens to be on screen beside it, and reloading
  // here should not depend on the rail staying mounted. Lightly debounced the
  // same way tasksStore.subscribeTasksChanged is (150ms, per board) — an
  // agent moving several subtasks in a row is one burst of events for the one
  // board this view shows, not one refetch per event.
  useEffect(() => {
    if (!board) return;
    const boardId = board.id;
    let timer: ReturnType<typeof setTimeout> | undefined;
    let boardLevel = false;
    const off = onTasksChanged((e) => {
      if (e.boardId !== boardId) return;
      // A board-level event (taskId === "") may have changed the board's own
      // fields (name, verify, workspaceId), so it earns a refreshBoards() on
      // top of the board's own re-list.
      if (e.taskId === "") boardLevel = true;
      clearTimeout(timer);
      timer = setTimeout(() => {
        void refreshBoard(boardId);
        if (boardLevel) {
          boardLevel = false;
          void refreshBoards();
        }
      }, 150);
    });
    return () => {
      off();
      clearTimeout(timer);
    };
  }, [board, refreshBoard, refreshBoards]);

  if (storeError && !boards) {
    return <EmptyState icon={KanbanSquare} title="Tasks are unavailable" body={storeError} />;
  }
  if (!boards) {
    return null;
  }
  if (!board) {
    return (
      <EmptyState
        icon={KanbanSquare}
        title="No board here"
        body={
          boardIdParam
            ? "This board is gone. Pick another one in the Tasks sidebar."
            : boards.length === 0
              ? "A board holds the work an agent can pick up. Create one in the Tasks sidebar."
              : "This workspace has no board yet. Create one in the Tasks sidebar."
        }
      />
    );
  }

  const tasks = tasksByBoard[board.id];
  if (!tasks) {
    return null;
  }

  return (
    <div className="flex h-full min-h-0 flex-col">
      <header className="flex items-center gap-2 border-b border-border-subtle px-3 py-2">
        <h2 className="min-w-0 truncate text-sm font-medium">{board.name}</h2>
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
        {/* Only offered for a board bound to a workspace: the plan belongs to
            a service of that workspace, and a board pointed nowhere would
            build a backlog out of nobody's plan (boardHome.canRunFromPlan). */}
        {canRunFromPlan(board) ? (
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
                  void ensureBoardTasks(board.id);
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
              .then(() => ensureBoardTasks(board.id))
              .catch((e: Error) => setError(e.message));
          }}
        >
          <Plus className="size-4" aria-hidden />
          New task
        </Button>
        <Button
          variant="ghost"
          size="icon"
          aria-label="Reload"
          onClick={() => void ensureBoardTasks(board.id)}
        >
          <RefreshCw className="size-4" aria-hidden />
        </Button>
      </header>
      <BoardVerifyDialog
        board={editingVerify}
        onClose={() => setEditingVerify(null)}
        onSave={(command, timeoutSec, service) => {
          setBoardVerify(editingVerify?.id ?? "", command, timeoutSec, service)
            .then(() => {
              setEditingVerify(null);
              void refreshBoards();
            })
            .catch((e: Error) => setError(e.message));
        }}
      />
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
                        .then(() => ensureBoardTasks(board.id))
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
