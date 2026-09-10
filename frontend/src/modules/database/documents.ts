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
  const preload = getModule("database")?.views.find((v) => v.id === viewId)?.preload;
  if (preload) {
    const startedAt = performance.now();
    void preload()
      .then(() => {
        if (import.meta.env.DEV || localStorage.getItem("devkitPerf") === "1") {
          // eslint-disable-next-line no-console -- dev-only instrumentation
          console.info(`[chunk] ${viewId} ready in ${Math.round(performance.now() - startedAt)}ms`);
        }
      })
      .catch(() => undefined);
  }
  useWorkbenchTabsStore.getState().openTab({ moduleId: "database", viewId, title, icon, params, preview });
}

export function openTableTab(schema: string, table: string, preview = false): void {
  open("database.table", table, Table2, { schema, table }, preview);
}

export function openConsoleTab(consoleId: string, title = "Console"): void {
  open("database.console", title, Terminal, { consoleId }, false);
}

/**
 * A blank console, from the strip's + button.
 *
 * The id has to be unique or the new tab would resolve to an existing console
 * through tabKey and simply focus it — which is what "new" must not do. A
 * counter rather than a timestamp: two clicks inside the same millisecond are
 * reachable, a repeated counter value is not.
 */
let consoleSerial = 0;
export function openNewConsoleTab(): void {
  consoleSerial += 1;
  openConsoleTab(`console-${consoleSerial}-${Math.random().toString(36).slice(2, 8)}`);
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
