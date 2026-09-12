import { useEffect, useState } from "react";
import {
  Plus,
  RefreshCw,
  Folder,
  Database,
  Table as TableIcon,
  Columns,
  Key,
  Shield,
  Radio,
  Settings2,
  Pencil,
  Copy,
  FolderPlus,
  FolderInput,
  Crosshair,
  Trash2,
  TriangleAlert,
  Activity,
  Bookmark,
  History,
  Network,
} from "lucide-react";
import { ActionTooltip } from "@/components/ui/action-tooltip";
import { Button } from "@/components/ui/button";
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyTitle,
} from "@/components/ui/empty";
import { SidebarHeader, SidebarSearch } from "@/components/ui/sidebar-shell";
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
import { TreeView, type TreeNodeData } from "@/components/tree-view";
import { cn } from "@/lib/utils";
import { useShallow } from "zustand/react/shallow";
import { useDatabaseStore } from "../databaseStore";
import { countTables, filterNamespace, matchesProfile } from "../databaseTree";
import { FolderIcon } from "../folderIcons";
import { EditFolderDialog } from "./EditFolderDialog";
import { DeleteProfileDialog } from "./DeleteProfileDialog";
import { Chip, EngineMark } from "./EngineBadge";
import { deleteProfile, type ConnectionProfile } from "../../../bridge/database";
import { copyText } from "../../../bridge/clipboard";
import {
  openActivityTab,
  openErdTab,
  openHistoryTab,
  openSavedTab,
  openTableTab,
} from "../documents";

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

const GLYPH = "size-3.5 shrink-0 text-text-tertiary";
/** --control-height-xs, which is what TreeView gives a row. */
const ROW_HEIGHT_PX = 24;
/** Hover-only row actions. Opacity rather than `hidden`, so revealing them
 *  does not resize the row and shove the count sideways. */
const ACTIONS =
  "flex items-center gap-1 opacity-0 group-hover:opacity-100 has-[:focus-visible]:opacity-100";
/**
 * 24×24, not the 16×16 these were.
 *
 * accessibility.md › Mobility puts macOS at a 28×28 default and a 20×20
 * minimum, and a `size-3` glyph in `p-0.5` came to sixteen — under the floor,
 * on the three controls that act on a connection. 24 is what the row can give
 * (it is `--control-height-xs` tall); the gap goes with it, because the same
 * section calls spacing between controls as important as their size.
 */
const ACTION =
  "flex size-6 items-center justify-center rounded-sm text-text-tertiary hover:bg-state-soft hover:text-text-primary";

/**
 * One tree, DataGrip-shaped: folder → connection → schema → table → column.
 *
 * One tree rather than two stacked panes: separate Connections and Schema panes
 * force the rail to answer "how do I split the height" first, and one connection
 * leaves an empty column while 173 tables scroll below it. Nested, every level
 * is the same list and depth carries what the headings would have.
 *
 * The one place this is not DataGrip: DevKit indexes a single connection at a
 * time (`schemaSnapshot` belongs to `selectedProfileId`), so only the selected
 * connection unfolds. Opening another one selects it, which is what indexes it.
 *
 * Not drawn for want of data: DataGrip's `tables`/`views` grouping folders and
 * `keys`/`indexes` under a table. The snapshot holds tables and columns only,
 * and a folder whose only child is the group below it is a click for nothing.
 */
