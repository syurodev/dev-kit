import { useEffect, useState } from "react";
import { toast } from "sonner";
import { errorMessage } from "@/bridge/errors";
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
  MoreHorizontal,
  Settings2,
  Pencil,
  Copy,
  FolderPlus,
  FolderInput,
  Crosshair,
  FileCode,
  Trash2,
  TriangleAlert,
  Activity,
  Bookmark,
  History,
  Network,
  Terminal,
} from "lucide-react";
import { ActionTooltip } from "@/components/ui/action-tooltip";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import { SidebarHeader, SidebarSearch } from "@/components/ui/sidebar-shell";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuTrigger,
} from "@/components/ui/context-menu";
import {
  DropdownMenu,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { TreeRowAction, TreeRowTag, TreeView, type TreeNodeData } from "@/components/tree-view";
import { useShallow } from "zustand/react/shallow";
import { useDatabaseStore, type SidebarPane } from "../databaseStore";
import { countTables, filterNamespace, keepsProfile } from "../databaseTree";
import { FolderIcon } from "../folderIcons";
import { DatabaseModuleOverlays } from "../DatabaseModuleOverlays";
import { RowContextMenuContent, RowDropdownMenuContent, type RowAction } from "@/components/row-actions";
import { EditFolderDialog } from "./EditFolderDialog";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { ConsoleFilesPanel } from "./ConsoleFilesPanel";
import { SavedQueriesPanel } from "./SavedQueriesPanel";
import { DeleteProfileDialog } from "./DeleteProfileDialog";
import { EngineMark } from "./EngineBadge";
import { deleteProfile, type ConnectionProfile } from "../../../bridge/database";
import { copyText } from "../../../bridge/clipboard";
import {
  openActivityTab,
  openErdTab,
  openHistoryTab,
  openNewConsoleTab,
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
/**
 * The rail's three lists, in the order they are reached for: the catalogue
 * you browse, the consoles you left open, the queries you kept.
 */
const PANES: { value: SidebarPane; label: string; Icon: typeof Database }[] = [
  { value: "connections", label: "Connections", Icon: Database },
  { value: "consoles", label: "Consoles", Icon: Terminal },
  { value: "saved", label: "Saved queries", Icon: Bookmark },
];

export function DatabaseSidebar() {
  // #42: said once, where the switch happened. The toolbar on screen now
  // describes the connection you arrived at, and nothing else would mention
  // the transaction still holding its locks on the one you left.
  const strandedTransaction = useDatabaseStore((s) => s.strandedTransaction);
  useEffect(() => {
    if (!strandedTransaction) return;
    toast.warning(`${strandedTransaction} still has an open transaction`, {
      description: "Its locks stay held. Switch back to commit or roll it back.",
    });
    useDatabaseStore.setState({ strandedTransaction: "" });
  }, [strandedTransaction]);

  const {
    profiles,
    selectedProfileId,
    setSelectedProfileId,
    loadingProfiles,
    fetchProfiles,
    hydrateConsoles,
    sidebarPane,
    setSidebarPane,
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
    openDDL,
  } = useDatabaseStore(
    useShallow((s) => ({
      profiles: s.profiles,
      selectedProfileId: s.selectedProfileId,
      setSelectedProfileId: s.setSelectedProfileId,
      loadingProfiles: s.loadingProfiles,
      fetchProfiles: s.fetchProfiles,
      hydrateConsoles: s.hydrateConsoles,
      sidebarPane: s.sidebarPane,
      setSidebarPane: s.setSidebarPane,
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
      openDDL: s.openDDL,
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
    // And the console files, for the same reason and in the same place: the
    // rail mounts once for the module, so this is one read rather than one
    // per console tab the window restored.
    void hydrateConsoles();
    // And whether the selected connection is already inside a transaction: one
    // outlives the window that opened it, so arriving can mean arriving inside
    // one, and showing "Auto" beside it would be a lie about what the next
    // statement does.
    void refreshTxState();
  }, [fetchProfiles, hydrateConsoles, refreshTxState]);

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
  const matchCount = countTables(namespace);
  const keep = (p: ConnectionProfile) =>
    keepsProfile(p, needle, p.id === selectedProfileId && matchCount > 0);

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
            <Key />
          ) : (
            <Columns />
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
      icon: <Columns />,
    }));
    // A failure has to be visible where it happened. emptyLabel cannot carry it:
    // the names above are still listed, so the branch is never empty and that
    // slot never renders.
    const failure = tableColumnsError[key];
    if (failure) {
      names.push({
        id: `col-error:${key}`,
        label: <span className="text-status-danger">Column types unavailable</span>,
        icon: <TriangleAlert className="text-status-danger" />,
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
        icon: <TableIcon className={focused ? "text-status-info" : undefined} />,
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
        count: columnNames.length || undefined,
        trailing: focused ? (
          <Crosshair className="text-status-info" aria-label="Focused in the ERD" />
        ) : null,
        wrap: (row) => (
          <ContextMenu key={`table:${key}`}>
            <ContextMenuTrigger asChild>{row}</ContextMenuTrigger>
            {/* Opening the data was double-click only — an action with no
                visible affordance at all. The menu names it and keeps the
                shortcut. */}
            <ContextMenuContent>
              <ContextMenuItem shortcut="Double-click" onSelect={() => openTableTab(schemaName, tableName, false)}>
                Open data
              </ContextMenuItem>
              {/* No profile to select here: a table row only exists under the
                  connection that is already open. */}
              <ContextMenuItem icon={<Terminal />} onSelect={() => openNewConsoleTab()}>
                Open query console
              </ContextMenuItem>
              <ContextMenuItem
                icon={<Crosshair />}
                onSelect={() => {
                  setSelectedSchema(schemaName);
                  setSelectedTable(tableName);
                  openErdTab();
                }}
              >
                Focus in ERD
              </ContextMenuItem>
              <ContextMenuItem icon={<FileCode />} onSelect={() => openDDL(schemaName, tableName)}>
                Show DDL
              </ContextMenuItem>
              <ContextMenuItem icon={<Copy />} onSelect={() => void copyText(tableName)}>
                Copy table name
              </ContextMenuItem>
              <ContextMenuSeparator />
              <ContextMenuItem
                icon={<RefreshCw />}
                onSelect={() => void refreshSchema(selectedProfileId)}
                disabled={indexingSchema}
              >
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
      icon: <Database />,
      // Under the connection row. Scrolling to table 140 of 173 used to leave
      // no answer on screen to "which connection, which schema" — the two
      // headings a split view would have given, bought here with sticky.
      sticky: 1,
      expandable: true,
      expanded: filtering || (expandedSchemas[schemaName] ?? true),
      onToggle: () => toggleSchemaExpand(schemaName),
      children: tableNodes(schemaName),
      emptyLabel: "No tables",
      count: Object.keys(namespace[schemaName] ?? {}).length,
      // The schema row had no right-click menu at all: the level between a
      // connection with nine actions and a table with five offered none, so
      // the gesture that works everywhere else in the tree died here.
      //
      // Three items, and each is the schema's version of one the table row
      // already carries — a menu that invents its own vocabulary one level up
      // is a second menu to learn.
      wrap: (row) => (
        <ContextMenu key={`schema:${schemaName}`}>
          <ContextMenuTrigger asChild>{row}</ContextMenuTrigger>
          <ContextMenuContent>
            <ContextMenuItem icon={<Terminal />} onSelect={() => openNewConsoleTab()}>
              Open query console
            </ContextMenuItem>
            <ContextMenuItem icon={<Copy />} onSelect={() => void copyText(schemaName)}>
              Copy schema name
            </ContextMenuItem>
            <ContextMenuSeparator />
            <ContextMenuItem
              icon={<RefreshCw />}
              onSelect={() => void refreshSchema(selectedProfileId)}
              disabled={indexingSchema}
            >
              Re-index schema
            </ContextMenuItem>
          </ContextMenuContent>
        </ContextMenu>
      ),
    }));
  }

  /**
   * Everything you can do to a connection, in one place.
   *
   * Read by both the row's button and its right-click menu — see rowActions.
   * Re-indexing leads because it is the one that goes stale; deleting trails
   * behind a divider because it is the one that cannot be undone.
   */
  function profileActions(
    profile: ConnectionProfile,
    folderId: string,
    isSelected: boolean,
  ): RowAction[] {
    return [
      {
        kind: "item",
        icon: <RefreshCw />,
        label: isSelected ? "Re-index schema" : "Open this connection first",
        onSelect: () => void refreshSchema(profile.id),
        disabled: indexingSchema || !isSelected,
      },
      {
        kind: "submenu",
        icon: <FolderInput />,
        label: "Move to folder",
        items: [
          {
            kind: "item",
            icon: <Folder />,
            label: "Ungrouped",
            onSelect: () => assignProfileToFolder(profile.id, ""),
            disabled: !folderId,
          },
          ...folders.map(
            (f): RowAction => ({
              kind: "item",
              icon: <FolderIcon name={f.icon} className="size-4" />,
              label: f.name,
              onSelect: () => assignProfileToFolder(profile.id, f.id),
              disabled: f.id === folderId,
            }),
          ),
        ],
      },
      {
        kind: "item",
        icon: <Settings2 />,
        label: "Connection options",
        onSelect: () => {
          setSelectedProfileId(profile.id);
          setShowConnectionOptionsDialog(true);
        },
      },
      {
        kind: "item",
        icon: <Copy />,
        label: "Copy connection target",
        onSelect: () =>
          void copyText(
            `${profile.username}@${profile.host}:${profile.port}${profile.database ? `/${profile.database}` : ""}`,
          ),
      },
      { kind: "separator" },
      {
        kind: "item",
        icon: <Terminal />,
        label: "Open query console",
        // Selecting first, because a console runs against the SELECTED
        // connection and nothing else: opened from a row that is not the
        // current one, an empty console pointed at the previous database is
        // the one outcome nobody wants. Guarded, because setSelectedProfileId
        // throws away the result grid.
        onSelect: () => {
          if (!isSelected) setSelectedProfileId(profile.id);
          openNewConsoleTab();
        },
      },
      { kind: "item", icon: <Network />, label: "ERD diagram", onSelect: () => openErdTab() },
      { kind: "item", icon: <Activity />, label: "Live activity", onSelect: () => openActivityTab() },
      { kind: "item", icon: <History />, label: "Query history", onSelect: () => openHistoryTab() },
      { kind: "item", icon: <Bookmark />, label: "Saved queries", onSelect: () => openSavedTab() },
      { kind: "separator" },
      {
        kind: "item",
        icon: <Trash2 />,
        label: "Delete connection",
        onSelect: () => setDeletingProfileId(profile.id),
        destructive: true,
      },
    ];
  }

  function profileNode(profile: ConnectionProfile, folderId: string): TreeNodeData {
    const isSelected = profile.id === selectedProfileId;
    const env = (profile.env || "").toLowerCase();
    const isProd = env === "prod" || env === "production";
    const indexed = schemaSnapshot?.indexedAt ? relativeTime(schemaSnapshot.indexedAt) : "";

    return {
      id: `profile:${profile.id}`,
      label: profile.name,
      icon: <EngineMark engine={profile.engine} />,
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
      title: `${profile.username}@${profile.host}:${profile.port}${profile.database ? `/${profile.database}` : ""}${
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
      // One button, not three. The three reached three of the nine things the
      // right-click menu offers, so which actions existed depended on how you
      // asked; this opens the same list. Hover-only: the tree shows it.
      actions: (
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <ActionTooltip label={`Actions for ${profile.name}`}>
              <TreeRowAction aria-label={`Actions for ${profile.name}`}>
                <MoreHorizontal />
              </TreeRowAction>
            </ActionTooltip>
          </DropdownMenuTrigger>
          <RowDropdownMenuContent actions={profileActions(profile, folderId, isSelected)} />
        </DropdownMenu>
      ),
      trailing: (
        <>
          {profile.readOnly && (
            <ActionTooltip label="Safe mode (read-only) is on">
              <span>
                <Shield />
              </span>
            </ActionTooltip>
          )}
          {profile.sshProfileId && (
            <ActionTooltip label="SSH tunnel enabled">
              <span>
                <Radio />
              </span>
            </ActionTooltip>
          )}
          {profile.env ? (
            <TreeRowTag danger={isProd}>{env}</TreeRowTag>
          ) : null}
        </>
      ),
      wrap: (row) => (
        <ContextMenu key={`profile:${profile.id}`}>
          <ContextMenuTrigger asChild>{row}</ContextMenuTrigger>
          <RowContextMenuContent actions={profileActions(profile, folderId, isSelected)} />
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
    const visible = inFolder.filter(keep);
    return {
      id: `folder:${folder.id}`,
      label: folder.name,
      icon: <FolderIcon name={folder.icon} />,
      // An empty folder gets no chevron rather than one that opens onto an
      // "Empty folder" line: the count already says it, and the row it saves
      // is a row.
      expandable: inFolder.length > 0,
      expanded: filtering || expandedFolders[folder.id] !== false,
      onToggle: () => toggleFolder(folder.id),
      children: visible.map((p) => profileNode(p, folder.id)),
      emptyLabel: "No connections match",
      count: inFolder.length,
      wrap: (row) => (
        <ContextMenu key={`folder:${folder.id}`}>
          <ContextMenuTrigger asChild>{row}</ContextMenuTrigger>
          <ContextMenuContent>
            <ContextMenuItem icon={<Pencil />} onSelect={() => setEditingFolderId(folder.id)}>
              Edit folder
            </ContextMenuItem>
            <ContextMenuItem icon={<FolderPlus />} onSelect={() => newFolder()}>
              New folder
            </ContextMenuItem>
            <ContextMenuSeparator />
            <ContextMenuItem icon={<Trash2 />} variant="destructive" onSelect={() => deleteFolder(folder.id)}>
              Delete folder
            </ContextMenuItem>
          </ContextMenuContent>
        </ContextMenu>
      ),
    };
  });

  const rootProfiles = profiles
    .filter((p) => !assigned.has(p.id) && keep(p))
    .map((p) => profileNode(p, ""));

  const tree = [
    // A folder that hides all its connections under a search is noise.
    ...folderNodes.filter((node) => !filtering || node.children?.length),
    ...rootProfiles,
  ];

  return (
    <div className="flex h-full min-h-0 flex-col select-none">
      {/*
        Three lists, one rail.

        The rail is already the navigation surface and it already groups by
        connection, which is the axis all three of these share — a second tree
        in the right-hand panel would have been the same grouping at the other
        end of the window, in a slot that holds one thing at a time and is
        shared with every other module.

        Consoles and Saved queries are two tabs rather than two sections of
        one, because they are two different things: a console is a local
        scratch file written as you type, a saved query is a named, sealed,
        synced vault item. One tree holding both would promise they behave
        alike, and none of it is true.

        gap-0 and the panels carry their own padding: the default Tabs gap
        would put a band between the strip and a list that is meant to run
        under it.
      */}
      <Tabs
        value={sidebarPane}
        onValueChange={(value) => setSidebarPane(value as SidebarPane)}
        className="min-h-0 flex-1 gap-0"
      >
        {/*
          Icon only, and the glyphs are the ones the documents already use:
          the console view's Terminal, the saved view's Bookmark. A tab that
          opens a list shows the same mark as the thing it lists, so the strip
          is learnable from the rest of the module rather than on its own.

          Labels did not fit. Three at the rail's 240px floor need 233px of
          text and padding and have 206 — so one of them had to be renamed to
          something shorter and less true, or truncated to "Connectio…". The
          name lives in the tooltip and in aria-label instead, which is what
          ActionTooltip is for: a Radix tooltip alone sets aria-describedby,
          and a control with no name is one a screen reader calls "button".
        */}
        <TabsList className="w-full">
          {PANES.map(({ value, label, Icon }) => (
            <ActionTooltip key={value} label={label}>
              <TabsTrigger value={value} className="flex-1">
                <Icon />
              </TabsTrigger>
            </ActionTooltip>
          ))}
        </TabsList>

        <TabsContent value="connections" className="flex min-h-0 flex-col">
      <SidebarHeader>
        <SidebarSearch
          value={filterQuery}
          onChange={setFilterQuery}
          onClear={() => setFilterQuery("")}
          placeholder="Search connections & tables…"
        />
        {/* No "refresh connections" here. Refreshing is something you do to a
            CONNECTION — re-index its schema — and that lives on the row it
            belongs to. The list itself is read when the rail mounts and again
            after anything that changes it, so a button for it was a control
            for a state that cannot go stale on its own. */}
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

      {/* The tree is its own scroller, with its own 4px above the first row
          and 8 below the last. The empty states are not in it. */}
      {profiles.length === 0 && !loadingProfiles ? (
        <div className="min-h-0 flex-1">
          <EmptyState
            size="rail"
            title="No connections yet"
            body="Save a database's host and credentials once; the profile is encrypted into the vault."
            action={{ label: "Add connection", icon: Plus, onClick: () => setShowNewConnectionDialog(true) }}
          />
        </div>
      ) : tree.length === 0 ? (
        <div className="min-h-0 flex-1">
          <EmptyState size="rail" body={`Nothing matches “${needle}”.`} />
        </div>
      ) : (
        <TreeView nodes={tree} className="flex-1 scrollbar-gutter-stable" />
      )}
        </TabsContent>

        <TabsContent value="consoles" className="flex min-h-0 flex-col">
          <div className="min-h-0 flex-1 overflow-y-auto scrollbar-gutter-stable px-1 pb-2">
            <ConsoleFilesPanel />
          </div>
        </TabsContent>

        <TabsContent value="saved" className="flex min-h-0 flex-col">
          <div className="min-h-0 flex-1 overflow-y-auto scrollbar-gutter-stable px-1 pt-2 pb-2">
            <SavedQueriesPanel />
          </div>
        </TabsContent>
      </Tabs>

      <DeleteProfileDialog
        profile={profiles.find((p) => p.id === deletingProfileId) ?? null}
        onClose={() => setDeletingProfileId("")}
        onConfirm={async () => {
          const id = deletingProfileId;
          try {
            await deleteProfile(id);
          } catch (error) {
            // #49: the dialog stays open and says why. It used to close first,
            // and a failed delete became an unhandled rejection nobody saw.
            toast.error("Could not delete the connection", { description: errorMessage(error) });
            return;
          }
          setDeletingProfileId("");
          // #49: a deleted profile left selected made every later call fail
          // with "not found". Nothing selected, and fetchProfiles picks the
          // first one left.
          if (useDatabaseStore.getState().selectedProfileId === id) {
            setSelectedProfileId("");
          }
          void fetchProfiles();
        }}
      />

      {/* The connection dialogs live here because this is the one component
          that mounts once for the module and stays mounted with no document
          open — see DatabaseModuleOverlays. */}
      <DatabaseModuleOverlays />

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
