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
import { errorMessage } from "../../bridge/errors";

export type DatabaseTab = "console" | "erd" | "activity" | "history" | "saved";

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

export type DatabaseStoreState = {
  profiles: ConnectionProfile[];
  selectedProfileId: string;
  activeTab: DatabaseTab;
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
  // Percent of the console's height given to the editor.
  consoleEditorSize: number;

  // Schema state
  schemaSnapshot: SchemaSnapshot | null;
  indexingSchema: boolean;
  selectedSchema: string;
  selectedTable: string;
  expandedSchemas: Record<string, boolean>;
  expandedTables: Record<string, boolean>;
  tableColumnsMap: Record<string, ColumnInfo[]>; // "schema.table" -> columns

  // Query console state
  sql: string;
  queryResult: QueryResult | null;
  runningQuery: boolean;
  queryStartTime: number | null;
  queryError: string;
  queryStatus: string;
  /** Wall-clock time of the last successful run — Figma's "412 ms" beside
   * the row count (742:37). null until a query has finished. */
  queryDurationMs: number | null;

  // Selected cell for CellInspector
  inspectedCell: { columnName: string; value: string } | null;

  // Modals & Dialogs
  showNewConnectionDialog: boolean;
  showConnectionOptionsDialog: boolean;
  showExportModal: boolean;
  showSaveQueryModal: boolean;

  // Actions
  fetchProfiles: () => Promise<void>;
  setSelectedProfileId: (id: string) => void;
  setActiveTab: (tab: DatabaseTab) => void;
  setSql: (sql: string) => void;
  setInspectedCell: (cell: { columnName: string; value: string } | null) => void;
  setShowNewConnectionDialog: (show: boolean) => void;
  setShowConnectionOptionsDialog: (show: boolean) => void;
  setShowExportModal: (show: boolean) => void;
  setShowSaveQueryModal: (show: boolean) => void;

  // Folder actions
  toggleFolder: (folderId: string) => void;
  toggleProfileExpand: (profileId: string) => void;
  createFolder: (name: string) => void;
  updateFolder: (id: string, patch: { name?: string; icon?: string }) => void;
  deleteFolder: (id: string) => void;
  setConsoleEditorSize: (size: number) => void;
  assignProfileToFolder: (profileId: string, folderId: string) => void;

  // Schema actions
  loadSchemaSnapshot: (profileId: string) => Promise<void>;
  refreshSchema: (profileId: string) => Promise<void>;
  toggleSchemaExpand: (schema: string) => void;
  toggleTableExpand: (schema: string, table: string) => Promise<void>;
  setSelectedSchema: (schema: string) => void;
  setSelectedTable: (table: string) => void;

  // Query actions
  executeQuery: () => Promise<void>;
  clearQueryResult: () => void;
  clearQueryError: () => void;
};

const DEFAULT_FOLDERS: DatabaseFolder[] = [
  { id: "production", name: "Production", icon: "server", profileIds: [] },
  { id: "development", name: "Development", icon: "laptop", profileIds: [] },
];

export const useDatabaseStore = create<DatabaseStoreState>()(
  persist(
    (set, get) => ({
  profiles: [],
  selectedProfileId: "",
  activeTab: "console",
  envTags: [],
  sshProfiles: [],
  loadingProfiles: false,
  profileError: "",

  folders: DEFAULT_FOLDERS,
  expandedFolders: { production: true, development: true },
  expandedProfiles: {},
  consoleEditorSize: 30,

  schemaSnapshot: null,
  indexingSchema: false,
  selectedSchema: "",
  selectedTable: "",
  expandedSchemas: { "public": true },
  expandedTables: {},
  tableColumnsMap: {},

  sql: "SELECT 1 AS n, 'hello' AS greeting;",
  queryResult: null,
  runningQuery: false,
  queryStartTime: null,
  queryError: "",
  queryStatus: "",
  queryDurationMs: null,

  inspectedCell: null,

  showNewConnectionDialog: false,
  showConnectionOptionsDialog: false,
  showExportModal: false,
  showSaveQueryModal: false,

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
      queryResult: null,
      queryError: "",
      queryStatus: "",
      queryDurationMs: null,
      tableColumnsMap: {},
      // A focused table belongs to the database it was picked from.
      selectedTable: "",
      // Picking a connection is what indexes it, so the tree unfolds it in the
      // same click rather than asking for a second one.
      expandedProfiles: { ...get().expandedProfiles, [id]: true },
    });
    if (id) {
      void get().loadSchemaSnapshot(id);
    }
  },

  setActiveTab: (tab: DatabaseTab) => set({ activeTab: tab }),
  setSql: (sql: string) => set({ sql }),
  setInspectedCell: (cell) => set({ inspectedCell: cell }),
  setShowNewConnectionDialog: (show) => set({ showNewConnectionDialog: show }),
  setShowConnectionOptionsDialog: (show) => set({ showConnectionOptionsDialog: show }),
  setShowExportModal: (show) => set({ showExportModal: show }),
  setShowSaveQueryModal: (show) => set({ showSaveQueryModal: show }),

  toggleFolder: (folderId: string) => {
    const current = get().expandedFolders;
    set({ expandedFolders: { ...current, [folderId]: current[folderId] === false } });
  },

  toggleProfileExpand: (profileId: string) => {
    const current = get().expandedProfiles;
    set({ expandedProfiles: { ...current, [profileId]: !current[profileId] } });
  },

  setConsoleEditorSize: (size: number) => set({ consoleEditorSize: size }),

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
    set({ indexingSchema: true });
    try {
      const snap = await indexSchema(profileId);
      set({ schemaSnapshot: snap, indexingSchema: false });
      const schemas = Object.keys(snap.namespace || {});
      if (schemas.length > 0 && !get().selectedSchema) {
        set({ selectedSchema: schemas[0] });
      }
    } catch (e) {
      set({ indexingSchema: false, queryError: errorMessage(e) });
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

    if (isNowExpanded && !get().tableColumnsMap[key]) {
      const profileId = get().selectedProfileId;
      if (!profileId) return;
      try {
        const columns = await listColumns(profileId, schema, table);
        set((state) => ({
          tableColumnsMap: { ...state.tableColumnsMap, [key]: columns },
        }));
      } catch (e) {
        set({ queryError: errorMessage(e) });
      }
    }
  },

  setSelectedSchema: (schema: string) => set({ selectedSchema: schema }),
  setSelectedTable: (table: string) => set({ selectedTable: table }),

  executeQuery: async () => {
    const { selectedProfileId, sql } = get();
    if (!selectedProfileId) return;

    const startedAt = Date.now();
    set({
      runningQuery: true,
      queryStartTime: startedAt,
      queryError: "",
      queryStatus: "",
      queryDurationMs: null,
      queryResult: null,
    });

    try {
      const result = await runQuery(selectedProfileId, sql);
      set({
        queryResult: result,
        runningQuery: false,
        queryStartTime: null,
        queryStatus: `Success: ${result.rowCount} row(s) returned`,
        queryDurationMs: Date.now() - startedAt,
      });
    } catch (e) {
      set({
        queryError: errorMessage(e),
        runningQuery: false,
        queryStartTime: null,
        queryStatus: "",
        queryDurationMs: null,
      });
    }
  },

  clearQueryResult: () => set({ queryResult: null }),
  clearQueryError: () => set({ queryError: "" }),
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
        consoleEditorSize: state.consoleEditorSize,
      }),
    },
  ),
);
