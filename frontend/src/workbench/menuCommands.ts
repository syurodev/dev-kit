import { useEffect } from "react";
import { Events } from "@wailsio/runtime";
import { runCommand } from "@/modules/registry";
import { selectActiveModuleTabs, useWorkbenchTabsStore } from "@/stores/workbenchTabs";
import { detectPlatform, type Platform } from "@/lib/platform";

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

/**
 * Ctrl+W, on the one platform whose accelerator has nowhere else to come from.
 *
 * Everywhere else the menu carries it: appmenu's CloseTab sits in the menu bar
 * with ⌘W/Ctrl+W on it, and the key arrives here as a MENU_COMMAND_EVENT.
 * Linux has no menu bar to hang it on — see setsApplicationMenu in
 * native_webview_linux.go — so the shortcut is bound in the page instead.
 *
 * Linux only, deliberately: bound everywhere, it would run alongside the menu's
 * own accelerator and close two tabs for one keypress.
 */
export function isCloseTabShortcut(event: ShortcutKey, platform: Platform): boolean {
  if (platform !== "linux") return false;
  if (event.defaultPrevented || event.isComposing || event.repeat) return false;
  // Ctrl+Shift+W is Close Window, which is the window manager's, not ours.
  if (event.altKey || event.metaKey || event.shiftKey || !event.ctrlKey) return false;
  return event.key.toLowerCase() === "w";
}

type ShortcutKey = Pick<
  KeyboardEvent,
  "key" | "metaKey" | "ctrlKey" | "altKey" | "shiftKey" | "repeat" | "isComposing" | "defaultPrevented"
>;

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

  useEffect(() => {
    const platform = detectPlatform();
    if (platform !== "linux") return;
    const onKeyDown = (event: KeyboardEvent) => {
      if (!isCloseTabShortcut(event, platform)) return;
      event.preventDefault();
      closeActiveTab();
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, []);
}