export function DatabaseSidebar() {
  const {
    profiles,
    selectedProfileId,
    setSelectedProfileId,
    loadingProfiles,
    fetchProfiles,
    refreshTxState,
    folders,
    expandedFolders,
    toggleFolder,
    createFolder,
    updateFolder,
    deleteFolder,
    assignProfileToFolder,
    expandedProfiles,
    toggleProfileExpand,
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
    tableColumnsError,
    setShowNewConnectionDialog,
    setShowConnectionOptionsDialog,
  } = useDatabaseStore(
    useShallow((s) => ({
      profiles: s.profiles,
      selectedProfileId: s.selectedProfileId,
      setSelectedProfileId: s.setSelectedProfileId,
      loadingProfiles: s.loadingProfiles,
      fetchProfiles: s.fetchProfiles,
      refreshTxState: s.refreshTxState,
      folders: s.folders,
      expandedFolders: s.expandedFolders,
      toggleFolder: s.toggleFolder,
      createFolder: s.createFolder,
      updateFolder: s.updateFolder,
      deleteFolder: s.deleteFolder,
      assignProfileToFolder: s.assignProfileToFolder,
      expandedProfiles: s.expandedProfiles,
      toggleProfileExpand: s.toggleProfileExpand,
      schemaSnapshot: s.schemaSnapshot,
      indexingSchema: s.indexingSchema,
      refreshSchema: s.refreshSchema,
      expandedSchemas: s.expandedSchemas,
      toggleSchemaExpand: s.toggleSchemaExpand,
      expandedTables: s.expandedTables,
      toggleTableExpand: s.toggleTableExpand,
      selectedSchema: s.selectedSchema,
      setSelectedSchema: s.setSelectedSchema,
      selectedTable: s.selectedTable,
      setSelectedTable: s.setSelectedTable,
      tableColumnsMap: s.tableColumnsMap,
      tableColumnsError: s.tableColumnsError,
      setShowNewConnectionDialog: s.setShowNewConnectionDialog,
      setShowConnectionOptionsDialog: s.setShowConnectionOptionsDialog,
    }))
  );

  // The rail loads what the rail shows.
  //
  // This used to live in DatabaseView's mount effect, which worked only
  // because a console was always open: with tabs remembered instead of
  // invented, a module can be on screen with no document at all, and the rail
  // sat on "No connections configured." until you opened one. A list that
  // needs a document open to be fetched is a list that belongs to the
  // document, and this one does not.
  //
  // Exactly one caller now, not one per document: the rail mounts once for the
  // module — SidebarSurface keeps it mounted even while collapsed — so moving
  // the call here removes a fetch rather than adding one.
  useEffect(() => {
    void fetchProfiles();
    // And whether the selected connection is already inside a transaction: one
    // outlives the window that opened it, so arriving can mean arriving inside
    // one, and showing "Auto" beside it would be a lie about what the next
    // statement does.
    void refreshTxState();
  }, [fetchProfiles, refreshTxState]);

  const [filterQuery, setFilterQuery] = useState("");
  const [editingFolderId, setEditingFolderId] = useState("");
  const [deletingProfileId, setDeletingProfileId] = useState("");

  // Creating and naming are one gesture: the folder appears immediately (so
  // Cancel still leaves you the folder you asked for) and its dialog opens on
  // top, with "Folder 3" selected and ready to be typed over.
  function newFolder() {
    const id = createFolder(`Folder ${folders.length + 1}`);
    if (id) setEditingFolderId(id);
  }

  const needle = filterQuery.trim();
  // While searching, everything above a match is forced open — a hit four
  // levels down that stays folded is the same as no hit at all. Tables are the
  // exception: opening those would dump every column of every match.
  const filtering = needle.length > 0;
  const namespace = filterNamespace(schemaSnapshot?.namespace ?? {}, needle);

  function columnNodes(schemaName: string, tableName: string): TreeNodeData[] {
    const key = `${schemaName}.${tableName}`;
    // The snapshot is the first source and usually the only one: indexing
    // already read every column's type, so a freshly indexed schema shows types
    // the instant a table opens, with no round trip to wait on. listColumns is
    // the fallback for a snapshot taken before the cache carried types.
    const columns = schemaSnapshot?.columns?.[schemaName]?.[tableName] ?? tableColumnsMap[key];
    if (columns?.length) {
      return columns.map((col) => ({
        id: `col:${key}.${col.name}`,
        label: col.name,
        icon:
          col.name === "id" || col.name.endsWith("_id") ? (
            <Key className={GLYPH} />
          ) : (
            <Columns className={GLYPH} />
          ),
        meta: col.dataType,
        title: `${col.dataType}${col.nullable ? "" : " · not null"}`,
      }));
    }
    // Names with no types: an old snapshot whose listColumns is still in flight,
    // or one that failed. Drawn anyway — the names are still true.
    const names: TreeNodeData[] = (namespace[schemaName]?.[tableName] ?? []).map((name) => ({
      id: `col:${key}.${name}`,
      label: name,
      icon: <Columns className={GLYPH} />,
    }));
    // A failure has to be visible where it happened. emptyLabel cannot carry it:
    // the names above are still listed, so the branch is never empty and that
    // slot never renders.
    const failure = tableColumnsError[key];
    if (failure) {
      names.push({
        id: `col-error:${key}`,
        label: <span className="text-status-danger">Column types unavailable</span>,
        icon: <TriangleAlert className={cn(GLYPH, "text-status-danger")} />,
        title: failure,
      });
    }
    return names;
  }

  function tableNodes(schemaName: string): TreeNodeData[] {
    return Object.entries(namespace[schemaName] ?? {}).map(([tableName, columnNames]) => {
      const key = `${schemaName}.${tableName}`;
      const focused = selectedSchema === schemaName && selectedTable === tableName;
      return {
        id: `table:${key}`,
        label: tableName,
        // Names are truncated at the rail's width, and a schema of
        // backup_his_medical_records_* tables truncates to the same string
        // several times over. The tooltip is the only thing telling them apart
        // without expanding each one.
        title: `${schemaName}.${tableName}`,
        icon: <TableIcon className={cn(GLYPH, focused && "text-status-info")} />,
        expandable: true,
        expanded: !!expandedTables[key],
        // Async: opening a table is what fetches its columns.
        onToggle: () => void toggleTableExpand(schemaName, tableName),
        selected: focused,
        // Clicking a table is also how the ERD is focused — picking one out of
        // 173 in a dropdown is not a real gesture.
        onSelect: () => {
          setSelectedSchema(schemaName);
          setSelectedTable(tableName);
        },
        // Single click selects and expands; double click opens the data. That
        // is the DataGrip gesture, and it is why opening a table no longer
        // overwrites whatever SQL a console had in it.
        onActivate: () => openTableTab(schemaName, tableName, false),
        children: columnNodes(schemaName, tableName),
        emptyLabel: tableColumnsError[key] ?? "No columns",
        meta: columnNames.length || undefined,
        trailing: focused ? (
          <Crosshair className="size-3 shrink-0 text-status-info" aria-label="Focused in the ERD" />
        ) : null,
        wrap: (row) => (
          <ContextMenu key={`table:${key}`}>
            <ContextMenuTrigger asChild>{row}</ContextMenuTrigger>
            {/* Opening the data was double-click only — an action with no
                visible affordance at all. The menu names it and keeps the
                shortcut. */}
            <ContextMenuContent>
              <ContextMenuItem onSelect={() => openTableTab(schemaName, tableName, false)}>
                Open data
                <ContextMenuShortcut>Double-click</ContextMenuShortcut>
              </ContextMenuItem>
              <ContextMenuItem
                onSelect={() => {
                  setSelectedSchema(schemaName);
                  setSelectedTable(tableName);
                  openErdTab();
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
        ),
      };
    });
  }

  function schemaNodes(): TreeNodeData[] {
    return Object.keys(namespace).map((schemaName) => ({
      id: `schema:${schemaName}`,
      label: schemaName,
      icon: <Database className={GLYPH} />,
      // Under the connection row. Scrolling to table 140 of 173 used to leave
      // no answer on screen to "which connection, which schema" — the two
      // headings a split view would have given, bought here with sticky.
      sticky: ROW_HEIGHT_PX,
      expandable: true,
      expanded: filtering || (expandedSchemas[schemaName] ?? true),
      onToggle: () => toggleSchemaExpand(schemaName),
      children: tableNodes(schemaName),
      emptyLabel: "No tables",
      meta: Object.keys(namespace[schemaName] ?? {}).length,
    }));
  }

  function profileNode(profile: ConnectionProfile, folderId: string): TreeNodeData {
    const isSelected = profile.id === selectedProfileId;
    const env = (profile.env || "").toLowerCase();
    const isProd = env === "prod" || env === "production";
    const indexed = schemaSnapshot?.indexedAt ? relativeTime(schemaSnapshot.indexedAt) : "";

    return {
      id: `profile:${profile.id}`,
      label: profile.name,
      icon: <EngineMark engine={profile.engine} className="size-3.5" />,
      // Only the selected connection unfolds, so only it has anything to stay
      // above. Pinning the others would stack four dead headings at the top.
      sticky: isSelected ? 0 : undefined,
      // Only the indexed connection can show a schema, so the fold state of
      // the others is not offered as a lie.
      expandable: true,
      expanded: isSelected && (filtering || !!expandedProfiles[profile.id]),
      onToggle: () => toggleProfileExpand(profile.id),
      selected: isSelected,
      // Guarded: setSelectedProfileId clears the result grid, and collapsing
      // the connection you are working in must not throw away its rows.
      onSelect: isSelected ? undefined : () => setSelectedProfileId(profile.id),
      title: `${profile.username}@${profile.host}:${profile.port}/${profile.database}${
        isSelected && indexed ? ` · indexed ${indexed}` : ""
      }`,
      children: isSelected ? schemaNodes() : undefined,
      loading: isSelected && indexingSchema,
      // A label that names a gesture is a label for a control that should have
      // been drawn (writing.md › "give them a button or link to do so if
      // possible"). This is the same complaint as "opening data was
      // double-click only", one level up the tree.
      emptyLabel: filtering ? (
        "No tables match"
      ) : (
        <button
          type="button"
          className="inline-flex items-center gap-1 rounded-sm text-text-secondary underline-offset-2 hover:text-text-primary hover:underline"
          onClick={() => void refreshSchema(profile.id)}
          disabled={indexingSchema}
        >
          <RefreshCw className="size-3" />
          Index this schema
        </button>
      ),
      trailing: (
        <>
          <div className={ACTIONS}>
            {isSelected && (
              <ActionTooltip
                label={indexed ? `Re-index schema — last indexed ${indexed}` : "Index schema"}
              >
                <button
                  type="button"
                  className={ACTION}
                  onClick={(e) => {
                    e.stopPropagation();
                    void refreshSchema(profile.id);
                  }}
                >
                  <RefreshCw className={cn("size-3", indexingSchema && "animate-spin")} />
                </button>
              </ActionTooltip>
            )}
            {/* Figma drew the folder dialog but not the way a profile gets into
                one. A menu is the smallest thing that works and reads: the list
                is short, and it says where the profile is now. */}
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <ActionTooltip label="Move to folder">
                  <button type="button" className={ACTION} onClick={(e) => e.stopPropagation()}>
                    <FolderInput className="size-3" />
                  </button>
                </ActionTooltip>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" onClick={(e) => e.stopPropagation()}>
                <DropdownMenuItem
                  onSelect={() => assignProfileToFolder(profile.id, "")}
                  disabled={!folderId}
                >
                  <Folder />
                  Ungrouped
                </DropdownMenuItem>
                {folders.map((f) => (
                  <DropdownMenuItem
                    key={f.id}
                    onSelect={() => assignProfileToFolder(profile.id, f.id)}
                    disabled={f.id === folderId}
                  >
                    <FolderIcon name={f.icon} className="size-4" />
                    {f.name}
                  </DropdownMenuItem>
                ))}
              </DropdownMenuContent>
            </DropdownMenu>
            <ActionTooltip label="Connection options">
              <button
                type="button"
                className={ACTION}
                onClick={(e) => {
                  e.stopPropagation();
                  setSelectedProfileId(profile.id);
                  setShowConnectionOptionsDialog(true);
                }}
              >
                <Settings2 className="size-3" />
              </button>
            </ActionTooltip>
          </div>
          {profile.readOnly && (
            <ActionTooltip label="Safe mode (read-only) is on">
              <span>
                <Shield className="size-3 shrink-0 text-text-tertiary" />
              </span>
            </ActionTooltip>
          )}
          {profile.sshProfileId && (
            <ActionTooltip label="SSH tunnel enabled">
              <span>
                <Radio className="size-3 shrink-0 text-text-tertiary" />
              </span>
            </ActionTooltip>
          )}
          {profile.env ? (
            <Chip
              tone={isProd ? "danger" : "default"}
              className={cn("h-5 px-1.5", !isProd && "text-text-tertiary")}
            >
              {env}
            </Chip>
          ) : null}
        </>
      ),
      wrap: (row) => (
        <ContextMenu key={`profile:${profile.id}`}>
          <ContextMenuTrigger asChild>{row}</ContextMenuTrigger>
          <ContextMenuContent>
            <ContextMenuItem
              onSelect={() => void refreshSchema(profile.id)}
              disabled={indexingSchema || !isSelected}
            >
              <RefreshCw />
              {isSelected ? "Re-index schema" : "Open this connection first"}
            </ContextMenuItem>
            <ContextMenuSub>
              <ContextMenuSubTrigger>
                <FolderInput />
                Move to folder
              </ContextMenuSubTrigger>
              <ContextMenuSubContent>
                <ContextMenuItem
                  onSelect={() => assignProfileToFolder(profile.id, "")}
                  disabled={!folderId}
                >
                  <Folder />
                  Ungrouped
                </ContextMenuItem>
                {folders.map((f) => (
                  <ContextMenuItem
                    key={f.id}
                    onSelect={() => assignProfileToFolder(profile.id, f.id)}
                    disabled={f.id === folderId}
                  >
                    <FolderIcon name={f.icon} className="size-4" />
                    {f.name}
                  </ContextMenuItem>
                ))}
              </ContextMenuSubContent>
            </ContextMenuSub>
            <ContextMenuItem
              onSelect={() => {
                setSelectedProfileId(profile.id);
                setShowConnectionOptionsDialog(true);
              }}
            >
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
            <ContextMenuItem onSelect={() => openErdTab()}>
              <Network />
              ERD diagram
            </ContextMenuItem>
            <ContextMenuItem onSelect={() => openActivityTab()}>
              <Activity />
              Live activity
            </ContextMenuItem>
            <ContextMenuItem onSelect={() => openHistoryTab()}>
              <History />
              Query history
            </ContextMenuItem>
            <ContextMenuItem onSelect={() => openSavedTab()}>
              <Bookmark />
              Saved queries
            </ContextMenuItem>
            <ContextMenuSeparator />
            <ContextMenuItem variant="destructive" onSelect={() => setDeletingProfileId(profile.id)}>
              <Trash2 />
              Delete connection
            </ContextMenuItem>
          </ContextMenuContent>
        </ContextMenu>
      ),
    };
  }

  // Folders first, then whatever is in none of them — a connection at the root
  // is what "ungrouped" looks like in a tree, so it needs no heading of its own.
  const assigned = new Set<string>();
  const folderNodes: TreeNodeData[] = folders.map((folder) => {
    const inFolder = profiles.filter((p) => folder.profileIds.includes(p.id));
    inFolder.forEach((p) => assigned.add(p.id));
    const visible = inFolder.filter((p) => matchesProfile(p, needle));
    return {
      id: `folder:${folder.id}`,
      label: folder.name,
      icon: <FolderIcon name={folder.icon} className="size-3.5 shrink-0 text-text-secondary" />,
      // An empty folder gets no chevron rather than one that opens onto an
      // "Empty folder" line: the count already says it, and the row it saves
      // is a row.
      expandable: inFolder.length > 0,
      expanded: filtering || expandedFolders[folder.id] !== false,
      onToggle: () => toggleFolder(folder.id),
      children: visible.map((p) => profileNode(p, folder.id)),
      emptyLabel: "No connections match",
      meta: inFolder.length,
      wrap: (row) => (
        <ContextMenu key={`folder:${folder.id}`}>
          <ContextMenuTrigger asChild>{row}</ContextMenuTrigger>
          <ContextMenuContent>
            <ContextMenuItem onSelect={() => setEditingFolderId(folder.id)}>
              <Pencil />
              Edit folder
            </ContextMenuItem>
            <ContextMenuItem onSelect={() => newFolder()}>
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
      ),
    };
  });

  const rootProfiles = profiles
    .filter((p) => !assigned.has(p.id) && matchesProfile(p, needle))
    .map((p) => profileNode(p, ""));

  const tree = [
    // A folder that hides all its connections under a search is noise.
    ...folderNodes.filter((node) => !filtering || node.children?.length),
    ...rootProfiles,
  ];

  const matchCount = countTables(namespace);

  return (
    <div className="flex h-full flex-col select-none">
      <SidebarHeader>
        <SidebarSearch
          value={filterQuery}
          onChange={setFilterQuery}
          onClear={() => setFilterQuery("")}
          placeholder="Search connections & tables…"
        />
        <ActionTooltip label="Refresh connections">
          <Button
            variant="ghost"
            size="icon-xs"
            onClick={() => void fetchProfiles()}
            disabled={loadingProfiles}
          >
            <RefreshCw className={cn("size-3", loadingProfiles && "animate-spin")} />
          </Button>
        </ActionTooltip>
        <ActionTooltip label="New folder">
          <Button variant="ghost" size="icon-xs" onClick={newFolder}>
            <FolderPlus className="size-3" />
          </Button>
        </ActionTooltip>
        <ActionTooltip label="New connection">
          <Button variant="ghost" size="icon-xs" onClick={() => setShowNewConnectionDialog(true)}>
            <Plus className="size-3" />
          </Button>
        </ActionTooltip>
      </SidebarHeader>

      {/* Searching a 173-table list and being shown a shorter one with no idea
          how much it cut is the part that made the box feel broken. */}
      {filtering && selectedProfileId ? (
        <span className="px-2 pb-1 text-code-11 text-text-tertiary">
          {matchCount} table{matchCount === 1 ? "" : "s"} match
        </span>
      ) : null}

      <div className="min-h-0 flex-1 overflow-y-auto scrollbar-gutter-symmetric pb-2">
        {profiles.length === 0 && !loadingProfiles ? (
          <Empty className="px-4 py-6">
            <EmptyTitle className="text-body-12 text-text-tertiary">
              No connections configured.
            </EmptyTitle>
            <EmptyContent>
              <Button
                variant="outline"
                size="sm"
                onClick={() => setShowNewConnectionDialog(true)}
              >
                <Plus className="size-3" /> Add profile
              </Button>
            </EmptyContent>
          </Empty>
        ) : tree.length === 0 ? (
          <Empty className="px-4 py-6">
            <EmptyDescription>Nothing matches “{needle}”.</EmptyDescription>
          </Empty>
        ) : (
          <TreeView nodes={tree} />
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
