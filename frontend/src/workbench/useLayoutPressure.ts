import { usePanelWidthsStore } from "@/stores/panelWidths";
import { useSidebarOpenStore } from "@/stores/sidebarShell";
import { useWorkbenchTabsStore } from "@/stores/workbenchTabs";
import { layoutPressure, useWindowWidth, type LayoutPressure } from "./panelLayout";

/**
 * What gives way right now, for the window as it is (panelLayout.layoutPressure).
 * Read by the three surfaces that give way: the sidebar, the columns and the
 * document groups. Each derives the same answer from the same inputs, so none
 * of them has to tell the others.
 */
export function useLayoutPressure(): LayoutPressure {
  const windowWidth = useWindowWidth();
  const sidebarOpen = useSidebarOpenStore((s) => s.open);
  const sidebarWidth = usePanelWidthsStore((s) => s.sidebarWidth);
  const columns = usePanelWidthsStore((s) => s.visibleColumns);
  const groups = useWorkbenchTabsStore((s) => Math.max(1, s.groups.length));
  return layoutPressure({ windowWidth, sidebarOpen, sidebarWidth, columns, groups });
}
