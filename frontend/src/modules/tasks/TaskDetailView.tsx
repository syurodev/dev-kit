import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useShallow } from "zustand/react/shallow";
import {
  Archive,
  ArchiveRestore,
  Copy,
  Ellipsis,
  Hourglass,
  KanbanSquare,
  ListChecks,
  Trash2,
} from "lucide-react";
import { toast } from "sonner";
import { getTask, listComments, onTasksChanged, verifyRuns, type Board, type Task } from "@/bridge/tasks";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { Chip } from "@/components/ui/chip";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { EmptyState } from "@/components/ui/empty-state";
import { EMPTY_ARRAY } from "@/lib/stableEmpty";
import { tabKey, useWorkbenchTabsStore } from "@/stores/workbenchTabs";
import { DETAIL_VIEW, MODULE, openBoard } from "./documents";
import { AgentHeld } from "./TaskBits";
import { archive, remove, taskKeys, unarchive } from "./taskActions";
import { childrenOf, indexById, openBlockers } from "./taskModel";
import { createBoardChangeDebouncer } from "./tasksTree";
import { useTasksStore } from "./tasksStore";
import { TaskActivity } from "./detail/TaskActivity";
import { TaskProperties } from "./detail/TaskProperties";
import { TaskRuns, isLive } from "./detail/TaskRuns";
import { TaskSubtasks } from "./detail/TaskSubtasks";
import { TaskDescription, TaskTitle } from "./detail/TaskText";

/** How often a run in flight is re-read. The run is DevKit's own process, so
 * this is a local read, and it stops the moment nothing is queued or running. */
const LIVE_RUN_POLL_MS = 1000;

function TaskActions({ task, board }: { task: Task; board: Board }) {
  const [confirmDelete, setConfirmDelete] = useState(false);
  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button size="icon-sm" variant="ghost" aria-label="Task actions">
            <Ellipsis aria-hidden />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          <DropdownMenuItem
            icon={<Copy aria-hidden />}
            onSelect={() => {
              void navigator.clipboard.writeText(task.key).then(() => toast.success(`Copied ${task.key}`));
            }}
          >
            Copy key
          </DropdownMenuItem>
          <DropdownMenuItem
            icon={<KanbanSquare aria-hidden />}
            onSelect={() => openBoard(board.id, board.name, { preview: false })}
          >
            Open board
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          {task.archivedAt ? (
            <DropdownMenuItem onSelect={() => void unarchive(task)} icon={<ArchiveRestore aria-hidden />}>
              Restore to board
            </DropdownMenuItem>
          ) : (
            <DropdownMenuItem onSelect={() => void archive(task)} icon={<Archive aria-hidden />}>
              Archive
            </DropdownMenuItem>
          )}
          <DropdownMenuItem
            variant="destructive"
            icon={<Trash2 aria-hidden />}
            onSelect={() => setConfirmDelete(true)}
          >
            Delete…
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
      <AlertDialog open={confirmDelete} onOpenChange={setConfirmDelete}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete {task.key}?</AlertDialogTitle>
            <AlertDialogDescription>
              The task and its comments are removed on every device that syncs this vault. Archive it
              instead to keep the record.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              onClick={() => {
                void remove(task).then((removed) => {
                  if (removed) {
                    useWorkbenchTabsStore
                      .getState()
                      .closeTab(tabKey(MODULE, DETAIL_VIEW, { taskId: task.id }));
                  }
                });
              }}
            >
              Delete
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}

/**
 * One task: its text, its properties, its subtasks and dependencies, its
 * verify runs, and what was said and done on it — every field editable here,
 * each write optimistic (taskActions.ts).
 *
 * Reads are TanStack queries: a task tab opened before shows what it had in
 * the first frame and re-reads underneath (stale-while-revalidate,
 * smoothness 2.4), and a `tasks:changed` for this task is what re-reads it.
 * The board's workflow and its other tasks come from tasksStore — the same
 * summaries the board and the rail already hold — so the pickers and the
 * dependency lists cost no call of their own.
 */
