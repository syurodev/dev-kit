import { create } from "zustand";
import { persist } from "zustand/middleware";
import { uiStateStorage } from "../../bridge/uistate";
import {
  listProfiles,
  listColumns,
  getSchemaCache,
  indexSchema,
  runQuery,
  type ConnectionProfile,
  type QueryResult,
  type SchemaSnapshot,
  type ColumnInfo,
} from "../../bridge/database";
import { listEnvTags, type EnvTag } from "../../bridge/environments";
import { listSSHProfiles, type SSHProfile } from "../../bridge/ssh";
import { errorMessage, isCancelError } from "../../bridge/errors";
import { buildBrowseQuery } from "./sql/browseQuery";

/**
 * A folder is keyed by id, not by its name.
 *
 * Name was the key until the Edit folder dialog arrived. Renaming then meant
 * writing a new key, so the profiles inside, the expanded state and the icon
 * would all reset at once. With no UI able to rename, that stayed theoretical;
 * with one, it is an everyday path.
 */
export type DatabaseFolder = {
  id: string;
  name: string;
  icon: string;
  profileIds: string[];
};

function newFolderId(): string {
  return crypto.randomUUID();
}

type LegacyFolderState = {
  folders?: Record<string, string[]> | DatabaseFolder[];
  expandedFolders?: Record<string, boolean>;
};

/**
 * v0 kept `folders` as name → profileIds, with `expandedFolders` on the same
 * names. Rekey both onto generated ids and keep the name as a label. Exported
 * so the rekeying can be tested without standing up a store.
 */
// Omit rather than intersect: the two fields are replaced, and intersecting
// would keep the caller's old key types alongside the new ones.
type MigratedFolderState<T> = Omit<T, "folders" | "expandedFolders"> & {
  folders: DatabaseFolder[];
  expandedFolders: Record<string, boolean>;
};

export function migrateFoldersToIds<T extends LegacyFolderState>(
  state: T,
  generateId: () => string = newFolderId,
): MigratedFolderState<T> {
  const legacy = state.folders;
  if (Array.isArray(legacy)) {
    return state as MigratedFolderState<T>;
  }

  const folders: DatabaseFolder[] = Object.entries(legacy ?? {}).map(
    ([name, profileIds]) => ({
      id: generateId(),
      name,
      icon: "folder",
      profileIds: profileIds ?? [],
    }),
  );

  const expandedFolders: Record<string, boolean> = {};
  for (const folder of folders) {
    const wasExpanded = state.expandedFolders?.[folder.name];
    if (wasExpanded !== undefined) expandedFolders[folder.id] = wasExpanded;
  }

  return { ...state, folders, expandedFolders };
}

/**
 * One query console's own state. Several consoles are open at once, so none of
 * this can live as a single field on the store: a result carries the id of the
 * console that asked for it, and a slow query landing after you switched tabs
 * has to find its way back to the tab that ran it.
 */
export type ConsoleState = {
  sql: string;
  queryResult: QueryResult | null;
  runningQuery: boolean;
  queryStartTime: number | null;
  queryError: string;
  queryStatus: string;
  queryDurationMs: number | null;
  queryRunId: number;
  /** Percent of the console's height given to the editor. */
  editorSize: number;
};

/** The console a module opens with, and the one every legacy caller means. */
export const DEFAULT_CONSOLE_ID = "console-1";

export const EMPTY_CONSOLE: ConsoleState = Object.freeze({
  sql: "",
  queryResult: null,
  runningQuery: false,
  queryStartTime: null,
  queryError: "",
  queryStatus: "",
  queryDurationMs: null,
  queryRunId: 0,
  editorSize: 50,
}) as ConsoleState;

/**
 * One table tab's browsing state.
 *
 * `cursors` is the key of the LAST row of every page already visited, so page
 * n starts after cursors[n-1]. Holding the trail rather than one cursor is what
 * makes the back button work: a keyset walk has no way to compute the previous
 * page's start from the current one.
 */
