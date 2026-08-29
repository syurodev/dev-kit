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

export type DatabaseStoreState = {
  profiles: ConnectionProfile[];
  selectedProfileId: string;
  activeTab: DatabaseTab;
  envTags: EnvTag[];
  sshProfiles: SSHProfile[];
  loadingProfiles: boolean;
  profileError: string;

  // Cosmetic folder groups (uistate)
  folders: Record<string, string[]>; // folderName -> profileIds
  expandedFolders: Record<string, boolean>;

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
  toggleFolder: (folderName: string) => void;
  assignProfileToFolder: (profileId: string, folderName: string) => void;

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

const DEFAULT_FOLDERS: Record<string, string[]> = { Production: [], Development: [] };

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
  expandedFolders: { "Production": true, "Development": true },

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
      tableColumnsMap: {},
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

  toggleFolder: (folderName: string) => {
    const current = get().expandedFolders;
    set({ expandedFolders: { ...current, [folderName]: !current[folderName] } });
  },

  assignProfileToFolder: (profileId: string, folderName: string) => {
    const folders = { ...get().folders };
    // Remove from other folders
    for (const key of Object.keys(folders)) {
      folders[key] = (folders[key] || []).filter((id) => id !== profileId);
    }
    if (folderName) {
      folders[folderName] = [...(folders[folderName] || []), profileId];
    }
    // persist middleware writes this through ui_state.
    set({ folders });
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

    set({
      runningQuery: true,
      queryStartTime: Date.now(),
      queryError: "",
      queryStatus: "",
      queryResult: null,
    });

    try {
      const result = await runQuery(selectedProfileId, sql);
      set({
        queryResult: result,
        runningQuery: false,
        queryStartTime: null,
        queryStatus: `Success: ${result.rowCount} row(s) returned`,
      });
    } catch (e) {
      set({
        queryError: errorMessage(e),
        runningQuery: false,
        queryStartTime: null,
        queryStatus: "",
      });
    }
  },

  clearQueryResult: () => set({ queryResult: null }),
  clearQueryError: () => set({ queryError: "" }),
    }),
    {
      name: "database.folders",
      storage: uiStateStorage,
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
