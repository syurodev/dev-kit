import { getModule, viewIcon } from "@/modules/registry";
import { perfLog } from "@/lib/perfLog";
import { moduleTabs, tabKey, useWorkbenchTabsStore } from "@/stores/workbenchTabs";
import { listConsoles, type QueryResult, type StoredConsole } from "@/bridge/database";
import { consolesHydrationStarted, ensureConsolesHydrated } from "./consoleHydration";
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
function open(viewId: string, title: string, params?: Record<string, unknown>, preview = true, beside = false) {
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
    beside,
  });
}

export function openTableTab(schema: string, table: string, preview = false, options: { beside?: boolean } = {}): void {
  const state=useDatabaseStore.getState();
  const profile=state.profiles.find(p=>p.id===state.selectedProfileId);
  const params={schema,table,...(profile?.parentProfileId || profile?.engine==="postgres" || !profile?.engine ? state.selectedProfileId ? {profileId:state.selectedProfileId} : {} : {})};
  open("database.table", profile?.parentProfileId ? `${table} · ${profile.database}` : table, params, preview, options.beside);
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

export function openConsoleTab(consoleId: string, title = "Console", options: { beside?: boolean } = {}): void {
  open("database.console", title, { consoleId }, false, options.beside);
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
  state.closeTab(tabKey("database", "database.console", { consoleId }), { remember: false });
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

/** The console a statement from outside opens in — keyed by the SQL itself. */
export function sqlConsoleId(sql: string): string {
  return consoleIdFor(`sql-${hashText(sql)}`);
}

/**
 * A console holding a statement that came from outside the module — an
 * agent's query in the transcript, a reference to one. Keyed by the SQL
 * itself (a hash: two statements that start alike are still two consoles),
 * so opening the same one twice focuses the console it already has. Nothing
 * is run.
 *
 * The SQL is written only into a console that is empty on disk as well as in
 * memory: once the person has typed in it, it is theirs. So the files are
 * read first — the rail may not have mounted yet after a relaunch — and a
 * console closed since (its slice dropped, its file kept) is read back
 * before it is judged empty.
 */
export async function openSqlConsole(
  sql: string,
  title = "Console",
  options: {
    beside?: boolean;
    load?: () => Promise<StoredConsole[]>;
    /**
     * Rows an agent already has for this statement (db.query), shown as the
     * console's result without running anything — only while the console
     * holds this statement and nothing of the person's is there: no result
     * of a run (earlier agent rows are replaced), no error of a failed run,
     * no run in progress. In memory only: results are never written to the
     * file.
     */
    result?: QueryResult;
    /** The agent's call named no connection (an older transcript); the console says so. */
    connectionUnknown?: boolean;
  } = {},
): Promise<void> {
  const consoleId = sqlConsoleId(sql);
  const load = options.load ?? listConsoles;
  const readNow = !consolesHydrationStarted();
  await ensureConsolesHydrated(load);
  if (!readNow && useDatabaseStore.getState().consoleState(consoleId).sql === "") {
    await useDatabaseStore
      .getState()
      .hydrateConsoles(async () => (await load()).filter((file) => file.id === consoleId));
  }
  const state = useDatabaseStore.getState();
  if (state.consoleState(consoleId).sql === "") state.setSql(consoleId, sql);
  const current = useDatabaseStore.getState().consoleState(consoleId);
  const nothingOfTheirs =
    current.results.every((tab) => tab.origin === "agent") && !current.queryError && !current.runningQuery;
  if (options.result && current.sql === sql && nothingOfTheirs) {
    state.showAgentResult(consoleId, sql, options.result, { connectionUnknown: options.connectionUnknown });
  }
  openConsoleTab(consoleId, title, { beside: options.beside });
}

/** FNV-1a, base 36: short, stable, and only ever used to name a console. */
function hashText(text: string): string {
  let hash = 0x811c9dc5;
  for (let i = 0; i < text.length; i += 1) {
    hash ^= text.charCodeAt(i);
    hash = Math.imul(hash, 0x01000193);
  }
  return (hash >>> 0).toString(36);
}

export function openErdTab(): void {
  open("database.erd", "ERD diagram", undefined, false);
}

export function openActivityTab(): void {
  open("database.activity", "Live activity", undefined, false);
}

export function openHistoryTab(): void {
  open("database.history", "Query history", undefined, false);
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
