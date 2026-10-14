import { useCallback, useDeferredValue, useEffect, useMemo, useState } from "react";
import {
  DndContext,
  DragOverlay,
  PointerSensor,
  useSensor,
  useSensors,
  type DragEndEvent,
  type DragStartEvent,
} from "@dnd-kit/core";
import { KanbanSquare, Plus, RefreshCw, Rows3, Workflow } from "lucide-react";
import { useShallow } from "zustand/react/shallow";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import { useModuleVisibility } from "@/components/ui/module-visibility";
import { useSplitPanelActions } from "@/components/ui/split-panel";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import {
  createTask,
  createTasksFromPlan,
  onTasksChanged,
  setBoardVerify,
  type Board,
  type Task,
} from "@/bridge/tasks";
import { cn } from "@/lib/utils";
import { useWorkspaceSwitcherStore } from "@/stores/workspaceSwitcher";
import { BoardVerifyDialog } from "./BoardVerifyDialog";
import { canRunFromPlan, homeBoardOf } from "./boardHome";
import { NewTaskDialog } from "./CreateDialogs";
import { BoardColumn } from "./board/BoardColumn";
import { BoardFilters } from "./board/BoardFilters";
import { BoardList } from "./board/BoardList";
import { BoardSummaryBar } from "./board/BoardSummary";
import { CARD_CLASS, TaskCardBody } from "./board/TaskCard";
import { TaskDetail } from "./detail/TaskDetail";
import { moveTo } from "./taskActions";
import {
  EMPTY_FILTER,
  boardLabels,
  boardSummary,
  columnsOf,
  indexById,
  openBlockers,
  type TaskFilter,
  type TaskView,
} from "./taskModel";
import { useTasksStore } from "./tasksStore";

/** Room for a task's text and its property column, one above the other. */
const INSPECTOR_WIDTH = 460;

/** Subtasks by their parent, built once per render of the board. */
function childrenByParent(tasks: readonly Task[]): Map<string, Task[]> {
  const out = new Map<string, Task[]>();
  for (const task of tasks) {
    if (!task.parentId) continue;
    const list = out.get(task.parentId);
    if (list) list.push(task);
    else out.set(task.parentId, [task]);
  }
  return out;
}

/**
 * The columns and the drag between them. Its own component so the filter
 * row's keystrokes and the board's header do not re-render every card — the
 * columns read a deferred copy of the filter.
 */
