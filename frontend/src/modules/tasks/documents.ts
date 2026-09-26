import { getModule, viewIcon } from "@/modules/registry";
import { useWorkbenchTabsStore, type ModuleTabs } from "@/stores/workbenchTabs";

/** Tasks' documents, each a tab — one per board, and one per task opened. */
export const MODULE = "tasks";
export const BOARD_VIEW = "tasks.board";
export const DETAIL_VIEW = "tasks.detail";

type OpenOptions = { preview?: boolean; beside?: boolean };

/** Start a view's chunk in the click, not in the render that needs it. */
function preload(viewId: string): void {
  void getModule(MODULE)?.views.find((v) => v.id === viewId)?.preload?.().catch(() => undefined);
}

export function openTaskDetail(taskId: string, title: string, options: OpenOptions = { preview: true }): string {
  preload(DETAIL_VIEW);
  return useWorkbenchTabsStore.getState().openTab({
    moduleId: MODULE,
    viewId: DETAIL_VIEW,
    title,
    icon: viewIcon(MODULE, DETAIL_VIEW),
    params: { taskId },
    preview: options.preview ?? false,
    beside: options.beside,
  });
}

/**
 * A board, as its own tab — the same per-item shape as a task, so two boards
 * can sit side by side and the sidebar can say which one is in front. The
 * `boardId` param is what makes it per-board: tabKey folds params into the id,
 * so opening the same board twice focuses the first tab.
 */
export function openBoard(boardId: string, name: string, options: OpenOptions = { preview: true }): string {
  preload(BOARD_VIEW);
  return useWorkbenchTabsStore.getState().openTab({
    moduleId: MODULE,
    viewId: BOARD_VIEW,
    title: name,
    icon: viewIcon(MODULE, BOARD_VIEW),
    params: { boardId },
    preview: options.preview ?? false,
    beside: options.beside,
  });
}

/**
 * Which board or task the module's front tab is showing, from its params
 * alone — "" for either when the front tab is not that kind of document. The
 * sidebar highlights it and aims "New task" at it, so the row that looks
 * selected is the one the document in front belongs to.
 */
export function activeDocument(tabs: ModuleTabs): { boardId: string; taskId: string } {
  const active = tabs.openTabs.find((tab) => tab.id === tabs.activeTabId);
  const param = (key: string) => {
    const value = active?.params?.[key];
    return typeof value === "string" ? value : "";
  };
  return {
    boardId: active?.viewId === BOARD_VIEW ? param("boardId") : "",
    taskId: active?.viewId === DETAIL_VIEW ? param("taskId") : "",
  };
}
