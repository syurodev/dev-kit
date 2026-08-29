import { useState } from "react";
import {
  Database,
  Plus,
  RefreshCw,
  Folder,
  FolderOpen,
  ChevronRight,
  ChevronDown,
  Table as TableIcon,
  Columns,
  Key,
  Shield,
  Radio,
  Search,
  Settings2,
  Trash2,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { useDatabaseStore } from "../databaseStore";
import { getEngineMeta } from "./EngineBadge";
import { deleteProfile } from "../../../bridge/database";

export function DatabaseSidebar() {
  const {
    profiles,
    selectedProfileId,
    setSelectedProfileId,
    loadingProfiles,
    fetchProfiles,
    folders,
    expandedFolders,
    toggleFolder,
    schemaSnapshot,
    indexingSchema,
    refreshSchema,
    expandedSchemas,
    toggleSchemaExpand,
    expandedTables,
    toggleTableExpand,
    tableColumnsMap,
    setSql,
    sql,
    setShowNewConnectionDialog,
    setShowConnectionOptionsDialog,
  } = useDatabaseStore();

  const [filterQuery, setFilterQuery] = useState("");

  const selectedProfile = profiles.find((p) => p.id === selectedProfileId);

  // Group profiles into folders
  const assignedProfileIds = new Set<string>();
  const folderEntries = Object.entries(folders).map(([name, ids]) => {
    const list = profiles.filter((p) => ids.includes(p.id));
    list.forEach((p) => assignedProfileIds.add(p.id));
    return { name, profiles: list };
  });
  const ungroupedProfiles = profiles.filter((p) => !assignedProfileIds.has(p.id));

  // Filtered lists
  const filterMatch = (text: string) =>
    !filterQuery || text.toLowerCase().includes(filterQuery.toLowerCase());

  function handleInsertTable(tableName: string) {
    if (!sql || sql.trim() === "SELECT 1 AS n, 'hello' AS greeting;") {
      setSql(`SELECT * FROM ${tableName} LIMIT 50;`);
    } else {
      setSql(`${sql} ${tableName}`);
    }
  }

  return (
    <div className="flex h-full flex-col select-none">
      {/* Search and Action Bar */}
      <div className="p-3 border-b border-border flex flex-col gap-2">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-1.5 font-semibold text-xs text-text-primary">
            <Database className="w-3.5 h-3.5 text-action-primary" />
            <span>Connections</span>
            <span className="text-[11px] text-text-tertiary">({profiles.length})</span>
          </div>
          <div className="flex items-center gap-1">
            <Button
              variant="ghost"
              size="icon"
              className="h-6 w-6 text-text-secondary hover:text-text-primary"
              onClick={() => void fetchProfiles()}
              disabled={loadingProfiles}
              title="Refresh connections"
            >
              <RefreshCw className={`w-3.5 h-3.5 ${loadingProfiles ? "animate-spin" : ""}`} />
            </Button>
            <Button
              variant="secondary"
              size="sm"
              className="h-6 px-2 text-xs gap-1"
              onClick={() => setShowNewConnectionDialog(true)}
              title="New connection"
            >
              <Plus className="w-3.5 h-3.5" />
              <span>New</span>
            </Button>
          </div>
        </div>

        <div className="relative">
          <Search className="w-3 h-3 absolute left-2.5 top-1/2 -translate-y-1/2 text-text-tertiary" />
          <Input
            value={filterQuery}
            onChange={(e) => setFilterQuery(e.target.value)}
            placeholder="Filter profiles & tables…"
            className="h-7 pl-7 text-[11px] bg-surface-raised border-border"
          />
        </div>
      </div>

      {/* Connections List */}
      <div className="flex-1 overflow-y-auto min-h-[160px] p-2 space-y-2 border-b border-border">
        {/* Folders */}
        {folderEntries.map(({ name, profiles: folderProfiles }) => {
          const visibleProfiles = folderProfiles.filter(
            (p) => filterMatch(p.name) || filterMatch(p.database) || filterMatch(p.host)
          );
          if (filterQuery && visibleProfiles.length === 0) return null;

          const isExpanded = expandedFolders[name] ?? true;
          return (
            <div key={name} className="space-y-0.5">
              <button
                type="button"
                onClick={() => toggleFolder(name)}
                className="w-full flex items-center gap-1.5 px-2 py-1 rounded text-xs text-text-secondary hover:text-text-primary hover:bg-surface-raised transition-colors group"
              >
                {isExpanded ? (
                  <ChevronDown className="w-3 h-3 text-text-tertiary" />
                ) : (
                  <ChevronRight className="w-3 h-3 text-text-tertiary" />
                )}
                {isExpanded ? (
                  <FolderOpen className="w-3.5 h-3.5 text-action-primary" />
                ) : (
                  <Folder className="w-3.5 h-3.5 text-text-tertiary group-hover:text-action-primary" />
                )}
                <span className="font-medium text-[11px] truncate flex-1 text-left">{name}</span>
                <span className="text-[10px] text-text-tertiary">{folderProfiles.length}</span>
              </button>

              {isExpanded && (
                <div className="pl-4 space-y-0.5">
                  {visibleProfiles.map((p) => (
                    <ProfileItem
                      key={p.id}
                      profile={p}
                      isSelected={p.id === selectedProfileId}
                      onSelect={() => setSelectedProfileId(p.id)}
                      onOptions={() => {
                        setSelectedProfileId(p.id);
                        setShowConnectionOptionsDialog(true);
                      }}
                      onDelete={async () => {
                        await deleteProfile(p.id);
                        void fetchProfiles();
                      }}
                    />
                  ))}
                </div>
              )}
            </div>
          );
        })}

        {/* Ungrouped Profiles */}
        {ungroupedProfiles.length > 0 && (
          <div className="space-y-0.5 pt-1">
            {folderEntries.length > 0 && (
              <span className="px-2 text-[10px] font-semibold text-text-tertiary uppercase tracking-wider">
                Ungrouped
              </span>
            )}
            {ungroupedProfiles
              .filter((p) => filterMatch(p.name) || filterMatch(p.database) || filterMatch(p.host))
              .map((p) => (
                <ProfileItem
                  key={p.id}
                  profile={p}
                  isSelected={p.id === selectedProfileId}
                  onSelect={() => setSelectedProfileId(p.id)}
                  onOptions={() => {
                    setSelectedProfileId(p.id);
                    setShowConnectionOptionsDialog(true);
                  }}
                  onDelete={async () => {
                    await deleteProfile(p.id);
                    void fetchProfiles();
                  }}
                />
              ))}
          </div>
        )}

        {profiles.length === 0 && !loadingProfiles && (
          <div className="text-center py-6 px-4 text-xs text-text-tertiary">
            <p>No connections configured.</p>
            <Button
              variant="outline"
              size="sm"
              className="mt-2 text-xs"
              onClick={() => setShowNewConnectionDialog(true)}
            >
              <Plus className="w-3 h-3 mr-1" /> Add Profile
            </Button>
          </div>
        )}
      </div>

      {/* Schema Tree Section (F3) */}
      <div className="h-1/2 flex flex-col">
        <div className="p-2.5 border-b border-border/60 flex items-center justify-between bg-surface-raised/40">
          <div className="flex items-center gap-1.5">
            <span className="text-[11px] font-semibold text-text-secondary">Schema Explorer</span>
            {selectedProfile ? (
              <span className="text-[10px] text-text-tertiary truncate max-w-[110px]">
                ({selectedProfile.database})
              </span>
            ) : null}
          </div>

          {selectedProfileId ? (
            <div className="flex items-center gap-1">
              <span className="text-[10px] text-text-tertiary">
                {schemaSnapshot?.indexedAt
                  ? `Indexed ${new Date(schemaSnapshot.indexedAt).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}`
                  : "Unindexed"}
              </span>
              <Button
                variant="ghost"
                size="icon"
                className="h-5 w-5 text-text-tertiary hover:text-text-primary"
                onClick={() => void refreshSchema(selectedProfileId)}
                disabled={indexingSchema}
                title="Re-index schema (manual refresh)"
              >
                <RefreshCw className={`w-3 h-3 ${indexingSchema ? "animate-spin" : ""}`} />
              </Button>
            </div>
          ) : null}
        </div>

        <div className="flex-1 overflow-y-auto p-2 space-y-1">
          {selectedProfileId && schemaSnapshot?.namespace ? (
            Object.entries(schemaSnapshot.namespace).map(([schemaName, tables]) => {
              const isSchemaExpanded = expandedSchemas[schemaName] ?? true;
              const tableEntries = Object.entries(tables || {});

              return (
                <div key={schemaName} className="space-y-0.5">
                  <button
                    type="button"
                    onClick={() => toggleSchemaExpand(schemaName)}
                    className="w-full flex items-center gap-1.5 px-1.5 py-1 rounded text-xs text-text-secondary hover:text-text-primary hover:bg-surface-raised transition-colors"
                  >
                    {isSchemaExpanded ? (
                      <ChevronDown className="w-3 h-3 text-text-tertiary" />
                    ) : (
                      <ChevronRight className="w-3 h-3 text-text-tertiary" />
                    )}
                    <Folder className="w-3.5 h-3.5 text-amber-500" />
                    <span className="font-mono text-[11px] font-medium flex-1 text-left">
                      {schemaName}
                    </span>
                    <span className="text-[10px] text-text-tertiary">{tableEntries.length}</span>
                  </button>

                  {isSchemaExpanded && (
                    <div className="pl-3.5 space-y-0.5">
                      {tableEntries
                        .filter(([tableName]) => filterMatch(tableName))
                        .map(([tableName, columnNames]) => {
                          const tableKey = `${schemaName}.${tableName}`;
                          const isTableExpanded = expandedTables[tableKey];
                          const loadedCols = tableColumnsMap[tableKey];

                          return (
                            <div key={tableName} className="space-y-0.5">
                              <div
                                className="w-full flex items-center gap-1.5 px-1.5 py-1 rounded text-xs text-text-primary hover:bg-surface-raised cursor-pointer group"
                                onClick={() => void toggleTableExpand(schemaName, tableName)}
                                onDoubleClick={() => handleInsertTable(tableName)}
                              >
                                {isTableExpanded ? (
                                  <ChevronDown className="w-2.5 h-2.5 text-text-tertiary" />
                                ) : (
                                  <ChevronRight className="w-2.5 h-2.5 text-text-tertiary" />
                                )}
                                <TableIcon className="w-3 h-3 text-sky-400" />
                                <span className="font-mono text-[11px] flex-1 truncate text-left">
                                  {tableName}
                                </span>
                                <span className="text-[9px] text-text-tertiary opacity-0 group-hover:opacity-100">
                                  {columnNames ? `${columnNames.length} cols` : ""}
                                </span>
                              </div>

                              {isTableExpanded && (
                                <div className="pl-4 space-y-0.5 border-l border-border/40 ml-2.5 my-0.5">
                                  {loadedCols ? (
                                    loadedCols.map((col) => (
                                      <div
                                        key={col.name}
                                        className="flex items-center justify-between gap-1 px-1 py-0.5 text-[10px] font-mono text-text-secondary hover:text-text-primary"
                                      >
                                        <div className="flex items-center gap-1 truncate">
                                          {col.name.endsWith("_id") || col.name === "id" ? (
                                            <Key className="w-2.5 h-2.5 text-amber-400 shrink-0" />
                                          ) : (
                                            <Columns className="w-2.5 h-2.5 text-text-tertiary shrink-0" />
                                          )}
                                          <span className="truncate">{col.name}</span>
                                        </div>
                                        <span className="text-[9px] text-text-tertiary bg-surface-raised px-1 rounded">
                                          {col.dataType}
                                        </span>
                                      </div>
                                    ))
                                  ) : (
                                    <div className="text-[10px] text-text-tertiary py-1">
                                      {(columnNames || []).map((colName) => (
                                        <div key={colName} className="px-1 py-0.5 font-mono truncate">
                                          {colName}
                                        </div>
                                      ))}
                                    </div>
                                  )}
                                </div>
                              )}
                            </div>
                          );
                        })}
                    </div>
                  )}
                </div>
              );
            })
          ) : (
            <div className="text-center py-8 text-xs text-text-tertiary">
              {selectedProfileId ? (
                indexingSchema ? (
                  <p className="flex items-center justify-center gap-1.5">
                    <RefreshCw className="w-3 h-3 animate-spin" /> Indexing schema…
                  </p>
                ) : (
                  <p>Schema cache empty. Click refresh above to index.</p>
                )
              ) : (
                <p>Select a connection to view its schema.</p>
              )}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

function ProfileItem({
  profile,
  isSelected,
  onSelect,
  onOptions,
  onDelete,
}: {
  profile: any;
  isSelected: boolean;
  onSelect: () => void;
  onOptions: () => void;
  onDelete: () => void;
}) {
  const engineMeta = getEngineMeta(profile.engine);
  const isProd = profile.env?.toLowerCase() === "prod" || profile.env?.toLowerCase() === "production";

  return (
    <div
      className={`group relative flex items-center justify-between p-2 rounded-lg text-xs cursor-pointer transition-colors border ${
        isSelected
          ? "bg-surface-raised border-action-primary/40 text-text-primary shadow-xs"
          : "border-transparent text-text-secondary hover:bg-surface-raised/60 hover:text-text-primary"
      }`}
      onClick={onSelect}
    >
      <div className="flex items-start gap-2 truncate flex-1 min-w-0">
        <span className={`w-2 h-2 rounded-full mt-1 shrink-0 ${engineMeta.accentDot}`} />
        <div className="truncate flex-1">
          <div className="flex items-center gap-1.5">
            <span className="font-semibold text-xs text-text-primary truncate">{profile.name}</span>
            {isProd ? (
              <Badge variant="destructive" className="text-[9px] px-1 py-0 h-3.5">
                PROD
              </Badge>
            ) : profile.env ? (
              <Badge variant="outline" className="text-[9px] px-1 py-0 h-3.5 text-text-tertiary">
                {profile.env}
              </Badge>
            ) : null}
          </div>
          <div className="text-[10px] text-text-tertiary truncate font-mono mt-0.5">
            {profile.username}@{profile.host}:{profile.port}/{profile.database}
          </div>
        </div>
      </div>

      <div className="flex items-center gap-1 shrink-0">
        {profile.readOnly && (
          <span title="Safe-mode (Read-Only) active">
            <Shield className="w-3 h-3 text-status-warning" />
          </span>
        )}
        {profile.sshProfileId && (
          <span title="SSH Tunnel enabled">
            <Radio className="w-3 h-3 text-status-info" />
          </span>
        )}

        <div className="opacity-0 group-hover:opacity-100 flex items-center gap-0.5 ml-1">
          <button
            type="button"
            className="p-1 text-text-tertiary hover:text-text-primary rounded"
            onClick={(e) => {
              e.stopPropagation();
              onOptions();
            }}
            title="Connection options"
          >
            <Settings2 className="w-3 h-3" />
          </button>
          <button
            type="button"
            className="p-1 text-text-tertiary hover:text-status-danger rounded"
            onClick={(e) => {
              e.stopPropagation();
              onDelete();
            }}
            title="Delete profile"
          >
            <Trash2 className="w-3 h-3" />
          </button>
        </div>
      </div>
    </div>
  );
}
