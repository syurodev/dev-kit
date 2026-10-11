import { getModule, viewIcon } from "@/modules/registry";
import { perfLog } from "@/lib/perfLog";
import { moduleTabs, tabKey, useWorkbenchTabsStore } from "@/stores/workbenchTabs";
import { DEFAULT_CONSOLE_ID, useDatabaseStore } from "./databaseStore";

/**
 * Opening a Database document.
 *
 * Every one of these is a tab, which is what removed the module's own five-way
 * view switch: a mode you can only be in one of is a worse fit than a document
 * you can have several of. `preview` is the shared slot a single click uses —
 * a second click on something else replaces it, so browsing the rail does not
 * leave a trail of tabs behind.
 */
function open(viewId: string, title: string, params?: Record<string, unknown>, preview = true) {
  // Start the chunk before React needs it: the tab appears in the same frame
  // either way, but the view lands sooner.
  const preload = getModule("database")?.views.find((v) => v.id === viewId)?.preload;
  if (preload) {
    const startedAt = performance.now();
    void preload()
      .then(() => {
        if (import.meta.env.DEV || localStorage.getItem("devkitPerf") === "1") {
          perfLog(`[perf] chunk ${viewId} ready in ${Math.round(performance.now() - startedAt)}ms`);
        }
      })
      .catch(() => undefined);
  }
  // The glyph comes from the manifest, not the call site: a restored tab has
  // only its view id to go on, and both paths have to reach the same icon or
  // the strip changes after a relaunch.
  useWorkbenchTabsStore.getState().openTab({
    moduleId: "database",
    viewId,
    title,
    icon: viewIcon("database", viewId),
    params,
    preview,
  });
}

export function openTableTab(schema: string, table: string, preview = false): void {
  const state=useDatabaseStore.getState();
  const profile=state.profiles.find(p=>p.id===state.selectedProfileId);
  const params={schema,table,...(profile?.parentProfileId || profile?.engine==="postgres" || !profile?.engine ? state.selectedProfileId ? {profileId:state.selectedProfileId} : {} : {})};
  open("database.table", profile?.parentProfileId ? `${table} · ${profile.database}` : table, params, preview);
}

/**
 * A console id, from something that identifies the console.
 *
 * The id is also the name of the file the console is written to, so it has to
 * survive being one: a table called `orders/2024` or a schema with a dot in
 * it would otherwise produce a path, and the store rejects those rather than
 * cleaning them. Deterministic, so reopening the same table finds the same
 * console rather than starting a second one beside it.
 */
export function consoleIdFor(seed: string): string {
  const slug = seed
    .replace(/[^A-Za-z0-9_-]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 48);
  return `console-${slug || "untitled"}`;
}

export function openConsoleTab(consoleId: string, title = "Console"): void {
  open("database.console", title, { consoleId }, false);
}

/**
 * Closes the tab a console is open in, if it is open in one.
 *
 * Deleting a console deletes the document, and leaving its editor open over
 * a file that no longer exists is the kind of half-state that ends with the
 * next keystroke writing the whole thing back.
 */
export function closeConsoleTab(consoleId: string): void {
  const state = useWorkbenchTabsStore.getState();
  state.closeTab(tabKey("database", "database.console", { consoleId }));
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
/** A console id no open console has — for a new console, or a copy of one. */
export function newConsoleId(): string {
  consoleSerial += 1;
  return `console-${consoleSerial}-${Math.random().toString(36).slice(2, 8)}`;
}

export function openNewConsoleTab(): void {
  openConsoleTab(newConsoleId());
}

/**
 * A saved query, opened in the console it always opens in.
 *
 * Namespaced by kind: a history entry and a saved query can share an id, and
 * without the prefix the two would collide on one console. Here rather than
 * in a view because two places open them now — the Saved queries document
 * and the rail's Saved list — and two copies of the id rule is one console
 * too many.
 */
export function openSavedQuery(entry: { id: string; name: string; sql: string }): void {
  const consoleId = consoleIdFor(`saved-${entry.id}`);
  useDatabaseStore.getState().setSql(consoleId, entry.sql);
  openConsoleTab(consoleId, entry.name || "Console");
}

export function openErdTab(): void {
  open("database.erd", "ERD diagram", undefined, false);
}

export function openActivityTab(): void {
  open("database.activity", "Live activity", undefined, false);
}

export function openHistoryTab(profileId?: string): void {
  open("database.history", "Query history", profileId ? {profileId} : undefined, false);
}

export function openSavedTab(): void {
  open("database.saved", "Saved queries", undefined, false);
}

/**
 * The console id of the tab in front of you, or "" when the active Database
 * tab is not a console.
 *
 * The menu bar's Run Query needs it: the keystroke inside a console is handled
 * by that console, but the menu item is fired from outside any of them and has
 * to ask which one is on screen. Same defaulting rule as consoleIdOfTab — the
 * module's home tab carries no params and means the first console.
 */
export function activeConsoleId(): string {
  const state = useWorkbenchTabsStore.getState();
  const tabs = moduleTabs(state.modules, "database");
  const active = tabs.openTabs.find((tab) => tab.id === tabs.activeTabId);
  if (!active || active.viewId !== "database.console") return "";
  return typeof active.params?.consoleId === "string" ? active.params.consoleId : DEFAULT_CONSOLE_ID;
}
