import { getModule, viewIcon } from "@/modules/registry";
import { useWorkbenchTabsStore } from "@/stores/workbenchTabs";

/** Tasks' documents, each a tab — the board, and one per task opened. */
export const MODULE = "tasks";
export const BOARD_VIEW = "tasks.board";
export const DETAIL_VIEW = "tasks.detail";

type OpenOptions = { preview?: boolean; beside?: boolean };

export function openTaskDetail(taskId: string, title: string, options: OpenOptions = { preview: true }): string {
  // Start the chunk in the click, not in the render that needs it.
  void getModule(MODULE)?.views.find((v) => v.id === DETAIL_VIEW)?.preload?.().catch(() => undefined);
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
