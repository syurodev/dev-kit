import { create } from "zustand";
import { persist } from "zustand/middleware";
import { uiStateStorage } from "../../bridge/uistate";
import {
  listProfiles,
  listColumns,
  getSchemaCache,
  indexSchema,
  runQuery,
  beginTransaction,
  commitTransaction,
  rollbackTransaction,
  transactionOpen,
  type ConnectionProfile,
  type QueryResult,
  type SchemaSnapshot,
  type ColumnInfo,
} from "../../bridge/database";
import { listEnvTags, type EnvTag } from "../../bridge/environments";
import { listSSHProfiles, type SSHProfile } from "../../bridge/ssh";
import { errorMessage, isCancelError } from "../../bridge/errors";
import { buildBrowseQuery, buildCountQuery } from "./sql/browseQuery";

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
  /**
   * Rows matching the filter, from a COUNT(*) run once per filter, or null
   * while it is unknown. The probe row answers "is there another page" and
   * nothing more, so this is what a pager needs to name a last page or a page
   * it has never walked to. Null is a working state, not a failure: the pager
   * falls back to counting forward from what it has seen.
   */
  totalRows: number | null;
  /** Column widths, kept per table because the columns are the table's own. */
  widths: number[];
  /**
   * Display order as column positions. Per table, not per module: the columns
   * belong to the table, and a position means nothing against another one.
   */
  order: number[];
  /**
   * How many columns of the display order stay on screen while the grid
   * scrolls sideways. Per table for the same reason the order is.
   */
  frozen: number;
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
  totalRows: null,
  widths: [],
  order: [],
  frozen: 0,
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

/**
 * The page sizes the pager offers.
 *
 * Capped at 5,000 because that is the ceiling the service clamps to
 * (database.MaxRowsCeiling) — offering 50,000 here would quietly hand back
 * 5,000 and call it a page. There is deliberately no "All": the grid
 * virtualises rendering, not memory, and every row crosses the bridge as
 * strings first, so "All" on a large table is a hang with a friendly name.
 */
export const BROWSE_PAGE_SIZES = [100, 200, 500, 1000, 5000] as const;

/** What the export modal calls a result nobody gave a better name for. */
const DEFAULT_EXPORT_FILENAME = "query-result";

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
  /**
   * Rows per page, for every table tab. One value, not one per tab, and
   * persisted: a page size is a preference about how you like to read a table,
   * not a property of one of them, and "set as default" is then just picking it.
   */
  browsePageSize: number;
  setBrowsePageSize: (size: number) => void;
  browseState: (key: string) => BrowseState;
  setBrowseWhere: (key: string, where: string) => void;
  setBrowseOrder: (key: string, orderBy: BrowseState["orderBy"]) => void;
  setBrowseWidths: (key: string, widths: number[]) => void;
  setBrowseOrderColumns: (key: string, order: number[]) => void;
  setBrowseFrozen: (key: string, frozen: number) => void;
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
  /**
   * Runs one UPDATE for a grid edit and reloads the page it changed.
   *
   * Reloads rather than patching the rows in memory: a column with a trigger,
   * a default or a check constraint may not hold what was sent, and a grid
   * showing what we MEANT to write is worse than one that waits a round trip
   * for what is actually there.
   */
  /**
   * Auto or Manual commit, and whether a transaction is open right now.
   *
   * Per profile, because that is what a transaction is attached to. The mode
   * is the UI's policy — the backend knows only "open or not" — and switching
   * to Manual opens one immediately, so the very next statement is inside it
   * rather than the first one silently landing outside.
   */
  txManual: boolean;
  txOpen: boolean;
  txBusy: boolean;
  setTxManual: (manual: boolean) => Promise<void>;
  endTransaction: (action: "commit" | "rollback") => Promise<void>;
  refreshTxState: () => Promise<void>;

  applyBrowseEdit: (
    key: string,
    opts: { schema: string; table: string },
    sql: string,
  ) => Promise<void>;
  /** Moves one page forward or back and loads it. */
  stepBrowsePage: (
    key: string,
    opts: { schema: string; table: string },
    step: 1 | -1,
  ) => Promise<void>;
  /** Loads an arbitrary page of a table tab. The pager's first/last and page numbers. */
  goToBrowsePage: (
    key: string,
    opts: { schema: string; table: string },
    page: number,
  ) => Promise<void>;
  /** Counts the rows matching the current filter, once, into `totalRows`. */
  countBrowseRows: (key: string, opts: { schema: string; table: string }) => Promise<void>;
  /** The last page's index once the count is in, or null while it is unknown. */
  lastBrowsePage: (key: string) => number | null;
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
   * The console the save-query modal acts on. Set when a console opens it,
   * because "save this statement" is about the console that asked, not about
   * whichever tab wins a race to render.
   *
   * A28: export no longer reads this. A console id can only ever name a
   * console, and a table tab has rows worth exporting but no console and no
   * statement — so export carries its result (below), while save, which
   * genuinely needs the SQL, keeps the id.
   */
  modalConsoleId: string;
  /**
   * The rows the export modal writes out, captured when it opens. What put
   * them on screen — a console's query, a table tab's page — is the caller's
   * business and not the modal's.
   */
  exportResult: QueryResult | null;
  /** The name the export modal starts with, without an extension. */
  exportFilename: string;

  // Actions
  fetchProfiles: () => Promise<void>;
  setSelectedProfileId: (id: string) => void;
  setSql: (consoleId: string, sql: string) => void;
  setInspectedCell: (cell: { columnName: string; value: string } | null) => void;
  setShowNewConnectionDialog: (show: boolean) => void;
  setShowConnectionOptionsDialog: (show: boolean) => void;
  setShowExportModal: (show: boolean) => void;
  setShowSaveQueryModal: (show: boolean) => void;
  openExportModal: (result: QueryResult, filename?: string) => void;
  openSaveQueryModal: (consoleId: string) => void;

  // Folder actions
  toggleFolder: (folderId: string) => void;
  toggleProfileExpand: (profileId: string) => void;
  /** The id of the folder created, or "" when the name was blank. */
  createFolder: (name: string) => string;
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

