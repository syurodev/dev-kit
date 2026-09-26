import type { Board } from "@/bridge/tasks";

/**
 * The pure half of TasksBoardView's "which board" question — no React, no
 * bridge — split out the way tasksTree.ts is split from TasksSidebar, so it
 * is tested without a render (boardHome.test.ts).
 */

/**
 * The board a home tab shows (no `boardId` param — the module's `opensHome`
 * tab opens with none): the current workspace's boards, first by name. The
 * sidebar tree is where a board is normally picked; this is only what
 * "arrived at Tasks with nothing open" falls back to.
 *
 * Null with no current workspace, or when it has no board at all — the
 * caller shows an empty state pointing at "New board" in the sidebar, since
 * creation lives there now and the board view no longer offers its own.
 */
export function homeBoardOf(boards: Board[], workspaceId: string | null): Board | null {
  if (!workspaceId) return null;
  const inWorkspace = boards.filter((board) => board.workspaceId === workspaceId);
  if (inWorkspace.length === 0) return null;
  return [...inWorkspace].sort((a, b) => a.name.localeCompare(b.name))[0];
}

/**
 * Whether "From plan" belongs on this board's header. The plan is Code
 * Graph's, read from a service of the board's WORKSPACE — so a board with
 * nothing bound has no service that could have an approved plan open, same as
 * the reason `db.explain` and friends gate on a profile's own binding.
 *
 * Replaces the old `board.projectPath` gate (one board, one folder, on this
 * machine only): a board syncs and a path does not travel with it, while
 * `workspaceId` is the same per-machine binding `task_board_bindings` records
 * everywhere else in this module.
 */
export function canRunFromPlan(board: Pick<Board, "workspaceId">): boolean {
  return !!board.workspaceId;
}
