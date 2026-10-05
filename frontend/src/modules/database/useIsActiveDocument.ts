import { focusedGroup } from "@/stores/documentGroups";
import { useModuleVisibility } from "@/components/ui/module-visibility";
import { tabKey, useWorkbenchTabsStore, type WorkbenchTabsState } from "@/stores/workbenchTabs";

/**
 * Whether this document is the module's active tab.
 *
 * Two things need it. The overlays are `position: fixed` and driven by shared
 * store booleans, so every mounted document would draw its own copy — three
 * console tabs would stack three backdrops. And Live activity polls while
 * mounted, which a background tab still is, so being on screen is what should
 * gate a request rather than being alive.
 *
 * Identity is the tab key, not the view id: two consoles are the same view and
 * differ only by params.
 */
export function isActiveDocument(state: WorkbenchTabsState, viewId: string, params?: Record<string, unknown>): boolean {
  const group = focusedGroup(state);
  return group?.moduleId === "database" && group.activeTabId === tabKey("database", viewId, params);
}

export function useIsActiveDocument(viewId: string, params?: Record<string, unknown>): boolean {
  const visible = useModuleVisibility();
  const active = useWorkbenchTabsStore((state) => isActiveDocument(state, viewId, params));
  return visible && active;
}
