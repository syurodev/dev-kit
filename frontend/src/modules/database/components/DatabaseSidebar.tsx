import { useState } from "react";
import {
  Plus,
  RefreshCw,
  Folder,
  ChevronRight,
  ChevronDown,
  Table as TableIcon,
  Columns,
  Key,
  Shield,
  Radio,
  Search,
  Settings2,
  Pencil,
  Copy,
  FolderPlus,
  FolderInput,
  X,
  Crosshair,
  Trash2,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuShortcut,
  ContextMenuSub,
  ContextMenuSubContent,
  ContextMenuSubTrigger,
  ContextMenuTrigger,
} from "@/components/ui/context-menu";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { SidebarSection } from "@/components/ui/sidebar-shell";
import { useDatabaseStore, type DatabaseFolder } from "../databaseStore";
import { FolderIcon } from "../folderIcons";
import { EditFolderDialog } from "./EditFolderDialog";
import { DeleteProfileDialog } from "./DeleteProfileDialog";
import { getEngineMeta } from "./EngineBadge";
import { deleteProfile } from "../../../bridge/database";
import { copyText } from "../../../bridge/clipboard";

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

export function DatabaseSidebar() {
  const {
    profiles,
    selectedProfileId,
    setSelectedProfileId,
    loadingProfiles,
    fetchProfiles,
    folders,
    expandedFolders,
    setActiveTab,
    toggleFolder,
    createFolder,
    updateFolder,
    deleteFolder,
    assignProfileToFolder,
    openSidebarSections,
    toggleSidebarSection,
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
  const [editingFolderId, setEditingFolderId] = useState("");
  const [deletingProfileId, setDeletingProfileId] = useState("");
  // Open unless explicitly closed, so a store written before this existed does
  // not come back with both panes shut.
  const openSections = {
    connections: openSidebarSections.connections !== false,
    tables: openSidebarSections.tables !== false,
  };

  const allTableNames = Object.values(schemaSnapshot?.namespace || {}).flatMap((tables) =>
    Object.keys(tables || {}),
  );
  const totalTables = allTableNames.length;

  // Group profiles into folders
  const assignedProfileIds = new Set<string>();
  const folderEntries = folders.map((folder) => {
    const list = profiles.filter((p) => folder.profileIds.includes(p.id));
    list.forEach((p) => assignedProfileIds.add(p.id));
    return { folder, profiles: list };
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
      <div className="p-3 pb-3">
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
              className="absolute right-2 top-1/2 -translate-y-1/2 p-1 rounded-md text-text-tertiary hover:text-text-primary"
              title="Clear search"
            >
              <X className="w-3 h-3" />
            </button>
          )}
        </div>
      </div>

      <div className="px-3 pb-2 flex items-center justify-between">
        <SidebarSection
          label="Connections"
          count={profiles.length}
          open={openSections.connections}
          onToggle={() => toggleSidebarSection("connections")}
        />
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
              variant="ghost"
              size="icon"
              className="h-6 w-6 text-text-secondary hover:text-text-primary"
              onClick={() => createFolder(`Folder ${folders.length + 1}`)}
              title="New folder"
            >
              <FolderPlus className="w-3.5 h-3.5" />
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
      {openSections.connections && (
      <div
        className={`overflow-y-auto px-2 pb-2 space-y-2 ${
          openSections.tables ? "shrink-0 max-h-[40%]" : "flex-1 min-h-0"
        }`}
      >
        {/* Folders */}
        {folderEntries.map(({ folder, profiles: folderProfiles }) => {
          const visibleProfiles = folderProfiles.filter(
            (p) => filterMatch(p.name) || filterMatch(p.database) || filterMatch(p.host)
          );
          if (filterQuery && visibleProfiles.length === 0) return null;

          const isExpanded = expandedFolders[folder.id] !== false;
          return (
            <div key={folder.id} className="space-y-1">
              <ContextMenu>
              <ContextMenuTrigger asChild>
              <div className="w-full flex items-center gap-2 px-2 py-1 rounded-md text-xs text-text-secondary hover:bg-surface-raised transition-colors group">
                <button
                  type="button"
                  onClick={() => toggleFolder(folder.id)}
                  className="flex items-center gap-2 flex-1 min-w-0 text-left hover:text-text-primary"
                >
                  {isExpanded ? (
                    <ChevronDown className="w-3 h-3 text-text-tertiary shrink-0" />
                  ) : (
                    <ChevronRight className="w-3 h-3 text-text-tertiary shrink-0" />
                  )}
                  <FolderIcon
                    name={folder.icon}
                    className="w-3.5 h-3.5 shrink-0 text-action-primary"
                  />
                  <span className="font-medium text-[11px] truncate">{folder.name}</span>
                </button>
                <span className="text-[10px] text-text-tertiary">{folderProfiles.length}</span>
                <button
                  type="button"
                  onClick={() => setEditingFolderId(folder.id)}
                  className="p-1 rounded-md text-text-tertiary hover:text-text-primary opacity-0 group-hover:opacity-100"
                  title={`Edit ${folder.name}`}
                >
                  <Pencil className="w-3 h-3" />
                </button>
              </div>
              </ContextMenuTrigger>
              <ContextMenuContent>
                <ContextMenuItem onSelect={() => setEditingFolderId(folder.id)}>
                  <Pencil />
                  Edit folder
                </ContextMenuItem>
                <ContextMenuItem onSelect={() => createFolder(`Folder ${folders.length + 1}`)}>
                  <FolderPlus />
                  New folder
                </ContextMenuItem>
                <ContextMenuSeparator />
                <ContextMenuItem variant="destructive" onSelect={() => deleteFolder(folder.id)}>
                  <Trash2 />
                  Delete folder
                </ContextMenuItem>
              </ContextMenuContent>
              </ContextMenu>

              {isExpanded && (
                <div className="pl-4 space-y-1">
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
                      onDelete={() => setDeletingProfileId(p.id)}
                      folders={folders}
                      currentFolderId={folder.id}
                      onMove={(folderId) => assignProfileToFolder(p.id, folderId)}
                    />
                  ))}
                </div>
              )}
            </div>
          );
        })}

        {/* Ungrouped Profiles */}
        {ungroupedProfiles.length > 0 && (
          <div className="space-y-1 pt-1">
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
                  onDelete={() => setDeletingProfileId(p.id)}
                  folders={folders}
                  currentFolderId=""
                  onMove={(folderId) => assignProfileToFolder(p.id, folderId)}
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
      )}

      {/* The schema tree takes whatever is left — it is the list with 161 rows
          in it, so it is the one that needs the room. */}
      <div
        className={`flex flex-col border-t border-border ${
          openSections.tables ? "flex-1 min-h-0" : "shrink-0"
        }`}
      >
        <div className="px-3 py-2 flex items-center justify-between gap-2">
          <SidebarSection
            label="Tables"
            count={filterQuery ? matchingTables : totalTables}
            open={openSections.tables}
            onToggle={() => toggleSidebarSection("tables")}
          />

          {/* Staleness is the only thing the timestamp is asked, so it reads as
              a relative age and hides behind the button it belongs to once the
              rail gets narrow. */}
          {selectedProfileId ? (
            <div className="flex items-center gap-1 min-w-0">
              <span className="text-[10px] text-text-tertiary truncate">
                {schemaSnapshot?.indexedAt
                  ? relativeTime(schemaSnapshot.indexedAt)
                  : "not indexed"}
              </span>
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

        {openSections.tables && (
        <div className="flex-1 overflow-y-auto p-2 space-y-1 border-t border-border/60">
          {selectedProfileId && schemaSnapshot?.namespace ? (
            Object.entries(schemaSnapshot.namespace).map(([schemaName, tables]) => {
              const isSchemaExpanded = expandedSchemas[schemaName] ?? true;
              const tableEntries = Object.entries(tables || {});

              return (
                <div key={schemaName} className="space-y-1">
                  <button
                    type="button"
                    onClick={() => toggleSchemaExpand(schemaName)}
                    className="w-full flex items-center gap-2 px-2 py-1 rounded-md text-xs text-text-secondary hover:text-text-primary hover:bg-surface-raised transition-colors"
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
                    <div className="pl-4 space-y-1">
                      {tableEntries
                        .filter(([tableName]) => filterMatch(tableName))
                        .map(([tableName, columnNames]) => {
                          const tableKey = `${schemaName}.${tableName}`;
                          const isTableExpanded = expandedTables[tableKey];
                          const isFocused =
                            selectedSchema === schemaName && selectedTable === tableName;
                          const loadedCols = tableColumnsMap[tableKey];

                          return (
                            <div key={tableName} className="space-y-1">
                              <ContextMenu>
                              <ContextMenuTrigger asChild>
                              <div
                                className={`w-full flex items-center gap-2 px-2 py-1 rounded-md text-xs cursor-pointer group ${
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
                              </ContextMenuTrigger>
                              {/* Inserting a SELECT was double-click only —
                                  an action with no visible affordance at all.
                                  The menu names it and keeps the shortcut. */}
                              <ContextMenuContent>
                                <ContextMenuItem onSelect={() => handleInsertTable(tableName)}>
                                  Select 50 rows
                                  <ContextMenuShortcut>Double-click</ContextMenuShortcut>
                                </ContextMenuItem>
                                <ContextMenuItem
                                  onSelect={() => {
                                    setSelectedSchema(schemaName);
                                    setSelectedTable(tableName);
                                    setActiveTab("erd");
                                  }}
                                >
                                  <Crosshair />
                                  Focus in ERD
                                </ContextMenuItem>
                                <ContextMenuItem onSelect={() => void copyText(tableName)}>
                                  <Copy />
                                  Copy table name
                                </ContextMenuItem>
                                <ContextMenuSeparator />
                                <ContextMenuItem
                                  onSelect={() => void refreshSchema(selectedProfileId)}
                                  disabled={indexingSchema}
                                >
                                  <RefreshCw />
                                  Re-index schema
                                </ContextMenuItem>
                              </ContextMenuContent>
                              </ContextMenu>

                              {isTableExpanded && (
                                <div className="pl-4 space-y-1 border-l border-border/40 ml-3 my-1">
                                  {loadedCols ? (
                                    loadedCols.map((col) => (
                                      <div
                                        key={col.name}
                                        className="flex items-center justify-between gap-1 px-1 py-1 text-[10px] font-mono text-text-secondary hover:text-text-primary"
                                      >
                                        <div className="flex items-center gap-1 truncate">
                                          {col.name.endsWith("_id") || col.name === "id" ? (
                                            <Key className="w-2.5 h-2.5 text-amber-400 shrink-0" />
                                          ) : (
                                            <Columns className="w-2.5 h-2.5 text-text-tertiary shrink-0" />
                                          )}
                                          <span className="truncate">{col.name}</span>
                                        </div>
                                        <span className="text-[9px] text-text-tertiary bg-surface-raised px-1 rounded-md">
                                          {col.dataType}
                                        </span>
                                      </div>
                                    ))
                                  ) : (
                                    <div className="text-[10px] text-text-tertiary py-1">
                                      {(columnNames || []).map((colName) => (
                                        <div key={colName} className="px-1 py-1 font-mono truncate">
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
                  <p className="flex items-center justify-center gap-2">
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
        )}
      </div>

      <DeleteProfileDialog
        profile={profiles.find((p) => p.id === deletingProfileId) ?? null}
        onClose={() => setDeletingProfileId("")}
        onConfirm={async () => {
          const id = deletingProfileId;
          setDeletingProfileId("");
          await deleteProfile(id);
          void fetchProfiles();
        }}
      />

      <EditFolderDialog
        folder={folders.find((f) => f.id === editingFolderId) ?? null}
        onClose={() => setEditingFolderId("")}
        onSave={(patch) => {
          updateFolder(editingFolderId, patch);
          setEditingFolderId("");
        }}
        onDelete={() => {
          deleteFolder(editingFolderId);
          setEditingFolderId("");
        }}
      />
    </div>
  );
}

function ProfileItem({
  profile,
  isSelected,
  onSelect,
  onOptions,
  onDelete,
  folders,
  currentFolderId,
  onMove,
}: {
  profile: any;
  isSelected: boolean;
  onSelect: () => void;
  onOptions: () => void;
  onDelete: () => void;
  folders: DatabaseFolder[];
  currentFolderId: string;
  onMove: (folderId: string) => void;
}) {
  const engineMeta = getEngineMeta(profile.engine);
  const isProd = profile.env?.toLowerCase() === "prod" || profile.env?.toLowerCase() === "production";

  return (
    <ContextMenu>
    <ContextMenuTrigger asChild>
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
          <div className="flex items-center gap-2">
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
          <div className="text-[10px] text-text-tertiary truncate font-mono mt-1">
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

        <div className="opacity-0 group-hover:opacity-100 flex items-center gap-1 ml-1">
          {/* Figma drew the folder dialog but not the way a profile gets into
              one. A menu is the smallest thing that works and reads: the list
              is short, and it says where the profile is now. */}
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <button
                type="button"
                className="p-1 text-text-tertiary hover:text-text-primary rounded-md"
                onClick={(e) => e.stopPropagation()}
                title="Move to folder"
              >
                <FolderInput className="w-3 h-3" />
              </button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" onClick={(e) => e.stopPropagation()}>
              <DropdownMenuItem onSelect={() => onMove("")} disabled={!currentFolderId}>
                <Folder />
                Ungrouped
              </DropdownMenuItem>
              {folders.map((f) => (
                <DropdownMenuItem
                  key={f.id}
                  onSelect={() => onMove(f.id)}
                  disabled={f.id === currentFolderId}
                >
                  <FolderIcon name={f.icon} className="size-4" />
                  {f.name}
                </DropdownMenuItem>
              ))}
            </DropdownMenuContent>
          </DropdownMenu>
          <button
            type="button"
            className="p-1 text-text-tertiary hover:text-text-primary rounded-md"
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
            className="p-1 text-text-tertiary hover:text-status-danger rounded-md"
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
    </ContextMenuTrigger>

    <ContextMenuContent>
      <ContextMenuSub>
        <ContextMenuSubTrigger>
          <FolderInput />
          Move to folder
        </ContextMenuSubTrigger>
        <ContextMenuSubContent>
          <ContextMenuItem onSelect={() => onMove("")} disabled={!currentFolderId}>
            <Folder />
            Ungrouped
          </ContextMenuItem>
          {folders.map((f) => (
            <ContextMenuItem
              key={f.id}
              onSelect={() => onMove(f.id)}
              disabled={f.id === currentFolderId}
            >
              <FolderIcon name={f.icon} className="size-4" />
              {f.name}
            </ContextMenuItem>
          ))}
        </ContextMenuSubContent>
      </ContextMenuSub>
      <ContextMenuItem onSelect={onOptions}>
        <Settings2 />
        Connection options
      </ContextMenuItem>
      <ContextMenuItem
        onSelect={() =>
          void copyText(
            `${profile.username}@${profile.host}:${profile.port}/${profile.database}`,
          )
        }
      >
        <Copy />
        Copy connection target
      </ContextMenuItem>
      <ContextMenuSeparator />
      <ContextMenuItem variant="destructive" onSelect={onDelete}>
        <Trash2 />
        Delete connection
      </ContextMenuItem>
    </ContextMenuContent>
    </ContextMenu>
  );
}