/**
 * The two folders the sidebar used to ship with — Production and Development,
 * seeded empty. A grouping nobody asked for is two rows of chrome and a
 * suggestion about how to organise connections the app has no business making,
 * so v2 drops them.
 *
 * Only while still empty: a seeded folder somebody actually filled is theirs
 * now, and deleting it would scatter the connections inside back to Ungrouped.
 */
export function dropSeededFolders(folders: DatabaseFolder[]): DatabaseFolder[] {
  const seeded = new Set(["production", "development"]);
  return folders.filter((f) => !(seeded.has(f.id) && f.profileIds.length === 0));
}

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

/**
 * Counts in flight, per table tab. Their own map: a count and a page load run
 * side by side for the same tab, and sharing one slot would have each cancel
 * the other.
 */
const pendingCountQueries = new Map<string, ReturnType<typeof runQuery>>();

export const useDatabaseStore = create<DatabaseStoreState>()(
  persist(
    (set, get) => ({
  profiles: [],
  selectedProfileId: "",
  envTags: [],
  sshProfiles: [],
  loadingProfiles: false,
  profileError: "",

  folders: [],
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
  browsePageSize: BROWSE_PAGE_SIZE,
  pageKeys: {},

  inspectedCell: null,

  showNewConnectionDialog: false,
  showConnectionOptionsDialog: false,
  showExportModal: false,
  showSaveQueryModal: false,
  modalConsoleId: "",
  exportResult: null,
  exportFilename: DEFAULT_EXPORT_FILENAME,

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
    // Asked, not assumed: a transaction outlives the window that opened it, so
    // arriving at a profile can mean arriving inside one.
    queueMicrotask(() => void get().refreshTxState());
    set({
      selectedProfileId: id,
      // A transaction belongs to the connection you just left, not the one you
      // arrived at. Cleared here and asked again below, rather than carried
      // across and shown against the wrong profile.
      txOpen: false,
      txManual: false,
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
  openExportModal: (result, filename) =>
    set({
      showExportModal: true,
      exportResult: result,
      exportFilename: filename?.trim() || DEFAULT_EXPORT_FILENAME,
    }),
  openSaveQueryModal: (consoleId) => set({ showSaveQueryModal: true, modalConsoleId: consoleId }),

  toggleFolder: (folderId: string) => {
    const current = get().expandedFolders;
    set({ expandedFolders: { ...current, [folderId]: current[folderId] === false } });
  },

  toggleProfileExpand: (profileId: string) => {
    const current = get().expandedProfiles;
    set({ expandedProfiles: { ...current, [profileId]: !current[profileId] } });
  },

  // Returns the new id so the caller can open the folder for naming straight
  // away — "Folder 3" is a placeholder, not a decision.
  createFolder: (name: string) => {
    const trimmed = name.trim();
    if (!trimmed) return "";
    const id = newFolderId();
    set({
      folders: [...get().folders, { id, name: trimmed, icon: "folder", profileIds: [] }],
    });
    return id;
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

  setBrowsePageSize: (size) =>
    set((state) => {
      if (!BROWSE_PAGE_SIZES.includes(size as (typeof BROWSE_PAGE_SIZES)[number])) {
        return {};
      }
      // Every open tab's walk is invalidated, for the reason setBrowseWhere's
      // is: the cursors are page boundaries, and the boundaries just moved.
      // The counts survive — how many rows match is not a function of how many
      // of them fit on a page.
      const browse = Object.fromEntries(
        Object.entries(state.browse).map(([key, value]) => [
          key,
          { ...value, cursors: [], pageIndex: 0, hasNext: false },
        ]),
      );
      return { browsePageSize: size, browse };
    }),

  // Both of these invalidate the walk: the cursors describe positions in a
  // result the new filter or sort no longer produces, and reusing them would
  // page through rows that are not there.
  setBrowseWhere: (key, where) =>
    // The count goes with the cursors: it answered "how many rows match" for a
    // filter that is no longer the filter.
    patchBrowse(set, key, { where, cursors: [], pageIndex: 0, hasNext: false, totalRows: null }),

  setBrowseOrder: (key, orderBy) =>
    patchBrowse(set, key, { orderBy, cursors: [], pageIndex: 0, hasNext: false }),

  setBrowseWidths: (key, widths) => patchBrowse(set, key, { widths }),

  setBrowseOrderColumns: (key, order) => patchBrowse(set, key, { order }),

  setBrowseFrozen: (key, frozen) => patchBrowse(set, key, { frozen: Math.max(0, frozen) }),

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

  txManual: false,
  txOpen: false,
  txBusy: false,

  setTxManual: async (manual) => {
    const { selectedProfileId, txOpen } = get();
    if (!selectedProfileId) {
      set({ txManual: manual });
      return;
    }
    set({ txBusy: true });
    try {
      if (manual && !txOpen) {
        await beginTransaction(selectedProfileId);
        set({ txManual: true, txOpen: true });
        return;
      }
      if (!manual && txOpen) {
        // Leaving Manual commits what is open. Rolling back would silently
        // throw away work the user did inside it; the button beside this one
        // is how you discard it on purpose.
        await commitTransaction(selectedProfileId);
        set({ txManual: false, txOpen: false });
        return;
      }
      set({ txManual: manual });
    } finally {
      set({ txBusy: false });
    }
  },

  endTransaction: async (action) => {
    const { selectedProfileId, txManual } = get();
    if (!selectedProfileId) {
      return;
    }
    set({ txBusy: true });
    try {
      await (action === "commit"
        ? commitTransaction(selectedProfileId)
        : rollbackTransaction(selectedProfileId));
      // Still in Manual, so the next statement needs a transaction of its own
      // — ending one is not leaving the mode.
      if (txManual) {
        await beginTransaction(selectedProfileId);
        set({ txOpen: true });
      } else {
        set({ txOpen: false });
      }
    } finally {
      set({ txBusy: false });
    }
  },

  refreshTxState: async () => {
    const { selectedProfileId } = get();
    if (!selectedProfileId) {
      set({ txOpen: false, txManual: false });
      return;
    }
    const open = await transactionOpen(selectedProfileId);
    // A transaction found already open means Manual: it cannot have been
    // opened by anything else, and showing Auto beside an open one would be a
    // lie about what the next statement does.
    set({ txOpen: open, txManual: open ? true : get().txManual });
  },

  applyBrowseEdit: async (key, { schema, table }, sql) => {
    const { selectedProfileId } = get();
    if (!selectedProfileId) {
      return;
    }
    // Throws on to the caller rather than raising a toast here. A store that
    // imports the toaster puts it in every graph the store is in — and this
    // one is reachable from the eager manifest, so sonner became 32 KiB of the
    // shell's startup for a message only the table tab ever shows.
    await runQuery(selectedProfileId, sql);
    await get().loadBrowsePage(key, { schema, table });
  },

  loadBrowsePage: async (key, { schema, table }) => {
    const { selectedProfileId, profiles } = get();
    if (!selectedProfileId) {
      return;
    }
    const browse = get().browseState(key);
    const engine = profiles.find((p) => p.id === selectedProfileId)?.engine;
    const pageKey = get().pageKeys[key] ?? null;
    // The trail only reaches pages this tab has walked to. Jumping straight to
    // page 30 leaves cursors[29] undefined, and the old `?? []` turned that
    // into a cursor with no values, which buildBrowseQuery refuses (A15) — the
    // jump arrived as an error banner. No trail means no cursor, and no cursor
    // means OFFSET: slower, correct, and it lays the trail for the pages after.
    const trail = browse.pageIndex > 0 ? browse.cursors[browse.pageIndex - 1] : null;
    const cursor =
      pageKey && trail ? { values: trail, direction: "next" as const } : null;

    // A3. The same stale-answer guard executeQuery puts on a console result.
    // Next clicked twice, or a sort applied while a page is in flight, leaves
    // two calls racing; without this the older one can land last and fill the
    // grid with rows the toolbar no longer describes.
    const runId = browse.runId + 1;
    const pageSize = get().browsePageSize;
    patchBrowse(set, key, { loading: true, error: "", runId });
    // And the superseded statement is stopped rather than left to finish: the
    // page it would return is one nobody is going to look at.
    pendingBrowseQueries.get(key)?.cancel();
    try {
      // A16: composed inside the try. buildBrowseQuery refuses a cursor whose
      // arity does not match the key (A15), and a throw from out here reached
      // nobody — it escaped as an unhandled rejection, leaving the page index
      // advanced and the previous page's rows on screen, which is the silent
      // wrongness the refusal existed to prevent.
      const query = buildBrowseQuery({
        schema,
        table,
        engine,
        where: browse.where,
        orderBy: browse.orderBy,
        pageKey,
        cursor,
        pageSize,
        offset: cursor ? undefined : browse.pageIndex * pageSize,
      });
      // The row cap goes with the statement: the service caps at 200 unless
      // asked otherwise, so a LIMIT 1001 without this comes back as 200 rows
      // with `truncated` set — a page that silently ignored the page size and
      // claimed a next page it had not looked for.
      const call = runQuery(selectedProfileId, query.sql, pageSize + 1);
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

  lastBrowsePage: (key) => {
    const { totalRows } = get().browseState(key);
    if (totalRows === null) {
      return null;
    }
    return Math.max(0, Math.ceil(totalRows / get().browsePageSize) - 1);
  },

  stepBrowsePage: (key, target, step) =>
    get().goToBrowsePage(key, target, get().browseState(key).pageIndex + step),

  goToBrowsePage: async (key, target, page) => {
    const browse = get().browseState(key);
    // How far forward the tab is allowed to go. Counted, that is the last page;
    // uncounted, the probe row knows about exactly one page past this one and
    // nothing beyond it, so that is the honest limit.
    const last = get().lastBrowsePage(key);
    const forwardLimit = last ?? browse.pageIndex + (browse.hasNext ? 1 : 0);
    if (page < 0 || page > forwardLimit || page === browse.pageIndex) {
      return;
    }
    const previous = browse.pageIndex;
    patchBrowse(set, key, { pageIndex: page });
    await get().loadBrowsePage(key, target);
    // A21: put the number back if no page arrived. loadBrowsePage clears
    // `error` before it runs, so an error here is this load's own — and a
    // superseded load leaves `error` empty, which correctly means the newer
    // load owns the page number now and this one must not touch it.
    if (get().browseState(key).error) {
      patchBrowse(set, key, { pageIndex: previous });
    }
  },

  countBrowseRows: async (key, { schema, table }) => {
    const { selectedProfileId, profiles } = get();
    const browse = get().browseState(key);
    // Once per filter. setBrowseWhere nulls it again, which is what makes this
    // re-run for a filter it has not answered for.
    if (!selectedProfileId || !schema || !table || browse.totalRows !== null) {
      return;
    }
    const engine = profiles.find((p) => p.id === selectedProfileId)?.engine;
    pendingCountQueries.get(key)?.cancel();
    try {
      const call = runQuery(
        selectedProfileId,
        buildCountQuery({ schema, table, engine, where: browse.where }),
      );
      pendingCountQueries.set(key, call);
      const result = await call;
      const value = Number(result.rows[0]?.[0]);
      // The filter it counted has to still be the filter, or the number
      // describes a result nobody is looking at.
      if (Number.isFinite(value) && get().browseState(key).where === browse.where) {
        patchBrowse(set, key, { totalRows: value });
      }
    } catch {
      // A count is a convenience, never the reason a table fails to open: the
      // pager walks by probe row without it and says "1–200+" instead of
      // "page 1 of 9". An error banner here would be about the pager's
      // decoration, over rows that loaded fine.
    } finally {
      pendingCountQueries.delete(key);
    }
  },

  setPageKey: (key, columns) =>
    set((state) => ({ pageKeys: { ...state.pageKeys, [key]: columns } })),

  closeBrowse: (key) =>
    set((state) => {
      if (!(key in state.browse) && !(key in state.pageKeys)) {
        return state;
      }
      const { [key]: _closed, ...browse } = state.browse;
      // A17: the key goes with the rows. It is this table's state too, and
      // leaving it behind both grows the map forever and lets a reopen order
      // its first page by a key that has since been re-read.
      const { [key]: _key, ...pageKeys } = state.pageKeys;
      return { browse, pageKeys };
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
      version: 2,
      migrate: (persisted) => {
        const migrated = migrateFoldersToIds(persisted as never);
        return { ...migrated, folders: dropSeededFolders(migrated.folders) };
      },
      // Only the cosmetic grouping persists. Profiles, schema and results all
      // come from the bridge; caching them here would let a stale copy outlive
      // the thing it describes.
      partialize: (state) => ({
        folders: state.folders,
        expandedFolders: state.expandedFolders,
        browsePageSize: state.browsePageSize,
      }),
    },
  ),
);
