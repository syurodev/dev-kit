import { create } from "zustand";
import { persist } from "zustand/middleware";
import { uiStateStorage } from "@/bridge/uistate";
import { errorMessage } from "@/bridge/errors";
import { listBoards, listTasks, onTasksChanged, type Board, type Task } from "@/bridge/tasks";
import { listWorkspaces, type Workspace } from "@/bridge/workspace";
import { createBoardChangeDebouncer } from "./tasksTree";

/**
 * Data and fold state for the Tasks sidebar tree (tasksTree.ts builds the rows
 * from it; TasksSidebar, Task 10, renders them). Split from the tree builder
 * the way databaseStore.ts is split from databaseTree.ts: this file owns the
 * bridge and persistence, that one owns shape and search, and neither needs
 * the other's tests to change when it changes.
 */
export type TasksStoreState = {
  boards: Board[] | null;
  workspaces: Workspace[] | null;
  /**
   * A board's tasks, present only once it has been asked for — a board can
   * hold thousands of tasks over its life, and `List` is bounded per board on
   * purpose, so nothing here fetches every board's tasks just because the
   * tree mounted.
   */
  tasksByBoard: Record<string, Task[] | undefined>;
  /** A board whose tasks are being (re)fetched right now. */
  loadingBoards: Record<string, boolean>;
  error: string;

  // Fold state — the only thing this store persists (partialize, below).
  expandedWorkspaces: Record<string, boolean>;
  expandedBoards: Record<string, boolean>;
  /** Keyed `${boardId}:${statusId}`: a status id is only unique within its board. */
  expandedStatuses: Record<string, boolean>;
  expandedTasks: Record<string, boolean>;

  /**
   * Flips one row's fold state. Each takes the row's OWN `expanded` value —
   * tasksTree.ts hands it back in the `onToggle` closure it builds, because
   * the default differs per row (the current workspace opens by default,
   * others don't; a terminal status starts closed, others don't) and working
   * that out a second time here would be a rule that can drift from the one
   * the tree actually draws.
   */
  toggleWorkspace: (id: string, expanded: boolean) => void;
  toggleBoard: (boardId: string, expanded: boolean) => void;
  toggleStatus: (boardId: string, statusId: string, expanded: boolean) => void;
  toggleTask: (taskId: string, expanded: boolean) => void;

  /** Boards and workspaces, fresh. Called once when the sidebar mounts, and
   * again on a board-level `tasks:changed` (create, update, delete, bind,
   * unbind — anything that can change the list or a board's own fields). */
  refreshBoards: () => Promise<void>;
  /** Loads a board's tasks the first time, or re-lists them if it is already
   * loaded — the lazy fetch AND the "it changed, read it again" path share
   * one function because they are the same request either way. */
  ensureBoardTasks: (boardId: string) => Promise<void>;
  /** Re-lists a board's tasks, but only if something has already asked for
   * them — a `tasks:changed` for a board nobody has opened is not worth a
   * round trip nobody will see. */
  refreshBoard: (boardId: string) => Promise<void>;

  /**
   * Subscribes to `tasks:changed` for as long as the sidebar is mounted.
   * Bursts are coalesced per board (createBoardChangeDebouncer, tasksTree.ts)
   * so an agent moving several subtasks in a row costs one refresh, not one
   * per move. Returns the unsubscribe function; call it from the mounting
   * component's cleanup.
   */
  subscribeTasksChanged: () => () => void;
};

export const useTasksStore = create<TasksStoreState>()(
  persist(
    (set, get) => ({
      boards: null,
      workspaces: null,
      tasksByBoard: {},
      loadingBoards: {},
      error: "",

      expandedWorkspaces: {},
      expandedBoards: {},
      expandedStatuses: {},
      expandedTasks: {},

      toggleWorkspace: (id, expanded) =>
        set((state) => ({ expandedWorkspaces: { ...state.expandedWorkspaces, [id]: !expanded } })),

      toggleBoard: (boardId, expanded) => {
        set((state) => ({ expandedBoards: { ...state.expandedBoards, [boardId]: !expanded } }));
        // Opening it is what fetches it — closing never discards what is
        // already loaded, so re-opening a board never costs a second trip.
        if (!expanded) void get().ensureBoardTasks(boardId);
      },

      toggleStatus: (boardId, statusId, expanded) =>
        set((state) => ({
          expandedStatuses: {
            ...state.expandedStatuses,
            [`${boardId}:${statusId}`]: !expanded,
          },
        })),

      toggleTask: (taskId, expanded) =>
        set((state) => ({ expandedTasks: { ...state.expandedTasks, [taskId]: !expanded } })),

      refreshBoards: async () => {
        try {
          const [boards, workspaces] = await Promise.all([listBoards(), listWorkspaces()]);
          set({ boards, workspaces, error: "" });
        } catch (e) {
          set({ error: errorMessage(e) });
        }
      },

      ensureBoardTasks: async (boardId) => {
        set((state) => ({ loadingBoards: { ...state.loadingBoards, [boardId]: true } }));
        try {
          const tasks = await listTasks(boardId);
          set((state) => ({
            tasksByBoard: { ...state.tasksByBoard, [boardId]: tasks },
            loadingBoards: { ...state.loadingBoards, [boardId]: false },
            error: "",
          }));
        } catch (e) {
          set((state) => ({
            loadingBoards: { ...state.loadingBoards, [boardId]: false },
            error: errorMessage(e),
          }));
        }
      },

      refreshBoard: async (boardId) => {
        if (get().tasksByBoard[boardId] === undefined) return;
        await get().ensureBoardTasks(boardId);
      },

      subscribeTasksChanged: () => {
        // A board-level event (taskId === "") may have changed the board's own
        // fields (name, verify, workspaceId) or the list itself, so it earns a
        // refreshBoards() on top of the board's own re-list — recorded here and
        // read back when the debounced fire lands, rather than fired at once,
        // so a burst that mixes board- and task-level events still costs one
        // trip of each kind rather than one pair per event.
        const boardLevel = new Set<string>();
        const debouncer = createBoardChangeDebouncer((boardId) => {
          void get().refreshBoard(boardId);
          if (boardLevel.delete(boardId)) {
            void get().refreshBoards();
          }
        }, 150);
        return onTasksChanged(({ boardId, taskId }) => {
          if (taskId === "") boardLevel.add(boardId);
          debouncer.trigger(boardId);
        });
      },
    }),
    {
      name: "tasksSidebar",
      storage: uiStateStorage,
      // Only fold state persists. Boards, workspaces and tasks all come from
      // the bridge — caching them here would let a stale copy outlive the
      // vault item it describes, the same reasoning databaseStore.ts's
      // partialize gives for not persisting profiles or the schema snapshot.
      partialize: (state) => ({
        expandedWorkspaces: state.expandedWorkspaces,
        expandedBoards: state.expandedBoards,
        expandedStatuses: state.expandedStatuses,
        expandedTasks: state.expandedTasks,
      }),
    },
  ),
);