export type BrowseState = {
  where: string;
  orderBy: { column: string; direction: "asc" | "desc" } | null;
  rows: QueryResult | null;
  loading: boolean;
  error: string;
  /** Last-row keys of pages already seen, one per page boundary crossed. */
  cursors: string[][];
  pageIndex: number;
  /** Whether the server returned the extra row, i.e. another page exists. */
  hasNext: boolean;
  /** Column widths, kept per table because the columns are the table's own. */
  widths: number[];
  /**
   * A3: which load this state belongs to. Bumped when a page is asked for, and
   * checked when one answers — the same guard `executeQuery` puts on a console
   * result, for the same reason: Next clicked twice, or a sort applied while a
   * page is in flight, must not be overwritten by the older answer landing
   * last. A browser silently showing rows the toolbar does not describe is
   * worse than one that is slow.
   */
  runId: number;
};

export const EMPTY_BROWSE: BrowseState = Object.freeze({
  where: "",
  orderBy: null,
  rows: null,
  loading: false,
  error: "",
  cursors: [],
  pageIndex: 0,
  hasNext: false,
  widths: [],
  runId: 0,
}) as BrowseState;

/**
 * Rows per page. The backend caps every result at DefaultMaxRows = 200
 * (internal/database/database.go) with no per-query override, so a page cannot
 * usefully be larger than this — and the LIMIT 201 this plan sends comes back
 * as 200 rows with `truncated` set rather than 201 rows. applyBrowsePage reads
 * `truncated` as the probe row the server ate (A1).
 *
 * A2: exported, because the toolbar numbers its rows from it and "Open in
 * console" composes a statement with it. Three copies of 200 would drift.
 */
export const BROWSE_PAGE_SIZE = 200;

