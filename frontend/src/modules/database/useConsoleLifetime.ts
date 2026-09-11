import { useEffect } from "react";
import { moduleTabs, useWorkbenchTabsStore } from "@/stores/workbenchTabs";
import { DEFAULT_CONSOLE_ID, useDatabaseStore } from "./databaseStore";

/**
 * The console id a Database tab owns.
 *
 * A console tab carries it directly; no other Database tab owns one. It lives
 * in one function because the cleanup below computes the id from a TAB while
 * each document computes it for itself, and two copies of that rule would
 * drift into a leak.
 */
export function consoleIdOfTab(
  viewId: string,
  params?: Record<string, unknown>,
): string | null {
  if (viewId === "database.console") {
    return typeof params?.consoleId === "string" ? params.consoleId : DEFAULT_CONSOLE_ID;
  }
  // A table tab used to be a console with pre-filled SQL. It is a data grid
  // now, and owns a BrowseState instead — see browseKeyOfTab.
  return null;
}

/** The browse state a Database tab owns, or null when it owns none. */
export function browseKeyOfTab(
  viewId: string,
  params?: Record<string, unknown>,
): string | null {
  if (viewId !== "database.table") {
    return null;
  }
  return `${String(params?.schema ?? "")}.${String(params?.table ?? "")}`;
}

/**
 * Frees a table's browse state once its tab is really gone.
 *
 * The same rule as useConsoleLifetime, for the same reason: unmounting is not
 * closing, and a module WorkbenchViews unmounted has to come back with its
 * filter, its sort and its page intact.
 */
export function useBrowseLifetime(browseKey: string): void {
  const closeBrowse = useDatabaseStore((s) => s.closeBrowse);
  useEffect(() => {
    return () => {
      const open = moduleTabs(useWorkbenchTabsStore.getState().modules, "database").openTabs;
      const stillOpen = open.some((tab) => browseKeyOfTab(tab.viewId, tab.params) === browseKey);
      if (!stillOpen) {
        closeBrowse(browseKey);
      }
    };
  }, [browseKey, closeBrowse]);
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
