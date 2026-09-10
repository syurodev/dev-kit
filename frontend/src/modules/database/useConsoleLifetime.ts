import { useEffect } from "react";
import { moduleTabs, useWorkbenchTabsStore } from "@/stores/workbenchTabs";
import { DEFAULT_CONSOLE_ID, useDatabaseStore } from "./databaseStore";

/**
 * The console id a Database tab owns.
 *
 * A console tab carries it directly; a table tab is identified by what it
 * shows, so its console id is derived from that. Both live in one function
 * because the cleanup below computes the id from a TAB while each document
 * computes it for itself, and two copies of that rule would drift into a leak.
 */
export function consoleIdOfTab(
  viewId: string,
  params?: Record<string, unknown>,
): string | null {
  if (viewId === "database.console") {
    return typeof params?.consoleId === "string" ? params.consoleId : DEFAULT_CONSOLE_ID;
  }
  if (viewId === "database.table") {
    return `table:${String(params?.schema ?? "")}.${String(params?.table ?? "")}`;
  }
  return null;
}

/**
 * Frees a console's slice once its tab is really gone.
 *
 * Unmounting is not closing: WorkbenchViews unmounts a module left alone long
 * enough, and that console has to come back with its SQL and its result. So the
 * slice is dropped only when no open tab still owns the id — otherwise a
 * console you merely navigated away from would lose 200 rows of result and
 * whatever you had typed.
 */
export function useConsoleLifetime(consoleId: string): void {
  const closeConsole = useDatabaseStore((s) => s.closeConsole);
  useEffect(() => {
    return () => {
      const open = moduleTabs(useWorkbenchTabsStore.getState().modules, "database").openTabs;
      const stillOpen = open.some(
        (tab) => consoleIdOfTab(tab.viewId, tab.params) === consoleId,
      );
      if (!stillOpen) {
        closeConsole(consoleId);
      }
    };
  }, [consoleId, closeConsole]);
}
