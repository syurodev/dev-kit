import { useState } from "react";
import {
  Plus,
  RefreshCw,
  Folder,
  ChevronRight,
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
import { cn } from "@/lib/utils";
import { useDatabaseStore, type DatabaseFolder } from "../databaseStore";
import { FolderIcon } from "../folderIcons";
import { EditFolderDialog } from "./EditFolderDialog";
import { DeleteProfileDialog } from "./DeleteProfileDialog";
import { Chip, EngineMark } from "./EngineBadge";
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

/**
 * Figma `DB / Sidebar Content` (741:8). Two labelled sections. Under
 * CONNECTIONS, group rows at 32 (chevron 14 · glyph 16 · name in Body/13
 * Medium · count in Code/11) and profile rows at 48 (engine mark · name over
 * host in Code/11 · an env chip); the selected profile is raised with a
 * subtle stroke. Under SCHEMA, a tree of 32px rows indented 8 / 24 / 40 for
 * schema / table / column, each a chevron slot, a 16px glyph, the name, and
 * a Code/11 count or type on the right.
 *
 * The page groups connections by environment; DevKit groups them by the
 * folders the user makes. The rows are the page's, the model is DevKit's —
 * layout is what the Figma is for.
 */
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

  const profileItemProps = (p: (typeof profiles)[number], currentFolderId: string) => ({
    profile: p,
    isSelected: p.id === selectedProfileId,
    onSelect: () => setSelectedProfileId(p.id),
    onOptions: () => {
      setSelectedProfileId(p.id);
      setShowConnectionOptionsDialog(true);
    },
    onDelete: () => setDeletingProfileId(p.id),
    folders,
    currentFolderId,
    onMove: (folderId: string) => assignProfileToFolder(p.id, folderId),
  });

  return (
    <div className="flex h-full flex-col select-none">
      {/* One search, at the top, for the whole sidebar. It used to sit under the
          CONNECTIONS heading while also filtering the 161 tables two sections
          below — the box and the list it acted on were nowhere near each
          other. Not on the Figma page; kept, it is how 161 tables get found. */}
      <div className="p-2 pb-1">
        <div className="relative">
          <Search className="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-text-tertiary" />
          <Input
            value={filterQuery}
            onChange={(e) => setFilterQuery(e.target.value)}
            placeholder="Search connections & tables…"
            className="h-8 border-border-subtle bg-surface-well pr-7 pl-8"
          />
          {filterQuery && (
            <button
              type="button"
              onClick={() => setFilterQuery("")}
              className="absolute top-1/2 right-2 -translate-y-1/2 rounded-md p-1 text-text-tertiary hover:text-text-primary"
              title="Clear search"
            >
              <X className="size-3" />
            </button>
          )}
        </div>
      </div>

      <div className="flex items-center justify-between pr-2">
        <SidebarSection
          label="Connections"
          count={profiles.length}
          open={openSections.connections}
          onToggle={() => toggleSidebarSection("connections")}
        />
        <div className="flex items-center gap-1">
          <Button
            variant="ghost"
            size="icon-xs"
            onClick={() => void fetchProfiles()}
            disabled={loadingProfiles}
            title="Refresh connections"
          >
            <RefreshCw className={cn("size-3.5", loadingProfiles && "animate-spin")} />
          </Button>
          <Button
            variant="ghost"
            size="icon-xs"
            onClick={() => createFolder(`Folder ${folders.length + 1}`)}
            title="New folder"
          >
            <FolderPlus className="size-3.5" />
          </Button>
          <Button
            variant="outline"
            size="xs"
            className="bg-surface-raised"
            onClick={() => setShowNewConnectionDialog(true)}
            title="New connection"
          >
            <Plus className="size-3.5" />
            New
          </Button>
        </div>
      </div>

      {/* Connections size to their content and stop. They used to take flex-1
          against the schema tree's fixed half, so a single connection left an
          empty column the height of the window while 161 tables scrolled in
          the bottom half. */}
      {openSections.connections && (
        <div
          className={cn(
            "flex flex-col gap-0.5 overflow-y-auto px-2 pb-2",
            openSections.tables ? "max-h-[40%] shrink-0" : "min-h-0 flex-1",
          )}
        >
          {folderEntries.map(({ folder, profiles: folderProfiles }) => {
            const visibleProfiles = folderProfiles.filter(
              (p) => filterMatch(p.name) || filterMatch(p.database) || filterMatch(p.host),
            );
            if (filterQuery && visibleProfiles.length === 0) return null;

            const isExpanded = expandedFolders[folder.id] !== false;
            return (
              <div key={folder.id} className="flex flex-col gap-0.5">
                <ContextMenu>
                  <ContextMenuTrigger asChild>
                    {/* Group row (741:11): 32 tall, chevron · glyph · name · count. */}
                    <div className="group flex h-8 items-center gap-2 rounded-md pr-3 pl-2 text-text-secondary hover:bg-surface-raised">
                      <button
                        type="button"
                        onClick={() => toggleFolder(folder.id)}
                        className="flex min-w-0 flex-1 items-center gap-2 text-left"
                      >
                        <ChevronRight
                          className={cn(
                            "size-3.5 shrink-0 text-text-tertiary transition-transform",
                            isExpanded && "rotate-90",
                          )}
                        />
                        <FolderIcon name={folder.icon} className="size-4 shrink-0 text-text-secondary" />
                        <span className="truncate text-body-13-medium text-text-primary">
                          {folder.name}
                        </span>
                      </button>
                      <button
                        type="button"
                        onClick={() => setEditingFolderId(folder.id)}
                        className="rounded-md p-1 text-text-tertiary opacity-0 hover:text-text-primary focus-visible:opacity-100 group-hover:opacity-100"
                        title={`Edit ${folder.name}`}
                      >
                        <Pencil className="size-3" />
                      </button>
                      <span className="text-code-11 text-text-tertiary">{folderProfiles.length}</span>
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

                {isExpanded &&
                  visibleProfiles.map((p) => (
                    <ProfileItem key={p.id} {...profileItemProps(p, folder.id)} />
                  ))}
              </div>
            );
          })}

          {ungroupedProfiles.length > 0 && (
            <div className="flex flex-col gap-0.5">
              {folderEntries.length > 0 && (
                <span className="px-3 pt-2 pb-1 text-label-11-caps text-text-tertiary">Ungrouped</span>
              )}
              {ungroupedProfiles
                .filter((p) => filterMatch(p.name) || filterMatch(p.database) || filterMatch(p.host))
                .map((p) => (
                  <ProfileItem key={p.id} {...profileItemProps(p, "")} />
                ))}
            </div>
          )}

          {profiles.length === 0 && !loadingProfiles && (
            <div className="px-4 py-6 text-center text-body-12 text-text-tertiary">
              <p>No connections configured.</p>
              <Button
                variant="outline"
                size="sm"
                className="mt-2"
                onClick={() => setShowNewConnectionDialog(true)}
              >
                <Plus className="size-3" /> Add profile
              </Button>
            </div>
          )}
        </div>
      )}

      {/* The schema tree takes whatever is left — it is the list with 161 rows
          in it, so it is the one that needs the room. */}
      <div
        className={cn(
          "flex flex-col border-t border-border-subtle",
          openSections.tables ? "min-h-0 flex-1" : "shrink-0",
        )}
      >
        <div className="flex items-center justify-between gap-2 pr-2">
          <SidebarSection
            label="Schema"
            count={filterQuery ? matchingTables : totalTables}
            open={openSections.tables}
            onToggle={() => toggleSidebarSection("tables")}
          />

          {/* Staleness is the only thing the timestamp is asked, so it reads as
              a relative age and hides behind the button it belongs to once the
              rail gets narrow. */}
          {selectedProfileId ? (
            <div className="flex min-w-0 items-center gap-1">
              <span className="truncate text-caption-11 text-text-tertiary">
                {schemaSnapshot?.indexedAt
                  ? relativeTime(schemaSnapshot.indexedAt)
                  : "not indexed"}
              </span>
              <Button
                variant="ghost"
                size="icon-xs"
                className="shrink-0"
                onClick={() => void refreshSchema(selectedProfileId)}
                disabled={indexingSchema}
                title={
                  schemaSnapshot?.indexedAt
                    ? `Re-index schema — last indexed ${relativeTime(schemaSnapshot.indexedAt)}`
                    : "Index schema"
                }
              >
                <RefreshCw className={cn("size-3", indexingSchema && "animate-spin")} />
              </Button>
            </div>
          ) : null}
        </div>

        {openSections.tables && (
          <div className="flex flex-1 flex-col gap-0.5 overflow-y-auto px-2 pb-2">
            {selectedProfileId && schemaSnapshot?.namespace ? (
              Object.entries(schemaSnapshot.namespace).map(([schemaName, tables]) => {
                const isSchemaExpanded = expandedSchemas[schemaName] ?? true;
                const tableEntries = Object.entries(tables || {});

                return (
                  <div key={schemaName} className="flex flex-col gap-0.5">
                    {/* Schema row (741:73): indent 8. */}
                    <button
                      type="button"
                      onClick={() => toggleSchemaExpand(schemaName)}
                      className="flex h-8 w-full items-center gap-2 rounded-md pr-3 pl-2 text-text-secondary hover:bg-surface-raised hover:text-text-primary"
                    >
                      <ChevronRight
                        className={cn(
                          "size-3.5 shrink-0 text-text-tertiary transition-transform",
                          isSchemaExpanded && "rotate-90",
                        )}
                      />
                      <Folder className="size-4 shrink-0 text-text-tertiary" />
                      <span className="min-w-0 flex-1 truncate text-left text-body-13">
                        {schemaName}
                      </span>
                      <span className="text-code-11 text-text-tertiary">{tableEntries.length}</span>
                    </button>

                    {isSchemaExpanded &&
                      tableEntries
                        .filter(([tableName]) => filterMatch(tableName))
                        .map(([tableName, columnNames]) => {
                          const tableKey = `${schemaName}.${tableName}`;
                          const isTableExpanded = expandedTables[tableKey];
                          const isFocused =
                            selectedSchema === schemaName && selectedTable === tableName;
                          const loadedCols = tableColumnsMap[tableKey];

                          return (
                            <div key={tableName} className="flex flex-col gap-0.5">
                              <ContextMenu>
                                <ContextMenuTrigger asChild>
                                  {/* Table row (741:80): indent 24. */}
                                  <div
                                    className={cn(
                                      "group flex h-8 cursor-pointer items-center gap-2 rounded-md pr-3 pl-6",
                                      isFocused
                                        ? "bg-surface-raised text-text-primary"
                                        : "text-text-secondary hover:bg-surface-raised hover:text-text-primary",
                                    )}
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
                                    <ChevronRight
                                      className={cn(
                                        "size-3.5 shrink-0 text-text-tertiary transition-transform",
                                        isTableExpanded && "rotate-90",
                                      )}
                                    />
                                    <TableIcon
                                      className={cn(
                                        "size-4 shrink-0",
                                        isFocused ? "text-status-info" : "text-text-tertiary",
                                      )}
                                    />
                                    <span className="min-w-0 flex-1 truncate text-left text-body-13">
                                      {tableName}
                                    </span>
                                    {/* Clicking a row focuses the ERD. Without a
                                        mark the diagram just changes and nothing
                                        says which row did it. */}
                                    {isFocused ? (
                                      <Crosshair
                                        className="size-3.5 shrink-0 text-status-info"
                                        aria-label="Focused in the ERD"
                                      />
                                    ) : (
                                      <span className="text-code-11 text-text-tertiary">
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

                              {/* Column rows (741:89): indent 40, blank chevron
                                  slot, glyph, name, type in Code/11. */}
                              {isTableExpanded &&
                                (loadedCols
                                  ? loadedCols.map((col) => (
                                      <div
                                        key={col.name}
                                        className="flex h-8 items-center gap-2 rounded-md pr-3 pl-10 text-text-secondary hover:text-text-primary"
                                      >
                                        <span className="size-3.5 shrink-0" />
                                        {col.name.endsWith("_id") || col.name === "id" ? (
                                          <Key className="size-4 shrink-0 text-text-tertiary" />
                                        ) : (
                                          <Columns className="size-4 shrink-0 text-text-tertiary" />
                                        )}
                                        <span className="min-w-0 flex-1 truncate text-body-13">
                                          {col.name}
                                        </span>
                                        <span className="text-code-11 text-text-tertiary">
                                          {col.dataType}
                                        </span>
                                      </div>
                                    ))
                                  : (columnNames || []).map((colName) => (
                                      <div
                                        key={colName}
                                        className="flex h-8 items-center gap-2 rounded-md pr-3 pl-10 text-text-secondary"
                                      >
                                        <span className="size-3.5 shrink-0" />
                                        <Columns className="size-4 shrink-0 text-text-tertiary" />
                                        <span className="min-w-0 flex-1 truncate text-body-13">
                                          {colName}
                                        </span>
                                      </div>
                                    )))}
                            </div>
                          );
                        })}
                  </div>
                );
              })
            ) : (
              <div className="py-8 text-center text-body-12 text-text-tertiary">
                {selectedProfileId ? (
                  indexingSchema ? (
                    <p className="flex items-center justify-center gap-2">
                      <RefreshCw className="size-3 animate-spin" /> Indexing schema…
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

/**
 * Profile row (741:19): 48 tall, the engine's mark, the name over the host in
 * Code/11, and an env chip on the right. Selected is raised with a subtle
 * stroke; the hover-only actions sit between the text and the chip so the
 * chip never moves.
 */
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
  const env = (profile.env || "").toLowerCase();
  const isProd = env === "prod" || env === "production";

  return (
    <ContextMenu>
      <ContextMenuTrigger asChild>
        <div
          className={cn(
            "group flex h-12 cursor-pointer items-center gap-2 rounded-md border pr-3 pl-2 transition-colors",
            isSelected
              ? "border-border-subtle bg-surface-raised"
              : "border-transparent hover:bg-surface-raised/60",
          )}
          onClick={onSelect}
        >
          <EngineMark engine={profile.engine} />
          <div className="flex min-w-0 flex-1 flex-col">
            <span
              className={cn(
                "truncate",
                isSelected ? "text-body-13-medium text-text-primary" : "text-body-13 text-text-secondary",
              )}
            >
              {profile.name}
            </span>
            <span className="truncate text-code-11 text-text-tertiary">
              {profile.host}:{profile.port}
              {profile.database ? ` · ${profile.database}` : ""}
            </span>
          </div>

          {profile.readOnly && (
            <span title="Safe-mode (Read-Only) active">
              <Shield className="size-3.5 text-text-tertiary" />
            </span>
          )}
          {profile.sshProfileId && (
            <span title="SSH Tunnel enabled">
              <Radio className="size-3.5 text-text-tertiary" />
            </span>
          )}

          <div className="flex items-center gap-0.5 opacity-0 group-hover:opacity-100 has-[:focus-visible]:opacity-100">
            {/* Figma drew the folder dialog but not the way a profile gets into
                one. A menu is the smallest thing that works and reads: the list
                is short, and it says where the profile is now. */}
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <button
                  type="button"
                  className="rounded-md p-1 text-text-tertiary hover:text-text-primary"
                  onClick={(e) => e.stopPropagation()}
                  title="Move to folder"
                >
                  <FolderInput className="size-3" />
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
              className="rounded-md p-1 text-text-tertiary hover:text-text-primary"
              onClick={(e) => {
                e.stopPropagation();
                onOptions();
              }}
              title="Connection options"
            >
              <Settings2 className="size-3" />
            </button>
            <button
              type="button"
              className="rounded-md p-1 text-text-tertiary hover:text-status-danger"
              onClick={(e) => {
                e.stopPropagation();
                onDelete();
              }}
              title="Delete profile"
            >
              <Trash2 className="size-3" />
            </button>
          </div>

          {profile.env ? (
            <Chip
              tone={isProd ? "danger" : "default"}
              className={cn("h-5 px-2", !isProd && "text-text-tertiary")}
            >
              {env}
            </Chip>
          ) : null}
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