export type DatabaseStoreState = {
  profiles: ConnectionProfile[];
  selectedProfileId: string;
  envTags: EnvTag[];
  sshProfiles: SSHProfile[];
  loadingProfiles: boolean;
  profileError: string;

  // Cosmetic folder groups (uistate)
  folders: DatabaseFolder[];
  expandedFolders: Record<string, boolean>; // folder id -> open
  // Which connection is unfolded in the sidebar tree. Deliberately NOT
  // persisted, unlike the folders: folders are local, profiles come from the
  // bridge, and a remembered flag for a profile that was deleted meanwhile
  // describes nothing.
  expandedProfiles: Record<string, boolean>; // profile id -> open

  // Schema state
  schemaSnapshot: SchemaSnapshot | null;
  indexingSchema: boolean;
  /**
   * Why the last schema index failed. Its own field rather than a console's
   * queryError: re-indexing belongs to the connection, and with several
   * consoles open there is no console it could honestly belong to.
   */
  schemaError: string;
  selectedSchema: string;
  selectedTable: string;
  expandedSchemas: Record<string, boolean>;
  expandedTables: Record<string, boolean>;
  tableColumnsMap: Record<string, ColumnInfo[]>; // "schema.table" -> columns
  /**
   * Why a table's columns could not be read, keyed the same way. Its own field
   * rather than queryError: a rail failure written into the console's error
   * slot is a failure nobody looking at the rail can see.
   */
  tableColumnsError: Record<string, string>;

  /** Every open console's state, keyed by the tab's console id. */
  consoles: Record<string, ConsoleState>;

  /** Every open table tab's browsing state, keyed "schema.table". */
  browse: Record<string, BrowseState>;
  browseState: (key: string) => BrowseState;
  setBrowseWhere: (key: string, where: string) => void;
  setBrowseOrder: (key: string, orderBy: BrowseState["orderBy"]) => void;
  setBrowseWidths: (key: string, widths: number[]) => void;
  closeBrowse: (key: string) => void;
  /**
   * Turns one page's raw answer into what the grid shows: drops the probe row,
   * flips a backwards page, and records whether another page exists.
   */
  applyBrowsePage: (
    key: string,
    page: {
      result: QueryResult;
      pageSize: number;
      reversed: boolean;
      pageKey: readonly string[] | null;
    },
  ) => void;
  /** Runs the statement for the current page of a table tab. */
  loadBrowsePage: (key: string, opts: { schema: string; table: string }) => Promise<void>;
  /** Moves one page forward or back and loads it. */
  stepBrowsePage: (
    key: string,
    opts: { schema: string; table: string },
    step: 1 | -1,
  ) => Promise<void>;
  /**
   * The columns that page each table, or null where it has none. Read once per
   * table from its indexes, because a keyset walk cannot be built without it
   * and an index list is a round trip.
   */
  pageKeys: Record<string, string[] | null>;
  setPageKey: (key: string, columns: string[] | null) => void;

  // Selected cell for CellInspector
  inspectedCell: { columnName: string; value: string } | null;

  // Modals & Dialogs
  showNewConnectionDialog: boolean;
  showConnectionOptionsDialog: boolean;
  showExportModal: boolean;
  showSaveQueryModal: boolean;
  /**
   * The console a modal acts on. Set when a console opens a modal, because
   * "export this result" is about the console that asked, not about whichever
   * tab wins a race to render.
   */
  modalConsoleId: string;

  // Actions
  fetchProfiles: () => Promise<void>;
  setSelectedProfileId: (id: string) => void;
  setSql: (consoleId: string, sql: string) => void;
  setInspectedCell: (cell: { columnName: string; value: string } | null) => void;
  setShowNewConnectionDialog: (show: boolean) => void;
  setShowConnectionOptionsDialog: (show: boolean) => void;
  setShowExportModal: (show: boolean) => void;
  setShowSaveQueryModal: (show: boolean) => void;
  openExportModal: (consoleId: string) => void;
  openSaveQueryModal: (consoleId: string) => void;

  // Folder actions
  toggleFolder: (folderId: string) => void;
  toggleProfileExpand: (profileId: string) => void;
  createFolder: (name: string) => void;
  updateFolder: (id: string, patch: { name?: string; icon?: string }) => void;
  deleteFolder: (id: string) => void;
  setConsoleEditorSize: (consoleId: string, size: number) => void;
  assignProfileToFolder: (profileId: string, folderId: string) => void;

  // Schema actions
  loadSchemaSnapshot: (profileId: string) => Promise<void>;
  refreshSchema: (profileId: string) => Promise<void>;
  toggleSchemaExpand: (schema: string) => void;
  toggleTableExpand: (schema: string, table: string) => Promise<void>;
  setSelectedSchema: (schema: string) => void;
  setSelectedTable: (table: string) => void;

  // Query actions
  executeQuery: (consoleId: string) => Promise<void>;
  cancelQuery: (consoleId: string) => void;
  clearQueryResult: (consoleId: string) => void;
  clearQueryError: (consoleId: string) => void;
  /** Reports a failure into one console — Explain and friends, which run
   * outside executeQuery but land in the same error slot. */
  setQueryError: (consoleId: string, message: string) => void;
  /** Reads a console, defaulting to EMPTY_CONSOLE so callers never branch. */
  consoleState: (consoleId: string) => ConsoleState;
  /** Drops a console's slice when its tab closes. */
  closeConsole: (consoleId: string) => void;
};

const DEFAULT_FOLDERS: DatabaseFolder[] = [
  { id: "production", name: "Production", icon: "server", profileIds: [] },
  { id: "development", name: "Development", icon: "laptop", profileIds: [] },
];

/**
 * Merges a patch into one console, creating it from EMPTY_CONSOLE if this is
 * the first thing that touched it. Every console writer goes through here so a
 * missing slice is never a branch at the call site.
 */
function patchConsole(
  set: (fn: (state: DatabaseStoreState) => Partial<DatabaseStoreState>) => void,
  consoleId: string,
  patch: Partial<ConsoleState>,
): void {
  set((state) => ({
    consoles: {
      ...state.consoles,
      [consoleId]: { ...(state.consoles[consoleId] ?? EMPTY_CONSOLE), ...patch },
    },
  }));
}