function BoardColumns({
  board,
  tasks,
  filter,
  view,
  inspectedId,
  onInspect,
}: {
  board: Board;
  tasks: readonly Task[];
  filter: TaskFilter;
  view: TaskView;
  inspectedId: string | null;
  onInspect: (task: Task) => void;
}) {
  const [dragging, setDragging] = useState<Task | null>(null);
  // A press becomes a drag only after the pointer travels, so a click on a
  // card still opens it (the same threshold the result grid's columns use).
  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 4 } }));
  const deferred = useDeferredValue(filter);
  const columns = columnsOf(board, tasks, deferred);
  const byId = indexById(tasks);
  const children = childrenByParent(tasks);

  const onDragStart = (event: DragStartEvent) => {
    setDragging((event.active.data.current?.task as Task | undefined) ?? null);
  };
  const onDragEnd = (event: DragEndEvent) => {
    setDragging(null);
    const task = event.active.data.current?.task as Task | undefined;
    const to = event.over?.id;
    if (!task || typeof to !== "string" || to === task.status) return;
    if (!board.workflow.statuses.some((status) => status.id === to)) return;
    // Optimistic, carrying the status the card was drawn in: a card someone
    // else moved meanwhile is refused and the board re-reads, never overwritten.
    void moveTo(task, to);
  };

  if (view === "list") {
    return (
      <div className={cn("flex min-h-0 flex-1 flex-col", deferred !== filter && "opacity-80")}>
        <BoardList
          board={board}
          columns={columns}
          childrenByParent={children}
          byId={byId}
          inspectedId={inspectedId}
          onInspect={onInspect}
        />
      </div>
    );
  }

  return (
    <DndContext
      sensors={sensors}
      onDragStart={onDragStart}
      onDragEnd={onDragEnd}
      onDragCancel={() => setDragging(null)}
    >
      <div
        className={cn(
          "flex min-h-0 flex-1 gap-3 overflow-x-auto p-4 transition-opacity duration-(--motion-fast)",
          // A filter still catching up to the box is drawn faintly rather
          // than blocking the keystroke (smoothness 6.5).
          deferred !== filter && "opacity-80",
        )}
      >
        {columns.map((column) => (
          <BoardColumn
            key={column.id}
            column={column}
            board={board}
            childrenByParent={children}
            byId={byId}
            inspectedId={inspectedId}
            onInspect={onInspect}
            dragging={dragging !== null}
          />
        ))}
      </div>
      {/* No drop animation: the card is already in its new column (optimistic)
          by the time the overlay would fly there, and auto-animate brings it
          in — two motions for one move would be one too many. */}
      <DragOverlay dropAnimation={null}>
        {dragging ? (
          <div className={cn(CARD_CLASS, "w-70 cursor-grabbing border-status-info/40 shadow-2xl")}>
            <TaskCardBody
              task={dragging}
              board={board}
              subtasks={children.get(dragging.id) ?? []}
              waitingOn={openBlockers(dragging, byId, board).length}
            />
          </div>
        ) : null}
      </DragOverlay>
    </DndContext>
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
  // Per board, so switching boards in this tab does not carry one board's
  // filter onto another. Local on purpose: a filter is how this person is
  // looking right now, not something the board is.
  const [filters, setFilters] = useState<Record<string, TaskFilter>>({});
  const [creating, setCreating] = useState(false);
  const currentWorkspaceId = useWorkspaceSwitcherStore((s) => s.currentId);

  const { boards, tasksByBoard, storeError, refreshBoards, ensureBoardTasks, refreshBoard, boardViews, setBoardView } =
    useTasksStore(
      useShallow((s) => ({
        boards: s.boards,
        tasksByBoard: s.tasksByBoard,
        storeError: s.error,
        refreshBoards: s.refreshBoards,
        ensureBoardTasks: s.ensureBoardTasks,
        refreshBoard: s.refreshBoard,
        boardViews: s.boardViews,
        setBoardView: s.setBoardView,
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

  // The task shown in the module's right-hand pane, remembered with the board
  // it was picked on so switching this tab to another board does not keep
  // showing a task from the last one.
  const [inspected, setInspected] = useState<{ boardId: string; taskId: string } | null>(null);
  const inspectedId = board && inspected?.boardId === board.id ? inspected.taskId : null;
  const closeInspector = useCallback(() => setInspected(null), []);

  // The pane (the shell's SplitPanel: it squeezes the board rather than
  // covering it, and is resizable) is one per module, so only the board tab
  // on screen may fill it — a board parked behind another tab keeps its pick
  // and puts it back when it is shown again. The content is keyed by task id
  // only; TaskDetail reads and subscribes by itself, so an edit in the pane
  // never re-pushes it.
  const visible = useModuleVisibility();
  const { openPanel, closePanel } = useSplitPanelActions();
  useEffect(() => {
    if (!visible || !inspectedId) return;
    openPanel(<TaskDetail key={inspectedId} taskId={inspectedId} layout="panel" onGone={closeInspector} />, {
      width: INSPECTOR_WIDTH,
      onClose: closeInspector,
    });
    return () => closePanel();
  }, [visible, inspectedId, openPanel, closePanel, closeInspector]);

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
  const filter = filters[board.id] ?? EMPTY_FILTER;
  const view: TaskView = boardViews[board.id] ?? "board";
  const shown = columnsOf(board, tasks, filter).reduce((sum, column) => sum + column.tasks.length, 0);

  return (
    <div className="flex h-full min-h-0 flex-col">
      <header className="flex h-14 shrink-0 items-center gap-5 border-b border-border-subtle px-4">
        <div className="flex min-w-0 flex-col">
          <h2 className="min-w-0 truncate text-title-16 text-text-primary">{board.name}</h2>
          {/* tasks:admin and UI-only: an agent can trigger the command and can
              never choose it. With no UI at all it was unreachable rather than
              protected, which is not the same thing. */}
          <button
            type="button"
            className="w-fit max-w-80 truncate text-left text-caption-11 text-text-tertiary transition-colors duration-(--motion-fast) hover:text-text-primary"
            title={
              board.verify
                ? `DevKit runs ${board.verify.command} and the exit code decides`
                : "Choose a command to close tasks with"
            }
            onClick={() => setEditingVerify(board)}
          >
            {board.verify ? (
              <>
                closes on <span className="font-mono">{board.verify.command}</span>
              </>
            ) : (
              "closing asks every time"
            )}
          </button>
        </div>
        <BoardSummaryBar summary={boardSummary(board, tasks)} />
        <div className="flex-1" />
        <ToggleGroup
          type="single"
          size="sm"
          variant="outline"
          value={view}
          onValueChange={(next) => {
            if (next === "board" || next === "list") setBoardView(board.id, next);
          }}
          aria-label="View"
        >
          <ToggleGroupItem value="board" aria-label="Board view">
            <KanbanSquare aria-hidden />
          </ToggleGroupItem>
          <ToggleGroupItem value="list" aria-label="List view">
            <Rows3 aria-hidden />
          </ToggleGroupItem>
        </ToggleGroup>
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
            <Workflow aria-hidden />
            From plan
          </Button>
        ) : null}
        {/* A dialog for the title, not a card called "New task" that then
            needs renaming — every one of those was a task nobody renamed. */}
        <Button size="sm" onClick={() => setCreating(true)}>
          <Plus aria-hidden />
          New task
        </Button>
        <Button
          variant="ghost"
          size="icon-sm"
          aria-label="Reload"
          onClick={() => void ensureBoardTasks(board.id)}
        >
          <RefreshCw aria-hidden />
        </Button>
      </header>
      <BoardFilters
        filter={filter}
        labels={boardLabels(tasks)}
        shown={shown}
        onChange={(next) => setFilters((current) => ({ ...current, [board.id]: next }))}
      />
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
      <NewTaskDialog
        boardName={creating ? board.name : null}
        onClose={() => setCreating(false)}
        onCreate={async (title) => {
          const created = await createTask({ boardId: board.id, title });
          useTasksStore.getState().upsertTask(created);
        }}
      />
      {error ? <p className="px-3 py-1 text-body-12 text-status-danger">{error}</p> : null}
      <BoardColumns
        board={board}
        tasks={tasks}
        filter={filter}
        view={view}
        inspectedId={inspectedId}
        onInspect={(task) =>
          // A second click on the card being shown closes the pane: the card
          // is the toggle, the way a row in a list-with-inspector behaves.
          setInspected(task.id === inspectedId ? null : { boardId: board.id, taskId: task.id })
        }
      />
    </div>
  );
}
