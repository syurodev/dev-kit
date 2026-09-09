import { useState } from "react";
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
import { TreeView, type TreeNodeData } from "@/components/tree-view";
import { cn } from "@/lib/utils";
import { useDatabaseStore } from "../databaseStore";
import { countTables, filterNamespace, matchesProfile } from "../databaseTree";
import { FolderIcon } from "../folderIcons";
import { EditFolderDialog } from "./EditFolderDialog";
import { DeleteProfileDialog } from "./DeleteProfileDialog";
import { Chip, EngineMark } from "./EngineBadge";
import { deleteProfile, type ConnectionProfile } from "../../../bridge/database";
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

const GLYPH = "size-3.5 shrink-0 text-text-tertiary";
/** Hover-only row actions. Opacity rather than `hidden`, so revealing them
 *  does not resize the row and shove the count sideways. */
const ACTIONS =
  "flex items-center gap-0.5 opacity-0 group-hover:opacity-100 has-[:focus-visible]:opacity-100";
const ACTION = "rounded-sm p-0.5 text-text-tertiary hover:text-text-primary";

/**
 * One tree, DataGrip-shaped: folder → connection → schema → table → column.
 *
 * It used to be two stacked panes, Connections over Schema, which forced the
 * rail to answer "how do I split the height" before it could answer anything
 * else — one connection left an empty column while 173 tables scrolled in the
 * bottom half. Nesting the schema under the connection it belongs to removes
 * the question: every level is the same list, and depth carries the meaning
 * the two headings used to.
 *
 * The one place this is not DataGrip: DevKit indexes a single connection at a
 * time (`schemaSnapshot` belongs to `selectedProfileId`), so only the selected
 * connection unfolds. Opening another one selects it, which is what indexes it.
 *
 * Not drawn, for want of data rather than taste: DataGrip's `tables` / `views`
 * grouping folders, and `keys` / `indexes` under a table. The snapshot holds
 * tables and their columns and nothing else, and a wrapper folder whose only
 * possible child is the group below it is a click that buys nothing.
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
    setSql,
    sql,
    setShowNewConnectionDialog,
    setShowConnectionOptionsDialog,
  } = useDatabaseStore();

  const [filterQuery, setFilterQuery] = useState("");
  const [editingFolderId, setEditingFolderId] = useState("");
  const [deletingProfileId, setDeletingProfileId] = useState("");

  const needle = filterQuery.trim();
  // While searching, everything above a match is forced open — a hit four
  // levels down that stays folded is the same as no hit at all. Tables are the
  // exception: opening those would dump every column of every match.
  const filtering = needle.length > 0;
  const namespace = filterNamespace(schemaSnapshot?.namespace ?? {}, needle);

  function handleInsertTable(tableName: string) {
    if (!sql || sql.trim() === "SELECT 1 AS n, 'hello' AS greeting;") {
      setSql(`SELECT * FROM ${tableName} LIMIT 50;`);
    } else {
      setSql(`${sql} ${tableName}`);
    }
  }

  function columnNodes(schemaName: string, tableName: string): TreeNodeData[] {
    const key = `${schemaName}.${tableName}`;
    const loaded = tableColumnsMap[key];
    if (loaded) {
      return loaded.map((col) => ({
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
    // The snapshot carries names but no types, so the row is drawn from it
    // while listColumns is in flight rather than showing nothing.
    return (namespace[schemaName]?.[tableName] ?? []).map((name) => ({
      id: `col:${key}.${name}`,
      label: name,
      icon: <Columns className={GLYPH} />,
    }));
  }

  function tableNodes(schemaName: string): TreeNodeData[] {
    return Object.entries(namespace[schemaName] ?? {}).map(([tableName, columnNames]) => {
      const key = `${schemaName}.${tableName}`;
      const focused = selectedSchema === schemaName && selectedTable === tableName;
      return {
        id: `table:${key}`,
        label: tableName,
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
        onActivate: () => handleInsertTable(tableName),
        children: columnNodes(schemaName, tableName),
        emptyLabel: "No columns",
        meta: columnNames.length || undefined,
        trailing: focused ? (
          <Crosshair className="size-3 shrink-0 text-status-info" aria-label="Focused in the ERD" />
        ) : null,
        wrap: (row) => (
          <ContextMenu key={`table:${key}`}>
            <ContextMenuTrigger asChild>{row}</ContextMenuTrigger>
            {/* Inserting a SELECT was double-click only — an action with no
                visible affordance at all. The menu names it and keeps the
                shortcut. */}
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
        ),
      };
    });
  }

  function schemaNodes(): TreeNodeData[] {
    return Object.keys(namespace).map((schemaName) => ({
      id: `schema:${schemaName}`,
      label: schemaName,
      icon: <Database className={GLYPH} />,
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
      emptyLabel: filtering ? "No tables match" : "Not indexed yet — right-click to index",
      trailing: (
        <>
          <div className={ACTIONS}>
            {isSelected && (
              <button
                type="button"
                className={ACTION}
                title={indexed ? `Re-index schema — last indexed ${indexed}` : "Index schema"}
                onClick={(e) => {
                  e.stopPropagation();
                  void refreshSchema(profile.id);
                }}
              >
                <RefreshCw className={cn("size-3", indexingSchema && "animate-spin")} />
              </button>
            )}
            {/* Figma drew the folder dialog but not the way a profile gets into
                one. A menu is the smallest thing that works and reads: the list
                is short, and it says where the profile is now. */}
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <button
                  type="button"
                  className={ACTION}
                  onClick={(e) => e.stopPropagation()}
                  title="Move to folder"
                >
                  <FolderInput className="size-3" />
                </button>
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
            <button
              type="button"
              className={ACTION}
              title="Connection options"
              onClick={(e) => {
                e.stopPropagation();
                setSelectedProfileId(profile.id);
                setShowConnectionOptionsDialog(true);
              }}
            >
              <Settings2 className="size-3" />
            </button>
          </div>
          {profile.readOnly && (
            <span title="Safe-mode (read-only) active">
              <Shield className="size-3 shrink-0 text-text-tertiary" />
            </span>
          )}
          {profile.sshProfileId && (
            <span title="SSH tunnel enabled">
              <Radio className="size-3 shrink-0 text-text-tertiary" />
            </span>
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
      <div className="flex items-center gap-1 pt-2 pb-1">
        <div className="relative min-w-0 flex-1">
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
              className="absolute top-1/2 right-1.5 -translate-y-1/2 rounded-sm p-0.5 text-text-tertiary hover:text-text-primary"
              title="Clear search"
            >
              <X className="size-3" />
            </button>
          )}
        </div>
        <Button
          variant="ghost"
          size="icon-xs"
          onClick={() => void fetchProfiles()}
          disabled={loadingProfiles}
          title="Refresh connections"
        >
          <RefreshCw className={cn("size-3", loadingProfiles && "animate-spin")} />
        </Button>
        <Button
          variant="ghost"
          size="icon-xs"
          onClick={() => createFolder(`Folder ${folders.length + 1}`)}
          title="New folder"
        >
          <FolderPlus className="size-3" />
        </Button>
        <Button
          variant="ghost"
          size="icon-xs"
          onClick={() => setShowNewConnectionDialog(true)}
          title="New connection"
        >
          <Plus className="size-3" />
        </Button>
      </div>

      {/* Searching a 173-table list and being shown a shorter one with no idea
          how much it cut is the part that made the box feel broken. */}
      {filtering && selectedProfileId ? (
        <span className="px-2 pb-1 text-code-11 text-text-tertiary">
          {matchCount} table{matchCount === 1 ? "" : "s"} match
        </span>
      ) : null}

      <div className="min-h-0 flex-1 overflow-y-auto pb-2">
        {profiles.length === 0 && !loadingProfiles ? (
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
        ) : tree.length === 0 ? (
          <p className="px-4 py-6 text-center text-body-12 text-text-tertiary">
            Nothing matches “{needle}”.
          </p>
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