export function TaskDetailView({ params }: { params?: Record<string, unknown> }) {
  const taskId = typeof params?.taskId === "string" ? params.taskId : "";

  const taskQuery = useQuery({
    queryKey: taskKeys.task(taskId),
    queryFn: () => getTask(taskId),
    enabled: !!taskId,
    // Fresh on every visit: an agent may have written to it while the tab
    // was away, and the event that says so only reaches a mounted view.
    staleTime: 0,
  });
  const commentsQuery = useQuery({
    queryKey: taskKeys.comments(taskId),
    queryFn: () => listComments(taskId),
    enabled: !!taskId,
    staleTime: 0,
  });
  const runsQuery = useQuery({
    queryKey: taskKeys.runs(taskId),
    queryFn: () => verifyRuns(taskId),
    enabled: !!taskId,
    staleTime: 0,
    refetchInterval: (query) => (query.state.data?.some(isLive) ? LIVE_RUN_POLL_MS : false),
  });

  const task = taskQuery.data;
  const boardId = task?.boardId ?? "";
  const { boards, board, boardTasks, refreshBoards, ensureBoardTasks, refreshBoard } = useTasksStore(
    useShallow((s) => ({
      boards: s.boards,
      board: s.boards?.find((candidate) => candidate.id === boardId),
      boardTasks: s.tasksByBoard[boardId],
      refreshBoards: s.refreshBoards,
      ensureBoardTasks: s.ensureBoardTasks,
      refreshBoard: s.refreshBoard,
    })),
  );

  useEffect(() => {
    if (boards === null) void refreshBoards();
  }, [boards, refreshBoards]);

  const tasksLoaded = boardTasks !== undefined;
  useEffect(() => {
    if (boardId && !tasksLoaded) void ensureBoardTasks(boardId);
  }, [boardId, tasksLoaded, ensureBoardTasks]);

  // This task re-reads itself on its own event. Any other write on its board
  // (a blocker closing, a subtask added by an agent) re-lists the board's
  // summaries, coalesced the way the rail coalesces them.
  const { refetch: refetchTask } = taskQuery;
  const { refetch: refetchComments } = commentsQuery;
  const { refetch: refetchRuns } = runsQuery;
  useEffect(() => {
    if (!taskId) return;
    const debouncer = createBoardChangeDebouncer((id) => void refreshBoard(id), 150);
    const off = onTasksChanged((e) => {
      if (e.taskId === taskId) {
        void refetchTask();
        void refetchComments();
        void refetchRuns();
      }
      if (boardId && e.boardId === boardId) debouncer.trigger(boardId);
    });
    return off;
  }, [taskId, boardId, refreshBoard, refetchTask, refetchComments, refetchRuns]);

  if (!taskId) {
    return <EmptyState icon={ListChecks} title="No task open" body="Pick a task on the board." />;
  }
  if (!task) {
    if (taskQuery.isError) {
      return (
        <EmptyState
          icon={ListChecks}
          tone="danger"
          title="Task unavailable"
          body={taskQuery.error instanceof Error ? taskQuery.error.message : "It could not be read."}
        >
          <Button size="sm" variant="outline" onClick={() => void refetchTask()}>
            Try again
          </Button>
        </EmptyState>
      );
    }
    // Not loaded yet: a local read, so nothing is drawn rather than a
    // skeleton that would flash for a frame (smoothness §1).
    return null;
  }
  if (!board) {
    if (boards === null) return null;
    return (
      <EmptyState
        icon={KanbanSquare}
        title="Board unavailable"
        body="This task's board is not one this machine can show. Pick a board in the Tasks sidebar."
      />
    );
  }

  const siblings = boardTasks ?? EMPTY_ARRAY;
  const byId = indexById(siblings);
  const waitingOn = openBlockers(task, byId, board);
  const subtasks = task.parentId ? EMPTY_ARRAY : childrenOf(siblings, task.id);

  return (
    <div className="flex h-full min-h-0 flex-col">
      <header className="flex h-12 shrink-0 items-center gap-2 border-b border-border-subtle px-4">
        <button
          type="button"
          className="min-w-0 truncate text-body-12 text-text-tertiary transition-colors duration-(--motion-fast) hover:text-text-primary"
          onClick={() => openBoard(board.id, board.name, { preview: false })}
        >
          {board.name}
        </button>
        <span className="text-body-12 text-text-tertiary">/</span>
        <span className="shrink-0 text-code-12 text-text-secondary">{task.key}</span>
        {task.sessionId ? (
          <Chip className="gap-1">
            <AgentHeld className="size-(--icon-12)" />
            held by an agent
          </Chip>
        ) : null}
        {task.archivedAt ? <Chip>archived</Chip> : null}
        <div className="flex-1" />
        <TaskActions task={task} board={board} />
      </header>

      <div className="@container min-h-0 flex-1 overflow-auto scrollbar-gutter-stable">
        <div className="mx-auto flex max-w-300 flex-col gap-8 px-6 py-5 @3xl:flex-row">
          <main className="flex min-w-0 flex-1 flex-col gap-6">
            <div className="flex flex-col gap-3">
              <TaskTitle task={task} />
              {waitingOn.length > 0 ? (
                <div
                  role="status"
                  className="flex items-center gap-2 rounded-md border border-status-warning/30 bg-status-warning/8 px-3 py-2 text-body-12 text-status-warning"
                >
                  <Hourglass aria-hidden className="size-(--icon-14) shrink-0" />
                  <span className="min-w-0 truncate">
                    Waiting on {waitingOn.length === 1 ? "1 task" : `${waitingOn.length} tasks`}:{" "}
                    {waitingOn.map((blocker) => blocker.key).join(", ")}
                  </span>
                </div>
              ) : null}
              <TaskDescription task={task} />
            </div>
            {task.parentId ? null : <TaskSubtasks task={task} board={board} subtasks={subtasks} />}
            <TaskRuns runs={runsQuery.data ?? EMPTY_ARRAY} />
            <TaskActivity
              taskId={task.id}
              board={board}
              comments={commentsQuery.data ?? EMPTY_ARRAY}
              history={task.history ?? EMPTY_ARRAY}
            />
          </main>
          <aside className="w-full shrink-0 @3xl:w-64">
            <TaskProperties task={task} board={board} boardTasks={siblings} byId={byId} />
          </aside>
        </div>
      </div>
    </div>
  );
}
