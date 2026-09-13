import { useEffect } from "react";
import { Events } from "@wailsio/runtime";
import { runCommand } from "@/modules/registry";
import { selectActiveModuleTabs, useWorkbenchTabsStore } from "@/stores/workbenchTabs";

/**
 * The native menu bar's end of the command bus.
 *
 * internal/appmenu builds the menu at startup — before any of this exists —
 * and emits nothing but the id of whatever was chosen. Everything about what
 * the command DOES lives with the module that declares it, which is what keeps
 * the menu from becoming a second, divergent description of the app.
 */
export const MENU_COMMAND_EVENT = "devkit:command";

/**
 * File ▸ Close Tab, ⌘W (appmenu.CloseTabID). The one command that is not a
 * module's: tabs belong to the shell.
 */
export const CLOSE_TAB_COMMAND = "workbench.tab.close";

/** Closes the tab in front of you. Nothing open, nothing closed. */
export function closeActiveTab(): void {
  const state = useWorkbenchTabsStore.getState();
  const { activeTabId } = selectActiveModuleTabs(state);
  if (activeTabId) state.closeTab(activeTabId);
}

export function useMenuCommands(): void {
  useEffect(() => {
    return Events.On(MENU_COMMAND_EVENT, (event) => {
      const id = Array.isArray(event.data) ? event.data[0] : event.data;
      if (id === CLOSE_TAB_COMMAND) {
        closeActiveTab();
        return;
      }
      if (typeof id === "string") {
        runCommand(id);
      }
    });
  }, []);
}
