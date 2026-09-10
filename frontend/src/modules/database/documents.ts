import { Table2, Network, Activity, History, Bookmark, Terminal } from "lucide-react";
import { getModule } from "@/modules/registry";
import { useWorkbenchTabsStore } from "@/stores/workbenchTabs";

/**
 * Opening a Database document.
 *
 * Every one of these is a tab, which is what removed the module's own five-way
 * view switch: a mode you can only be in one of is a worse fit than a document
 * you can have several of. `preview` is the shared slot a single click uses —
 * a second click on something else replaces it, so browsing the rail does not
 * leave a trail of tabs behind.
 */
function open(viewId: string, title: string, icon: typeof Table2, params?: Record<string, unknown>, preview = true) {
  // Start the chunk before React needs it: the tab appears in the same frame
  // either way, but the view lands sooner.
  void getModule("database")?.views.find((v) => v.id === viewId)?.preload?.().catch(() => undefined);
  useWorkbenchTabsStore.getState().openTab({ moduleId: "database", viewId, title, icon, params, preview });
}

export function openTableTab(schema: string, table: string, preview = false): void {
  open("database.table", table, Table2, { schema, table }, preview);
}

export function openConsoleTab(consoleId: string, title = "Console"): void {
  open("database.console", title, Terminal, { consoleId }, false);
}

export function openErdTab(): void {
  open("database.erd", "ERD diagram", Network, undefined, false);
}

export function openActivityTab(): void {
  open("database.activity", "Live activity", Activity, undefined, false);
}

export function openHistoryTab(): void {
  open("database.history", "Query history", History, undefined, false);
}

export function openSavedTab(): void {
  open("database.saved", "Saved queries", Bookmark, undefined, false);
}