function patchBrowse(
  set: (fn: (state: DatabaseStoreState) => Partial<DatabaseStoreState>) => void,
  key: string,
  patch: Partial<BrowseState>,
): void {
  set((state) => ({
    browse: {
      ...state.browse,
      [key]: { ...(state.browse[key] ?? EMPTY_BROWSE), ...patch },
    },
  }));
}

/**
 * The in-flight call per console, so Cancel reaches the right one. Outside the
 * store because a CancellableCall is not state — nothing renders from it, and
 * putting it in the store would make every console re-render when any query
 * starts.
 */
const pendingQueries = new Map<string, ReturnType<typeof runQuery>>();

/**
 * The in-flight page per table tab, so a superseded load can be stopped. Same
 * reasoning as pendingQueries: a CancellableCall is not state, and holding it
 * in the store would re-render every open table whenever any one of them paged.
 */
const pendingBrowseQueries = new Map<string, ReturnType<typeof runQuery>>();

export const useDatabaseStore = create<DatabaseStoreState>()(
  persist(
    (set, get) => ({
  profiles: [],
  selectedProfileId: "",
  envTags: [],
  sshProfiles: [],
  loadingProfiles: false,
  profileError: "",

  folders: DEFAULT_FOLDERS,
  expandedFolders: { production: true, development: true },
  expandedProfiles: {},

  schemaSnapshot: null,
  indexingSchema: false,
  schemaError: "",
  selectedSchema: "",
  selectedTable: "",
  expandedSchemas: { "public": true },
  expandedTables: {},
  tableColumnsMap: {},
  tableColumnsError: {},

  consoles: {},
  browse: {},
  pageKeys: {},

  inspectedCell: null,

  showNewConnectionDialog: false,
  showConnectionOptionsDialog: false,
  showExportModal: false,
  showSaveQueryModal: false,
  modalConsoleId: "",

  fetchProfiles: async () => {
    set({ loadingProfiles: true, profileError: "" });
    try {
      const [profiles, envTags, sshProfiles] = await Promise.all([
        listProfiles(),
        listEnvTags(),
        listSSHProfiles(),
      ]);
      set({ profiles, envTags, sshProfiles, loadingProfiles: false });
      const currentSelected = get().selectedProfileId;
      if (!currentSelected && profiles.length > 0) {
        get().setSelectedProfileId(profiles[0].id);
      }
    } catch (e) {
      set({ profileError: errorMessage(e), loadingProfiles: false });
    }
  },

  setSelectedProfileId: (id: string) => {
    set({
      selectedProfileId: id,
      tableColumnsMap: {},
      tableColumnsError: {},
      // A focused table belongs to the database it was picked from.
      selectedTable: "",
      // Picking a connection is what indexes it, so the tree unfolds it in the
      // same click rather than asking for a second one.
      expandedProfiles: { ...get().expandedProfiles, [id]: true },
    });
    // Every console's result came from the connection you just left, so none of
    // them describe anything any more. The SQL stays: the same query against a
    // different database is usually why you switched.
    set((state) => ({
      consoles: Object.fromEntries(
        Object.entries(state.consoles).map(([id, console]) => [
          id,
          { ...console, queryResult: null, queryError: "", queryStatus: "", queryDurationMs: null },
        ]),
      ),
    }));
    if (id) {
      void get().loadSchemaSnapshot(id);
    }
  },

  setInspectedCell: (cell) => set({ inspectedCell: cell }),
  setShowNewConnectionDialog: (show) => set({ showNewConnectionDialog: show }),
  setShowConnectionOptionsDialog: (show) => set({ showConnectionOptionsDialog: show }),
  setShowExportModal: (show) => set({ showExportModal: show }),
  setShowSaveQueryModal: (show) => set({ showSaveQueryModal: show }),
  openExportModal: (consoleId) => set({ showExportModal: true, modalConsoleId: consoleId }),
  openSaveQueryModal: (consoleId) => set({ showSaveQueryModal: true, modalConsoleId: consoleId }),

  toggleFolder: (folderId: string) => {
    const current = get().expandedFolders;
    set({ expandedFolders: { ...current, [folderId]: current[folderId] === false } });
  },

  toggleProfileExpand: (profileId: string) => {
    const current = get().expandedProfiles;
    set({ expandedProfiles: { ...current, [profileId]: !current[profileId] } });
  },

  createFolder: (name: string) => {
    const trimmed = name.trim();
    if (!trimmed) return;
    set({
      folders: [
        ...get().folders,
        { id: newFolderId(), name: trimmed, icon: "folder", profileIds: [] },
      ],
    });
  },

  updateFolder: (id: string, patch: { name?: string; icon?: string }) => {
    set({
      folders: get().folders.map((f) =>
        f.id === id
          ? { ...f, name: patch.name?.trim() || f.name, icon: patch.icon ?? f.icon }
          : f,
      ),
    });
  },

  // Deleting a folder deletes the grouping only. The profiles inside fall back
  // to Ungrouped — removing a connection is DBDeleteProfile, somewhere else.
  deleteFolder: (id: string) => {
    const expanded = { ...get().expandedFolders };
    delete expanded[id];
    set({ folders: get().folders.filter((f) => f.id !== id), expandedFolders: expanded });
  },

  // A profile belongs to at most one folder, so assigning removes it from
  // every other. An empty folderId means Ungrouped.
  assignProfileToFolder: (profileId: string, folderId: string) => {
    set({
      folders: get().folders.map((f) => ({
        ...f,
        profileIds:
          f.id === folderId
            ? [...f.profileIds.filter((id) => id !== profileId), profileId]
            : f.profileIds.filter((id) => id !== profileId),
      })),
    });
  },

  loadSchemaSnapshot: async (profileId: string) => {
    try {
      const snap = await getSchemaCache(profileId);
      set({ schemaSnapshot: snap });
      const schemas = Object.keys(snap.namespace || {});
      if (schemas.length > 0 && !get().selectedSchema) {
        set({ selectedSchema: schemas[0] });
      }
    } catch {
      // If cache read fails, try indexing once
      void get().refreshSchema(profileId);
    }
  },

  refreshSchema: async (profileId: string) => {
    set({ indexingSchema: true, schemaError: "" });
    try {
      const snap = await indexSchema(profileId);
      set({ schemaSnapshot: snap, indexingSchema: false });
      const schemas = Object.keys(snap.namespace || {});
      if (schemas.length > 0 && !get().selectedSchema) {
        set({ selectedSchema: schemas[0] });
      }
    } catch (e) {
      set({ indexingSchema: false, schemaError: errorMessage(e) });
    }
  },

  toggleSchemaExpand: (schema: string) => {
    const current = get().expandedSchemas;
    set({ expandedSchemas: { ...current, [schema]: !current[schema] } });
  },

  toggleTableExpand: async (schema: string, table: string) => {
    const key = `${schema}.${table}`;
    const expanded = get().expandedTables;
    const isNowExpanded = !expanded[key];
    set({ expandedTables: { ...expanded, [key]: isNowExpanded } });

    // The snapshot already carries every column's type, so opening a table
    // normally costs nothing. listColumns runs only for a snapshot written
    // before the cache carried types, which the next re-index retires.
    const indexed = get().schemaSnapshot?.columns?.[schema]?.[table];
    if (isNowExpanded && !indexed?.length && !get().tableColumnsMap[key]) {
      const profileId = get().selectedProfileId;
      if (!profileId) return;
      try {
        const columns = await listColumns(profileId, schema, table);
        set((state) => {
          const { [key]: _cleared, ...rest } = state.tableColumnsError;
          return {
            tableColumnsMap: { ...state.tableColumnsMap, [key]: columns },
            tableColumnsError: rest,
          };
        });
      } catch (e) {
        set((state) => ({
          tableColumnsError: { ...state.tableColumnsError, [key]: errorMessage(e) },
        }));
      }
    }
  },

  setSelectedSchema: (schema: string) => set({ selectedSchema: schema }),
  setSelectedTable: (table: string) => set({ selectedTable: table }),

  executeQuery: async (consoleId) => {
    const { selectedProfileId } = get();
    const console = get().consoleState(consoleId);
    if (!selectedProfileId || !console.sql.trim()) {
      return;
    }

    const runId = console.queryRunId + 1;
    patchConsole(set, consoleId, {
      queryRunId: runId,
      runningQuery: true,
      queryError: "",
      queryStatus: "",
      queryStartTime: Date.now(),
    });

    try {
      const call = runQuery(selectedProfileId, console.sql);
      pendingQueries.set(consoleId, call);
      const result = await call;
      // A newer run in THIS console has already started; its answer wins.
      if (get().consoleState(consoleId).queryRunId !== runId) {
        return;
      }
      patchConsole(set, consoleId, {
        queryResult: result,
        runningQuery: false,
        queryDurationMs: Date.now() - (get().consoleState(consoleId).queryStartTime ?? Date.now()),
        queryStatus: `${result.rowCount} row${result.rowCount === 1 ? "" : "s"}`,
      });
    } catch (e) {
      if (isCancelError(e)) {
        // A cancel is something the user asked for; it leaves the previous
        // result alone.
        patchConsole(set, consoleId, { runningQuery: false });
        return;
      }
      if (get().consoleState(consoleId).queryRunId !== runId) {
        return;
      }
      patchConsole(set, consoleId, { queryError: errorMessage(e), runningQuery: false });
    } finally {
      pendingQueries.delete(consoleId);
    }
  },

  cancelQuery: (consoleId) => {
    pendingQueries.get(consoleId)?.cancel();
  },

  consoleState: (consoleId) => get().consoles[consoleId] ?? EMPTY_CONSOLE,

  closeConsole: (consoleId) =>
    set((state) => {
      if (!(consoleId in state.consoles)) {
        return state;
      }
      const { [consoleId]: _closed, ...rest } = state.consoles;
      return { consoles: rest };
    }),

  browseState: (key) => get().browse[key] ?? EMPTY_BROWSE,

  // Both of these invalidate the walk: the cursors describe positions in a
  // result the new filter or sort no longer produces, and reusing them would
  // page through rows that are not there.
  setBrowseWhere: (key, where) =>
    patchBrowse(set, key, { where, cursors: [], pageIndex: 0, hasNext: false }),

  setBrowseOrder: (key, orderBy) =>
    patchBrowse(set, key, { orderBy, cursors: [], pageIndex: 0, hasNext: false }),

  setBrowseWidths: (key, widths) => patchBrowse(set, key, { widths }),

  applyBrowsePage: (key, { result, pageSize, reversed }) => {
    // A1. Two ways to learn there is more: the probe row arrived, or the server
    // ate it. DBQuery caps at DefaultMaxRows = 200 with no override, so a
    // LIMIT 201 for a 200-row page comes back as 200 rows with `truncated`
    // set and the probe row gone. Reading the count alone would leave hasNext
    // false on every full page and the table unpageable past row 200.
    const hasNext = result.rows.length > pageSize || result.truncated;
    const kept = result.rows.length > pageSize ? result.rows.slice(0, pageSize) : result.rows;
    // A backwards page was asked for in DESC order so the server could walk
    // away from the cursor; the rows arrive bottom-first.
    const rows = reversed ? [...kept].reverse() : kept;
    patchBrowse(set, key, {
      rows: { ...result, rows, rowCount: rows.length },
      hasNext: reversed ? true : hasNext,
      loading: false,
      error: "",
    });
  },

  loadBrowsePage: async (key, { schema, table }) => {
    const { selectedProfileId, profiles } = get();
    if (!selectedProfileId) {
      return;
    }
    const browse = get().browseState(key);
    const engine = profiles.find((p) => p.id === selectedProfileId)?.engine;
    const pageKey = get().pageKeys[key] ?? null;
    const cursor =
      pageKey && browse.pageIndex > 0
        ? { values: browse.cursors[browse.pageIndex - 1] ?? [], direction: "next" as const }
        : null;

    const query = buildBrowseQuery({
      schema,
      table,
      engine,
      where: browse.where,
      orderBy: browse.orderBy,
      pageKey,
      cursor,
      pageSize: BROWSE_PAGE_SIZE,
      offset: pageKey ? undefined : browse.pageIndex * BROWSE_PAGE_SIZE,
    });

    // A3. The same stale-answer guard executeQuery puts on a console result.
    // Next clicked twice, or a sort applied while a page is in flight, leaves
    // two calls racing; without this the older one can land last and fill the
    // grid with rows the toolbar no longer describes.
    const runId = browse.runId + 1;
    patchBrowse(set, key, { loading: true, error: "", runId });
    // And the superseded statement is stopped rather than left to finish: the
    // page it would return is one nobody is going to look at.
    pendingBrowseQueries.get(key)?.cancel();
    try {
      const call = runQuery(selectedProfileId, query.sql);
      pendingBrowseQueries.set(key, call);
      const result = await call;
      if (get().browseState(key).runId !== runId) {
        return;
      }
      get().applyBrowsePage(key, {
        result,
        pageSize: query.pageSize,
        reversed: query.reversed,
        pageKey,
      });
      // Remember where this page ended, so the next one can start after it.
      if (pageKey && result.rows.length > 0) {
        const last = result.rows[Math.min(result.rows.length, query.pageSize) - 1];
        const keyIndexes = pageKey.map((column) => result.columns.indexOf(column));
        if (!keyIndexes.includes(-1)) {
          const trail = [...get().browseState(key).cursors];
          trail[get().browseState(key).pageIndex] = keyIndexes.map((i) => last[i]);
          patchBrowse(set, key, { cursors: trail });
        }
      }
    } catch (e) {
      // Both branches check the guard first. A cancel usually means a newer
      // load superseded this one, and clearing `loading` for the old call
      // would leave the spinner off while the new page is still coming.
      if (get().browseState(key).runId !== runId) {
        return;
      }
      if (isCancelError(e)) {
        patchBrowse(set, key, { loading: false });
        return;
      }
      patchBrowse(set, key, { loading: false, error: errorMessage(e) });
    } finally {
      if (get().browseState(key).runId === runId) {
        pendingBrowseQueries.delete(key);
      }
    }
  },

  stepBrowsePage: async (key, target, step) => {
    const browse = get().browseState(key);
    const next = browse.pageIndex + step;
    if (next < 0 || (step === 1 && !browse.hasNext)) {
      return;
    }
    patchBrowse(set, key, { pageIndex: next });
    await get().loadBrowsePage(key, target);
  },

  setPageKey: (key, columns) =>
    set((state) => ({ pageKeys: { ...state.pageKeys, [key]: columns } })),

  closeBrowse: (key) =>
    set((state) => {
      if (!(key in state.browse)) {
        return state;
      }
      const { [key]: _closed, ...rest } = state.browse;
      return { browse: rest };
    }),

  setSql: (consoleId, sql) => patchConsole(set, consoleId, { sql }),

  setConsoleEditorSize: (consoleId, size) =>
    patchConsole(set, consoleId, { editorSize: size }),

  clearQueryResult: (consoleId) =>
    patchConsole(set, consoleId, { queryResult: null, queryStatus: "", queryDurationMs: null }),

  clearQueryError: (consoleId) => patchConsole(set, consoleId, { queryError: "" }),

  setQueryError: (consoleId, message) => patchConsole(set, consoleId, { queryError: message }),
    }),
    {
      name: "database.folders",
      storage: uiStateStorage,
      version: 1,
      migrate: (persisted) => migrateFoldersToIds(persisted as never),
      // Only the cosmetic grouping persists. Profiles, schema and results all
      // come from the bridge; caching them here would let a stale copy outlive
      // the thing it describes.
      partialize: (state) => ({
        folders: state.folders,
        expandedFolders: state.expandedFolders,
      }),
    },
  ),
);
