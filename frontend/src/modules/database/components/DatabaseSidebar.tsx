import { useState } from "react";
import {
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
  X,
  Crosshair,
  Trash2,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { useDatabaseStore } from "../databaseStore";
import { getEngineMeta } from "./EngineBadge";
import { deleteProfile } from "../../../bridge/database";

/**
 * "indexed 2 hours ago" rather than a wall-clock time: the question the label
 * answers is whether the index is stale, and a timestamp makes the reader do
 * that arithmetic themselves.
 */
function relativeTime(iso: string): string {
  const then = new Date(iso).getTime();
  if (!Number.isFinite(then)) return "recently";
  const seconds = Math.max(0, Math.round((Date.now() - then) / 1000));
  if (seconds < 60) return "just now";
  const minutes = Math.round(seconds / 60);
  if (minutes < 60) return `${minutes} minute${minutes === 1 ? "" : "s"} ago`;
  const hours = Math.round(minutes / 60);
  if (hours < 24) return `${hours} hour${hours === 1 ? "" : "s"} ago`;
  const days = Math.round(hours / 24);
  return `${days} day${days === 1 ? "" : "s"} ago`;
}

/** Both section headings were hand-rolled and drifted apart — different sizes,
 *  different ways of showing a count. One shape, used twice. */
function SectionLabel({ label, count }: { label: string; count?: number }) {
  return (
    <div className="flex items-baseline gap-1.5 min-w-0">
      <span className="text-[10px] font-semibold uppercase tracking-[0.08em] text-text-tertiary">
        {label}
      </span>
      {count !== undefined && (
        <span className="text-[10px] font-mono text-text-tertiary/70">{count}</span>
      )}
    </div>
  );
}

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
    selectedSchema,
    setSelectedSchema,
    selectedTable,
    setSelectedTable,
    tableColumnsMap,
    setSql,
    sql,
    setShowNewConnectionDialog,
    setShowConnectionOptionsDialog,
  } = useDatabaseStore();

  const [filterQuery, setFilterQuery] = useState("");

  const allTableNames = Object.values(schemaSnapshot?.namespace || {}).flatMap((tables) =>
    Object.keys(tables || {}),
  );
  const totalTables = allTableNames.length;

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

  // Searching 161 rows and being shown a list with no idea how much it cut is
  // the part that made the box feel like it was not working.
  const matchingTables = filterQuery ? allTableNames.filter(filterMatch).length : totalTables;

  function handleInsertTable(tableName: string) {
    if (!sql || sql.trim() === "SELECT 1 AS n, 'hello' AS greeting;") {
      setSql(`SELECT * FROM ${tableName} LIMIT 50;`);
    } else {
      setSql(`${sql} ${tableName}`);
    }
  }

  return (
    <div className="flex h-full flex-col select-none">
      {/* One search, at the top, for the whole sidebar. It used to sit under the
          CONNECTIONS heading while also filtering the 161 tables two sections
          below — the box and the list it acted on were nowhere near each
          other. */}
      <div className="p-3 pb-2.5">
        <div className="relative">
          <Search className="w-3.5 h-3.5 absolute left-2.5 top-1/2 -translate-y-1/2 text-text-tertiary" />
          <Input
            value={filterQuery}
            onChange={(e) => setFilterQuery(e.target.value)}
            placeholder="Search connections & tables…"
            className="h-8 pl-8 pr-7 text-xs bg-surface-raised border-border"
          />
          {filterQuery && (
            <button
              type="button"
              onClick={() => setFilterQuery("")}
              className="absolute right-2 top-1/2 -translate-y-1/2 p-0.5 rounded text-text-tertiary hover:text-text-primary"
              title="Clear search"
            >
              <X className="w-3 h-3" />
            </button>
          )}
        </div>
      </div>

      <div className="px-3 pb-1.5 flex items-center justify-between">
        <SectionLabel label="Connections" count={profiles.length} />
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

      {/* Connections size to their content and stop. They used to take flex-1
          against the schema tree's fixed half, so a single connection left an
          empty column the height of the window while 161 tables scrolled in
          the bottom half. */}
      <div className="shrink-0 max-h-[40%] overflow-y-auto px-2 pb-2 space-y-2 border-b border-border">
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

      {/* The schema tree takes whatever is left — it is the list with 161 rows
          in it, so it is the one that needs the room. */}
      <div className="flex-1 flex flex-col min-h-0">
        <div className="px-3 py-2 border-b border-border/60 flex items-center justify-between gap-2">
          <SectionLabel label="Tables" count={totalTables} />

          {/* Staleness is the only thing the timestamp is asked, so it reads as
              a relative age and hides behind the button it belongs to once the
              rail gets narrow. */}
          {selectedProfileId ? (
            <div className="flex items-center gap-1 min-w-0">
              {filterQuery ? (
                <span className="text-[10px] font-mono text-text-tertiary truncate">
                  {matchingTables} match
                </span>
              ) : (
                <span className="text-[10px] text-text-tertiary truncate">
                  {schemaSnapshot?.indexedAt
                    ? relativeTime(schemaSnapshot.indexedAt)
                    : "not indexed"}
                </span>
              )}
              <Button
                variant="ghost"
                size="icon"
                className="h-6 w-6 text-text-tertiary hover:text-text-primary shrink-0"
                onClick={() => void refreshSchema(selectedProfileId)}
                disabled={indexingSchema}
                title={
                  schemaSnapshot?.indexedAt
                    ? `Re-index schema — last indexed ${relativeTime(schemaSnapshot.indexedAt)}`
                    : "Index schema"
                }
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
                          const isFocused =
                            selectedSchema === schemaName && selectedTable === tableName;
                          const loadedCols = tableColumnsMap[tableKey];

                          return (
                            <div key={tableName} className="space-y-0.5">
                              <div
                                className={`w-full flex items-center gap-1.5 px-1.5 py-1 rounded text-xs cursor-pointer group ${
                                  isFocused
                                    ? "bg-surface-raised text-action-primary"
                                    : "text-text-primary hover:bg-surface-raised"
                                }`}
                                onClick={() => {
                                  // Clicking a table is also how the ERD is
                                  // focused — picking one out of 161 in a
                                  // dropdown is not a real gesture.
                                  setSelectedSchema(schemaName);
                                  setSelectedTable(tableName);
                                  void toggleTableExpand(schemaName, tableName);
                                }}
                                onDoubleClick={() => handleInsertTable(tableName)}
                              >
                                {isTableExpanded ? (
                                  <ChevronDown className="w-2.5 h-2.5 text-text-tertiary" />
                                ) : (
                                  <ChevronRight className="w-2.5 h-2.5 text-text-tertiary" />
                                )}
                                <TableIcon
                                  className={`w-3 h-3 ${
                                    isFocused ? "text-action-primary" : "text-sky-400"
                                  }`}
                                />
                                <span className="font-mono text-[11px] flex-1 truncate text-left">
                                  {tableName}
                                </span>
                                {/* Clicking a row focuses the ERD. Without a
                                    mark the diagram just changes and nothing
                                    says which row did it. */}
                                {isFocused ? (
                                  <Crosshair
                                    className="w-3 h-3 text-action-primary shrink-0"
                                    aria-label="Focused in the ERD"
                                  />
                                ) : (
                                  <span className="text-[9px] text-text-tertiary opacity-0 group-hover:opacity-100">
                                    {columnNames ? `${columnNames.length} cols` : ""}
                                  </span>
                                )}
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
