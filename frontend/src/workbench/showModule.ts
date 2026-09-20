import { getModule } from "@/modules/registry";
import { moduleTabs, useWorkbenchTabsStore } from "@/stores/workbenchTabs";

/**
 * Brings a module on screen WITH its home view — for code that has just
 * pointed the module at something (a tool, a session, a topic) and needs the
 * person to see it.
 *
 * focusModule alone is not that. A module with nothing open deliberately opens
 * nothing (Workbench.focusModule says why) and shows its empty state, so a
 * tool picked from the palette would be selected behind "Tools has nothing
 * open". With no tab open this opens the home view, as that empty state's
 * button would — but never through the module's newTab, which for some modules
 * means "a new console", not "show me what is there".
 */
export function showModuleHome(moduleId: string): void {
  const module = getModule(moduleId);
  const home = module?.views[0];
  if (!module || !home) return;
  void home.preload?.()?.catch(() => undefined);
  const state = useWorkbenchTabsStore.getState();
  if (moduleTabs(state.modules, moduleId).openTabs.length === 0) {
    state.openTab({ moduleId, viewId: home.id, title: module.title, icon: home.icon });
  } else {
    state.focusModule(moduleId);
  }
}

/**
 * After a module is focused: opens its home view when the module asked for that
 * (ModuleManifest.opensHome) and has nothing open. Any other module keeps the
 * shell's default, which is to show its empty state and let the person choose.
 */
export function openHomeWhenAsked(moduleId: string): void {
  if (getModule(moduleId)?.opensHome) showModuleHome(moduleId);
}
