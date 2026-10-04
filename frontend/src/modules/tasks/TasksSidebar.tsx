import { useEffect, useState, type ReactNode } from "react";
import { toast } from "sonner";
import {
  Archive,
  ArrowRightLeft,
  Boxes,
  Columns2,
  Copy,
  ExternalLink,
  KanbanSquare,
  Link,
  MoreHorizontal,
  Plus,
  Settings2,
  SquarePlus,
  Unlink,
  Workflow,
} from "lucide-react";
import { useShallow } from "zustand/react/shallow";
import { errorMessage } from "@/bridge/errors";
import { copyText } from "@/bridge/clipboard";
import {
  archiveTask,
  bindBoard,
  createBoard,
  createTask,
  createTasksFromPlan,
  setBoardVerify,
  unbindBoard,
  type Board,
  type Progress,
  type Task,
} from "@/bridge/tasks";
import type { Workspace } from "@/bridge/workspace";
import { ActionTooltip } from "@/components/ui/action-tooltip";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import { SidebarHeader, SidebarSearch } from "@/components/ui/sidebar-shell";
import { ContextMenu, ContextMenuTrigger } from "@/components/ui/context-menu";
import { DropdownMenu, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { RowContextMenuContent, RowDropdownMenuContent, type RowAction } from "@/components/row-actions";
import { TreeRowAction, TreeRowTag, TreeView, type TreeNodeData } from "@/components/tree-view";
import { moduleTabs, useWorkbenchTabsStore } from "@/stores/workbenchTabs";
import { useWorkspaceSwitcherStore } from "@/stores/workspaceSwitcher";
import { BoardVerifyDialog } from "./BoardVerifyDialog";
import { NewBoardDialog, NewTaskDialog } from "./CreateDialogs";
import { activeDocument, MODULE, openBoard, openTaskDetail } from "./documents";
import { useTasksStore } from "./tasksStore";
import { buildTasksTree } from "./tasksTree";
import {
  boardActions,
  boardOfTask,
  isUrgentPriority,
  newBoardBlockedReason,
  newTaskTarget,
  sidebarBody,
  taskActions,
  workspaceActions,
  type SidebarAction,
  type SidebarActionId,
} from "./tasksSidebarActions";

/** A task tab's title: the key is what people say out loud, the title is what they read. */
function taskTabTitle(task: Task): string {
  return `${task.key} ${task.title}`.trim();
}

/** The glyph each action draws. Kept beside the component, not in the pure helpers — an icon is UI. */
const ACTION_ICONS: Partial<Record<SidebarActionId, ReactNode>> = {
  "workspace.newBoard": <SquarePlus />,
  "board.open": <ExternalLink />,
  "board.newTask": <Plus />,
  "board.verify": <Settings2 />,
  "board.fromPlan": <Workflow />,
  "board.linkCurrent": <Link />,
  "board.unlink": <Unlink />,
  "task.open": <ExternalLink />,
  "task.openBeside": <Columns2 />,
  "task.copyKey": <Copy />,
  "task.archive": <Archive />,
};

/**
 * Turns the pure action list into the shared menu rows. `run` gets the id and,
 * for an action aimed elsewhere (a workspace to link to), its target.
 */
function toRowActions(
  actions: SidebarAction[],
  run: (id: SidebarActionId, target?: string) => void,
): RowAction[] {
  return actions.map((action): RowAction => {
    if (action.kind === "separator") return action;
    if (action.kind === "submenu") {
      return {
        kind: "submenu",
        icon: <ArrowRightLeft />,
        label: action.label,
        items: toRowActions(action.items, run),
      };
    }
    return {
      kind: "item",
      icon: action.target ? <Boxes /> : ACTION_ICONS[action.id],
      label: action.label,
      disabled: action.disabled,
      onSelect: () => run(action.id, action.target),
    };
  });
}

/**
 * The same actions, offered twice — the row's hover button and its right-click
 * menu — from one list, which is the reason RowAction exists (row-actions.tsx).
 */
function withMenus(node: TreeNodeData, name: string, actions: RowAction[]): TreeNodeData {
  return {
    ...node,
    actions: (
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <ActionTooltip label={`Actions for ${name}`}>
            <TreeRowAction aria-label={`Actions for ${name}`}>
              <MoreHorizontal />
            </TreeRowAction>
          </ActionTooltip>
        </DropdownMenuTrigger>
        <RowDropdownMenuContent actions={actions} />
      </DropdownMenu>
    ),
    wrap: (row) => (
      <ContextMenu key={node.id}>
        <ContextMenuTrigger asChild>{row}</ContextMenuTrigger>
        <RowContextMenuContent actions={actions} />
      </ContextMenu>
    ),
  };
}

/** A task's marks: who holds it, how urgent it is, how far its subtasks are. */
function taskTrailing(task: Task, progress: Progress | null): ReactNode {
  if (!task.sessionId && !task.priority && !progress) return null;
  return (
    <>
      {task.sessionId ? (
        // The same mark the board's card draws. A dot, not a name: which
        // session holds it is in the task's own view, and a rail row has no
        // room for a session title.
        <ActionTooltip label="Held by an agent">
          <span className="size-1.5 rounded-full bg-status-success" />
        </ActionTooltip>
      ) : null}
      {task.priority ? (
        <TreeRowTag danger={isUrgentPriority(task.priority)}>{task.priority}</TreeRowTag>
      ) : null}
      {progress ? (
        // Counted by category (progressOf), so a board that renamed its Done
        // column still counts.
        <span className="tabular-nums">
          {progress.done}/{progress.total}
        </span>
      ) : null}
    </>
  );
}

/**
 * The Tasks rail: workspace → board → status → task, one tree, the way the
 * Database rail is folder → connection → schema → table.
 *
 * A tree rather than the board view's dropdown because the question a board
 * is picked to answer — "what is there to do in this project" — is a
 * question about the workspace, and the dropdown listed every board on every
 * machine this vault has synced to. Grouped by workspace, the current one on
 * top and open, the rest folded; a board synced from another machine and
 * linked to nothing here waits in its own group at the bottom, with the
 * gesture that links it on its row.
 *
 * Data lives in tasksStore (fetching, persistence of fold state) and shape in
 * tasksTree (ordering, search); what a row's menu offers is decided in
 * tasksSidebarActions. This file draws, and turns clicks into bridge calls.
 */
export function TasksSidebar() {
  const {
    boards,
    workspaces,
    tasksByBoard,
    loadingBoards,
    error,
    expandedWorkspaces,
    expandedBoards,
    expandedStatuses,
    expandedTasks,
    toggleWorkspace,
    toggleBoard,
    toggleStatus,
    toggleTask,
    refreshBoards,
    ensureBoardTasks,
    refreshBoard,
    subscribeTasksChanged,
  } = useTasksStore(
    useShallow((s) => ({
      boards: s.boards,
      workspaces: s.workspaces,
      tasksByBoard: s.tasksByBoard,
      loadingBoards: s.loadingBoards,
      error: s.error,
      expandedWorkspaces: s.expandedWorkspaces,
      expandedBoards: s.expandedBoards,
      expandedStatuses: s.expandedStatuses,
      expandedTasks: s.expandedTasks,
      toggleWorkspace: s.toggleWorkspace,
      toggleBoard: s.toggleBoard,
      toggleStatus: s.toggleStatus,
      toggleTask: s.toggleTask,
      refreshBoards: s.refreshBoards,
      ensureBoardTasks: s.ensureBoardTasks,
      refreshBoard: s.refreshBoard,
      subscribeTasksChanged: s.subscribeTasksChanged,
    })),
  );
  const currentWorkspaceId = useWorkspaceSwitcherStore((s) => s.currentId);
  // The document in front, so the row that looks selected is the one it
  // belongs to — clicking a tab in the strip moves the highlight too.
  const active = useWorkbenchTabsStore(
    useShallow((s) => activeDocument(moduleTabs(s.modules, MODULE))),
  );

  // The rail loads what the rail shows — the same reasoning DatabaseSidebar
  // gives: it mounts once for the module and stays mounted while collapsed,
  // so this is one read, not one per board tab the window restored. And it
  // listens for as long as it is mounted: an agent closing a task over MCP is
  // a change nobody in this window made, and polling for it would be a timer
  // running all day to notice something that already announces itself.
  useEffect(() => {
    void refreshBoards();
    return subscribeTasksChanged();
  }, [refreshBoards, subscribeTasksChanged]);

  // The store records why a read failed; said once, where it happened.
  useEffect(() => {
    if (error) toast.error("Could not load tasks", { description: error });
  }, [error]);

  const [filterQuery, setFilterQuery] = useState("");
  // The last board or task row clicked — the fallback "New task" aims at when
  // the tab in front is neither a board nor a task we can place.
  const [lastBoardId, setLastBoardId] = useState("");
  // Dialogs. Each non-null is open, and names what it is for.
  const [newBoardIn, setNewBoardIn] = useState<Workspace | null>(null);
  const [newTaskOn, setNewTaskOn] = useState<Board | null>(null);
  const [editingVerify, setEditingVerify] = useState<Board | null>(null);

  const needle = filterQuery.trim();
  const currentWorkspace = workspaces?.find((w) => w.id === currentWorkspaceId) ?? null;
  const newBoardReason = newBoardBlockedReason(currentWorkspaceId, workspaces);
  const taskTarget = newTaskTarget(boards, [
    active.boardId,
    boardOfTask(tasksByBoard, active.taskId),
    lastBoardId,
  ]);

  /** A bridge call from a menu: its failure is a toast, since no form is open to hold it. */
  async function attempt(what: string, call: () => Promise<unknown>): Promise<void> {
    try {
      await call();
    } catch (e) {
      toast.error(what, { description: errorMessage(e) });
    }
  }

  function runBoardAction(board: Board, id: SidebarActionId, target?: string) {
    switch (id) {
      case "board.open":
        openBoard(board.id, board.name, { preview: false });
        return;
      case "board.newTask":
        setNewTaskOn(board);
        return;
      case "board.verify":
        setEditingVerify(board);
        return;
      case "board.fromPlan":
        void attempt("Could not create tasks from the plan", async () => {
          const created = await createTasksFromPlan(board.id);
          // Nothing created is the normal answer on a second run, not a
          // failure — say which it was.
          if (created.length === 0) toast.info("Every node of the plan already has a task.");
          else toast.success(`${created.length} task${created.length === 1 ? "" : "s"} created from the plan`);
          await ensureBoardTasks(board.id);
        });
        return;
      case "board.linkCurrent":
        if (!currentWorkspaceId) return;
        void attempt("Could not link the board", async () => {
          await bindBoard(board.id, currentWorkspaceId);
          await refreshBoards();
        });
        return;
      case "board.linkTo":
        if (!target) return;
        void attempt("Could not link the board", async () => {
          await bindBoard(board.id, target);
          await refreshBoards();
        });
        return;
      case "board.unlink":
        // No confirmation: unlinking touches no task and is undone from the
        // same menu one group down. What it changes is who can see the board —
        // agents stop, which is the point of doing it.
        void attempt("Could not unlink the board", async () => {
          await unbindBoard(board.id);
          await refreshBoards();
        });
        return;
    }
  }

  function runTaskAction(task: Task, id: SidebarActionId) {
    switch (id) {
      case "task.open":
        openTaskDetail(task.id, taskTabTitle(task), { preview: false });
        return;
      case "task.openBeside":
        openTaskDetail(task.id, taskTabTitle(task), { preview: false, beside: true });
        return;
      case "task.copyKey":
        void attempt("Could not copy the key", () => copyText(task.key));
        return;
      case "task.archive":
        void attempt(`Could not archive ${task.key}`, async () => {
          await archiveTask(task.id);
          // Said, because the row it was on is gone the moment this lands and
          // a list that shrinks by one is easy to miss.
          toast.success(`${task.key} archived`);
          await refreshBoard(task.boardId);
        });
        return;
    }
  }

  const tree = buildTasksTree({
    boards,
    workspaces,
    currentWorkspaceId,
    tasksByBoard,
    loadingBoards,
    expandedWorkspaces,
    expandedBoards,
    expandedStatuses,
    expandedTasks,
    needle,
    selectedBoardId: active.boardId,
    selectedTaskId: active.taskId,
    callbacks: {
      toggleWorkspace,
      toggleBoard,
      toggleStatus,
      toggleTask,
      // Single click previews, double click keeps — the tab gesture every
      // other rail in the app uses, so a board glanced at while scanning the
      // tree does not leave a tab behind for each one.
      selectBoard: (board) => {
        setLastBoardId(board.id);
        openBoard(board.id, board.name);
      },
      activateBoard: (board) => openBoard(board.id, board.name, { preview: false }),
      selectTask: (task) => {
        setLastBoardId(task.boardId);
        openTaskDetail(task.id, taskTabTitle(task));
      },
      activateTask: (task) => openTaskDetail(task.id, taskTabTitle(task), { preview: false }),
    },
    decorate: {
      workspace: (workspace, node) =>
        withMenus(
          // The title bar's workspace mark, so the row reads as the thing the
          // switcher picks rather than as one more folder.
          { ...node, icon: <Boxes />, title: workspace.root },
          workspace.name,
          toRowActions(workspaceActions(), () => setNewBoardIn(workspace)),
        ),
      unlinkedGroup: (node) => ({
        ...node,
        icon: <Unlink />,
        title: "Boards no agent can see until they are linked to a workspace on this machine",
      }),
      board: (board, node) =>
        withMenus(
          { ...node, icon: <KanbanSquare /> },
          board.name,
          toRowActions(boardActions(board, { currentWorkspaceId, workspaces }), (id, target) =>
            runBoardAction(board, id, target),
          ),
        ),
      task: (task, node, progress) =>
        withMenus(
          {
            ...node,
            // The key in the code face and a step quieter: it is an id, read
            // to tell two tasks apart, and the title is what the row is for.
            label: (
              <>
                <span className="font-mono text-code-11 text-text-tertiary">{task.key}</span>{" "}
                {task.title}
              </>
            ),
            title: taskTabTitle(task),
            trailing: taskTrailing(task, progress),
          },
          task.key,
          toRowActions(taskActions(task), (id) => runTaskAction(task, id)),
        ),
    },
  });

  const body = sidebarBody({ boards, workspaces, treeLength: tree.length, needle });

  // Also off until the list has answered: the dialog is titled with the
  // workspace's name, and before the read lands there is only its id.
  const canCreateBoard = !newBoardReason && !!currentWorkspace;
  function openNewBoard() {
    if (currentWorkspace) setNewBoardIn(currentWorkspace);
  }

  const newBoardButton = (
    <ActionTooltip label={newBoardReason || `New board in ${currentWorkspace?.name ?? "this workspace"}`} wrap>
      <Button variant="ghost" size="icon-xs" disabled={!canCreateBoard} onClick={openNewBoard}>
        <SquarePlus className="size-3" />
      </Button>
    </ActionTooltip>
  );

  return (
    <div className="flex h-full min-h-0 flex-col select-none">
      <SidebarHeader>
        <SidebarSearch
          value={filterQuery}
          onChange={setFilterQuery}
          onClear={() => setFilterQuery("")}
          placeholder="Search tasks…"
        />
        {newBoardButton}
        <ActionTooltip label={taskTarget ? `New task on ${taskTarget.name}` : "Select a board first"} wrap>
          <Button
            variant="ghost"
            size="icon-xs"
            disabled={!taskTarget}
            onClick={() => taskTarget && setNewTaskOn(taskTarget)}
          >
            <Plus className="size-3" />
          </Button>
        </ActionTooltip>
      </SidebarHeader>

      {/* Nothing at all until the first read answers: "No boards yet" that
          flashes and is replaced by a tree is a claim the rail takes back. */}
      {body === "hidden" ? null : body === "empty" ? (
        <div className="min-h-0 flex-1">
          <EmptyState
            size="rail"
            title="No boards yet"
            body={
              "A board holds the work an agent can pick up, and it lives in a workspace — only agents working there can see it." +
              (newBoardReason ? " Pick or create a workspace in the title bar first." : "")
            }
            action={{ label: "New board", icon: SquarePlus, onClick: openNewBoard, disabled: !canCreateBoard }}
          />
        </div>
      ) : body === "noMatch" ? (
        <div className="min-h-0 flex-1">
          {/* Only boards already opened are searched inside — the rest match
              on their name alone (tasksTree.ts), which is why this says what
              it looked for and not "no such task". */}
          <EmptyState size="rail" body={`Nothing matches “${needle}”.`} />
        </div>
      ) : (
        <TreeView nodes={tree} className="flex-1 scrollbar-gutter-stable" />
      )}

      <NewBoardDialog
        workspaceName={newBoardIn?.name ?? null}
        onClose={() => setNewBoardIn(null)}
        onCreate={async (name, keyPrefix) => {
          const workspaceId = newBoardIn?.id ?? "";
          // Bound in the same call, so the board is never briefly unlinked
          // and never briefly invisible to the agents working here. "blue"
          // is the board view's own default; colour has no editor yet.
          const board = await createBoard(name, keyPrefix, "blue", workspaceId);
          await refreshBoards();
          setLastBoardId(board.id);
          openBoard(board.id, board.name, { preview: false });
        }}
      />

      <NewTaskDialog
        boardName={newTaskOn?.name ?? null}
        onClose={() => setNewTaskOn(null)}
        onCreate={async (title) => {
          const board = newTaskOn;
          if (!board) return;
          const task = await createTask({ boardId: board.id, title });
          await ensureBoardTasks(board.id);
          // Kept rather than previewed: a task just made is one about to be
          // written into, and the first keystroke would pin it anyway.
          openTaskDetail(task.id, taskTabTitle(task), { preview: false });
        }}
      />

      {/* Mounted only while open: it reads the workspace's services on mount,
          and a closed dialog in an always-mounted rail would do that for
          nobody every time the rail came up. */}
      {editingVerify ? (
        <BoardVerifyDialog
          board={editingVerify}
          onClose={() => setEditingVerify(null)}
          onSave={(command, timeoutSec, service) => {
            const board = editingVerify;
            void attempt("Could not save the verify command", async () => {
              await setBoardVerify(board.id, command, timeoutSec, service);
              setEditingVerify(null);
              await refreshBoards();
            });
          }}
        />
      ) : null}
    </div>
  );
}
