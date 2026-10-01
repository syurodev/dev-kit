import { useEffect, useState, type ReactNode } from "react";
import { toast } from "sonner";
import {
  Check,
  Copy,
  Download,
  ExternalLink,
  Folder,
  FolderPlus,
  Globe,
  Layers,
  MoreHorizontal,
  Pencil,
  Plus,
  SquarePlus,
  Trash2,
  Upload,
  Variable,
} from "lucide-react";
import { useShallow } from "zustand/react/shallow";
import {
  deleteCollectionTree,
  deleteEnvironment,
  deleteRequest,
  duplicateEnvironment,
  getRequest,
  saveCollection,
  saveRequest,
  type ApiCollection,
  type ApiEnvironment,
  type ApiRequest,
  type ExportScope,
} from "@/bridge/apiclient";
import { errorMessage } from "@/bridge/errors";
import { ActionTooltip } from "@/components/ui/action-tooltip";
import { Button } from "@/components/ui/button";
import { ContextMenu, ContextMenuTrigger } from "@/components/ui/context-menu";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { Empty, EmptyContent, EmptyDescription, EmptyTitle } from "@/components/ui/empty";
import { SidebarHeader, SidebarSearch, SidebarSection } from "@/components/ui/sidebar-shell";
import { RowContextMenuContent, RowDropdownMenuContent, type RowAction } from "@/components/row-actions";
import { TreeRowAction, TreeRowTag, TreeView, type TreeNodeData } from "@/components/tree-view";
import { moduleTabs, useWorkbenchTabsStore } from "@/stores/workbenchTabs";
import { tagAsks, useApiClientStore } from "./apiclientStore";
import { ConfirmDialog, ExportDialog, ImportDialog, NameDialog } from "./Dialogs";
import {
  closeTabByParams,
  MODULE,
  openNewRequest,
  openRequest,
  openVariables,
  REQUEST_VIEW,
  VARIABLES_VIEW,
} from "./documents";
import { methodTone } from "./requestModel";
import { ancestorsOf, buildSidebarTree, descendantsOf, type SidebarEntry } from "./sidebarTree";

function withMenus(node: TreeNodeData, name: string, actions: RowAction[]): TreeNodeData {
  return {
    ...node,
    actions: (
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <ActionTooltip label={`Actions for ${name}`}>
            <TreeRowAction aria-label={`Actions for ${name}`}>
              <MoreHorizontal />
            </TreeRowAction>
          </ActionTooltip>
        </DropdownMenuTrigger>
        <RowDropdownMenuContent actions={actions} />
      </DropdownMenu>
    ),
    wrap: (row) => (
      <ContextMenu key={node.id}>
        <ContextMenuTrigger asChild>{row}</ContextMenuTrigger>
        <RowContextMenuContent actions={actions} />
      </ContextMenu>
    ),
  };
}

/** The method as a fixed-width coloured chip, the way every client draws it. */
function MethodMark({ method }: { method: string }) {
  const label = method.toUpperCase() === "DELETE" ? "DEL" : method.toUpperCase() === "OPTIONS" ? "OPT" : method.toUpperCase();
  return <span className={`inline-block w-9 font-mono text-code-11 ${methodTone(method)}`}>{label}</span>;
}

type Dialog =
  | { kind: "newFolder"; parentId: string }
  | { kind: "renameFolder"; collection: ApiCollection }
  | { kind: "renameRequest"; request: ApiRequest }
  | { kind: "deleteFolder"; collection: ApiCollection; requests: number }
  | { kind: "deleteRequest"; request: ApiRequest }
  | { kind: "deleteEnvironment"; environment: ApiEnvironment }
  | null;

/**
 * The API Client rail: collections and their folders and requests, then the
 * environments and the global variables.
 *
 * Postman's layout on purpose — the people this is for have it in their
 * hands already — with DevKit's differences where they matter: an
 * environment shows its env tag, and the one that asks before every send is
 * marked, because "which environment am I pointed at" is the question that
 * decides whether a click reaches production.
 */
export function ApiClientSidebar() {
  const { requests, collections, environments, error, activeEnvironmentId, expanded } = useApiClientStore(
    useShallow((s) => ({
      requests: s.requests,
      collections: s.collections,
      environments: s.environments,
      error: s.error,
      activeEnvironmentId: s.activeEnvironmentId,
      expanded: s.expanded,
    })),
  );
  const { refresh, toggleExpanded, expand, setActiveEnvironment, setImportKind } = useApiClientStore.getState();
  const importKind = useApiClientStore((s) => s.importKind);
  const activeTab = useWorkbenchTabsStore(
    useShallow((s) => {
      const tabs = moduleTabs(s.modules, MODULE);
      const tab = tabs.openTabs.find((t) => t.id === tabs.activeTabId);
      return {
        viewId: tab?.viewId ?? "",
        requestId: typeof tab?.params?.requestId === "string" ? tab.params.requestId : "",
        scope: typeof tab?.params?.scope === "string" ? tab.params.scope : "",
        id: typeof tab?.params?.id === "string" ? tab.params.id : "",
      };
    }),
  );

  const [filter, setFilter] = useState("");
  const [dialog, setDialog] = useState<Dialog>(null);
  const [envOpen, setEnvOpen] = useState(true);
  const [exporting, setExporting] = useState<{ scope: ExportScope; title: string } | null>(null);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  useEffect(() => {
    if (error) toast.error("Could not load the API client", { description: error });
  }, [error]);

  // The request in front is shown inside its open folders, so the highlight
  // is never hidden in a collapsed one.
  useEffect(() => {
    if (!activeTab.requestId || !requests || !collections) return;
    const request = requests.find((r) => r.id === activeTab.requestId);
    if (request?.collectionId) for (const id of ancestorsOf(collections, request.collectionId)) expand(id);
  }, [activeTab.requestId, requests, collections, expand]);

  async function attempt(what: string, call: () => Promise<unknown>) {
    try {
      await call();
    } catch (e) {
      toast.error(what, { description: errorMessage(e) });
    }
  }

  async function duplicateRequest(request: ApiRequest) {
    const full = await getRequest(request.id);
    const copy = await saveRequest({ ...full, id: "", name: `${full.name} copy` });
    await refresh();
    openRequest(copy.id, copy.name);
  }

  function folderActions(collection: ApiCollection): RowAction[] {
    return [
      { kind: "item", icon: <SquarePlus />, label: "New request", onSelect: () => openNewRequest({ collectionId: collection.id }) },
      { kind: "item", icon: <FolderPlus />, label: "New folder", onSelect: () => setDialog({ kind: "newFolder", parentId: collection.id }) },
      { kind: "separator" },
      {
        kind: "item",
        icon: <Variable />,
        label: "Variables…",
        onSelect: () => openVariables("collection", collection.id, `${collection.name} · variables`),
      },
      { kind: "item", icon: <Pencil />, label: "Rename…", onSelect: () => setDialog({ kind: "renameFolder", collection }) },
      {
        kind: "item",
        icon: <Upload />,
        label: "Export…",
        onSelect: () => setExporting({ scope: { kind: "collection", id: collection.id }, title: collection.name }),
      },
      { kind: "separator" },
      {
        kind: "item",
        icon: <Trash2 />,
        label: "Delete…",
        destructive: true,
        onSelect: () => {
          const doomed = descendantsOf(collections ?? [], collection.id);
          const count = (requests ?? []).filter((r) => doomed.has(r.collectionId)).length;
          setDialog({ kind: "deleteFolder", collection, requests: count });
        },
      },
    ];
  }

  function requestActions(request: ApiRequest): RowAction[] {
    return [
      { kind: "item", icon: <ExternalLink />, label: "Open", onSelect: () => openRequest(request.id, request.name) },
      {
        kind: "item",
        icon: <Copy />,
        label: "Duplicate",
        onSelect: () => void attempt("Could not duplicate the request", () => duplicateRequest(request)),
      },
      { kind: "item", icon: <Pencil />, label: "Rename…", onSelect: () => setDialog({ kind: "renameRequest", request }) },
      { kind: "separator" },
      { kind: "item", icon: <Trash2 />, label: "Delete…", destructive: true, onSelect: () => setDialog({ kind: "deleteRequest", request }) },
    ];
  }

  function toNode(entry: SidebarEntry): TreeNodeData {
    if (entry.kind === "folder") {
      const { collection } = entry;
      const open = filter.trim() ? true : !!expanded[collection.id];
      return withMenus(
        {
          id: `c:${collection.id}`,
          label: collection.name,
          icon: collection.parentId ? <Folder /> : <Layers />,
          count: entry.total || undefined,
          expandable: true,
          expanded: open,
          onToggle: () => toggleExpanded(collection.id),
          onSelect: () => toggleExpanded(collection.id),
          children: entry.children.map(toNode),
          emptyLabel: (
            <button
              type="button"
              className="text-caption-11 text-text-tertiary hover:text-text-primary"
              onClick={() => openNewRequest({ collectionId: collection.id })}
            >
              Add a request
            </button>
          ),
        },
        collection.name,
        folderActions(collection),
      );
    }
    const { request } = entry;
    return withMenus(
      {
        id: `r:${request.id}`,
        // The method leads the label rather than taking the icon slot: the
        // slot is a 14px glyph, and "DELETE" is not one.
        label: (
          <>
            <MethodMark method={request.method} />
            {request.name}
          </>
        ),
        title: `${request.method} ${request.url}`,
        selected: activeTab.viewId === REQUEST_VIEW && activeTab.requestId === request.id,
        onSelect: () => openRequest(request.id, request.name, { preview: true }),
        onActivate: () => openRequest(request.id, request.name),
      },
      request.name,
      requestActions(request),
    );
  }

  const tree = requests && collections ? buildSidebarTree(collections, requests, filter).map(toNode) : [];
  const loaded = !!requests && !!collections;
  const empty = loaded && requests.length === 0 && collections.length === 0;

  const newMenu = (
    <DropdownMenu>
      <ActionTooltip label="New…" wrap>
        <DropdownMenuTrigger asChild>
          <Button variant="ghost" size="icon-xs" aria-label="New">
            <Plus className="size-3" />
          </Button>
        </DropdownMenuTrigger>
      </ActionTooltip>
      <DropdownMenuContent align="end">
        <DropdownMenuItem onSelect={() => openNewRequest()}>
          <SquarePlus /> New request
        </DropdownMenuItem>
        <DropdownMenuItem onSelect={() => setDialog({ kind: "newFolder", parentId: "" })}>
          <Layers /> New collection
        </DropdownMenuItem>
        <DropdownMenuItem onSelect={() => openVariables("environment", "new", "New environment")}>
          <Globe /> New environment
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem onSelect={() => setImportKind("curl")}>
          <Download /> Import cURL…
        </DropdownMenuItem>
        <DropdownMenuItem onSelect={() => setImportKind("file")}>
          <Download /> Import file…
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );

  return (
    <div className="flex h-full min-h-0 flex-col select-none">
      <SidebarHeader>
        <SidebarSearch value={filter} onChange={setFilter} onClear={() => setFilter("")} placeholder="Search requests…" />
        {newMenu}
      </SidebarHeader>

      <SidebarSection
        label="Collections"
        first
        actions={
          <ActionTooltip label="New collection" wrap>
            <Button variant="ghost" size="icon-xs" onClick={() => setDialog({ kind: "newFolder", parentId: "" })}>
              <FolderPlus className="size-3" />
            </Button>
          </ActionTooltip>
        }
      />
      {!loaded ? (
        <div className="min-h-0 flex-1" />
      ) : empty ? (
        <div className="min-h-0 flex-1">
          <Empty className="px-4 py-6">
            <EmptyTitle className="text-body-12 text-text-tertiary">No requests yet.</EmptyTitle>
            <EmptyDescription>
              Start a request, paste a curl command, or bring a collection over from Postman.
            </EmptyDescription>
            <EmptyContent className="flex flex-col gap-2">
              <Button variant="outline" size="sm" onClick={() => openNewRequest()}>
                <SquarePlus className="size-3" /> New request
              </Button>
              <Button variant="ghost" size="sm" onClick={() => setImportKind("file")}>
                <Download className="size-3" /> Import…
              </Button>
            </EmptyContent>
          </Empty>
        </div>
      ) : tree.length === 0 ? (
        <div className="min-h-0 flex-1">
          <Empty className="px-4 py-6">
            <EmptyDescription>Nothing matches “{filter.trim()}”.</EmptyDescription>
          </Empty>
        </div>
      ) : (
        <TreeView nodes={tree} className="min-h-0 flex-1 scrollbar-gutter-stable" />
      )}

      <EnvironmentsSection
        open={envOpen}
        onToggle={() => setEnvOpen((value) => !value)}
        environments={environments}
        activeEnvironmentId={activeEnvironmentId}
        selected={activeTab.viewId === VARIABLES_VIEW ? `${activeTab.scope}:${activeTab.id}` : ""}
        onActivate={setActiveEnvironment}
        onDuplicate={(environment) =>
          void attempt("Could not duplicate the environment", async () => {
            const copy = await duplicateEnvironment(environment.id);
            await refresh();
            openVariables("environment", copy.id, copy.name);
          })
        }
        onDelete={(environment) => setDialog({ kind: "deleteEnvironment", environment })}
        onExport={(scope, title) => setExporting({ scope, title })}
      />
      <ExportDialog scope={exporting?.scope ?? null} title={exporting?.title ?? ""} onClose={() => setExporting(null)} />

      <NameDialog
        open={dialog?.kind === "newFolder"}
        title={dialog?.kind === "newFolder" && dialog.parentId ? "New folder" : "New collection"}
        placeholder="Name"
        submitLabel="Create"
        onClose={() => setDialog(null)}
        onSubmit={async (name) => {
          if (dialog?.kind !== "newFolder") return;
          const created = await saveCollection({ name, parentId: dialog.parentId });
          if (dialog.parentId) expand(dialog.parentId);
          expand(created.id);
          await refresh();
        }}
      />
      <NameDialog
        open={dialog?.kind === "renameFolder"}
        title="Rename"
        initial={dialog?.kind === "renameFolder" ? dialog.collection.name : ""}
        onClose={() => setDialog(null)}
        onSubmit={async (name) => {
          if (dialog?.kind !== "renameFolder") return;
          await saveCollection({ ...dialog.collection, name });
          await refresh();
        }}
      />
      <NameDialog
        open={dialog?.kind === "renameRequest"}
        title="Rename request"
        initial={dialog?.kind === "renameRequest" ? dialog.request.name : ""}
        onClose={() => setDialog(null)}
        onSubmit={async (name) => {
          if (dialog?.kind !== "renameRequest") return;
          const full = await getRequest(dialog.request.id);
          await saveRequest({ ...full, name });
          // An open editor holds the old name; it re-reads on its next load,
          // and its own baseline would otherwise call the rename "unsaved".
          useApiClientStore.getState().dropEditor(`request:${full.id}`);
          await refresh();
        }}
      />
      <ConfirmDialog
        open={dialog?.kind === "deleteFolder"}
        title={dialog?.kind === "deleteFolder" ? `Delete “${dialog.collection.name}”?` : ""}
        description={
          dialog?.kind === "deleteFolder"
            ? dialog.requests > 0
              ? `Its ${dialog.requests} request${dialog.requests === 1 ? "" : "s"} and every folder inside go with it, on every device this vault syncs to. This cannot be undone.`
              : "It is empty. This cannot be undone."
            : ""
        }
        onClose={() => setDialog(null)}
        onConfirm={() => {
          if (dialog?.kind !== "deleteFolder") return;
          const target = dialog.collection;
          setDialog(null);
          void attempt(`Could not delete ${target.name}`, async () => {
            const removed = await deleteCollectionTree(target.id);
            await refresh();
            toast.success(`${target.name} deleted${removed ? ` with ${removed} request${removed === 1 ? "" : "s"}` : ""}`);
          });
        }}
      />
      <ConfirmDialog
        open={dialog?.kind === "deleteRequest"}
        title={dialog?.kind === "deleteRequest" ? `Delete “${dialog.request.name}”?` : ""}
        description="It is removed on every device this vault syncs to, with what this machine remembers sending. This cannot be undone."
        onClose={() => setDialog(null)}
        onConfirm={() => {
          if (dialog?.kind !== "deleteRequest") return;
          const target = dialog.request;
          setDialog(null);
          void attempt(`Could not delete ${target.name}`, async () => {
            await deleteRequest(target.id);
            closeTabByParams(REQUEST_VIEW, { requestId: target.id });
            useApiClientStore.getState().dropEditor(`request:${target.id}`);
            await refresh();
          });
        }}
      />
      <ConfirmDialog
        open={dialog?.kind === "deleteEnvironment"}
        title={dialog?.kind === "deleteEnvironment" ? `Delete “${dialog.environment.name}”?` : ""}
        description="Its variables, secret ones included, are removed on every device this vault syncs to. Requests are not touched."
        onClose={() => setDialog(null)}
        onConfirm={() => {
          if (dialog?.kind !== "deleteEnvironment") return;
          const target = dialog.environment;
          setDialog(null);
          void attempt(`Could not delete ${target.name}`, async () => {
            await deleteEnvironment(target.id);
            closeTabByParams(VARIABLES_VIEW, { scope: "environment", id: target.id });
            await refresh();
          });
        }}
      />
      <ImportDialog
        open={importKind !== null}
        initialKind={importKind ?? "curl"}
        onClose={() => setImportKind(null)}
        onImported={(result) => {
          if (result?.collectionId) expand(result.collectionId);
          if (result?.environmentId) setActiveEnvironment(result.environmentId);
          void refresh();
        }}
      />
    </div>
  );
}

/**
 * Environments under the tree: Globals, then each environment with its tag.
 * Clicking one opens its variables; the check marks the one sends go
 * against, and "Use" makes it so.
 */
function EnvironmentsSection({
  open,
  onToggle,
  environments,
  activeEnvironmentId,
  selected,
  onActivate,
  onDuplicate,
  onDelete,
  onExport,
}: {
  open: boolean;
  onToggle: () => void;
  environments: ApiEnvironment[] | null;
  activeEnvironmentId: string;
  selected: string;
  onActivate: (id: string) => void;
  onDuplicate: (environment: ApiEnvironment) => void;
  onDelete: (environment: ApiEnvironment) => void;
  onExport: (scope: ExportScope, title: string) => void;
}) {
  const nodes: TreeNodeData[] = [
    withMenus(
      {
        id: "globals",
        label: "Globals",
        icon: <Globe />,
        selected: selected === "globals:",
        onSelect: () => openVariables("globals", "", "Globals"),
      },
      "Globals",
      [
        { kind: "item", icon: <Variable />, label: "Edit variables", onSelect: () => openVariables("globals", "", "Globals") },
        { kind: "item", icon: <Upload />, label: "Export…", onSelect: () => onExport({ kind: "globals", id: "" }, "Globals") },
      ],
    ),
    ...(environments ?? []).map((environment): TreeNodeData => {
      const active = environment.id === activeEnvironmentId;
      const actions: RowAction[] = [
        { kind: "item", icon: <Check />, label: "Use for sending", disabled: active, onSelect: () => onActivate(environment.id) },
        {
          kind: "item",
          icon: <Variable />,
          label: "Edit variables",
          onSelect: () => openVariables("environment", environment.id, environment.name),
        },
        { kind: "item", icon: <Copy />, label: "Duplicate", onSelect: () => onDuplicate(environment) },
        {
          kind: "item",
          icon: <Upload />,
          label: "Export…",
          onSelect: () => onExport({ kind: "environment", id: environment.id }, environment.name),
        },
        { kind: "separator" },
        { kind: "item", icon: <Trash2 />, label: "Delete…", destructive: true, onSelect: () => onDelete(environment) },
      ];
      return withMenus(
        {
          id: `e:${environment.id}`,
          label: environment.name,
          icon: active ? <Check className="text-status-success" /> : <Variable />,
          trailing: <EnvTagChip tag={environment.env} />,
          title: active ? `${environment.name} — sends go here` : `${environment.name} — double-click to send here`,
          selected: selected === `environment:${environment.id}`,
          onSelect: () => openVariables("environment", environment.id, environment.name),
          onActivate: () => onActivate(environment.id),
        },
        environment.name,
        actions,
      );
    }),
  ];
  return (
    <div className="flex max-h-2/5 shrink-0 flex-col border-t border-border-subtle">
      <SidebarSection
        label="Environments"
        count={environments?.length}
        open={open}
        onToggle={onToggle}
        actions={
          <ActionTooltip label="New environment" wrap>
            <Button variant="ghost" size="icon-xs" onClick={() => openVariables("environment", "new", "New environment")}>
              <Plus className="size-3" />
            </Button>
          </ActionTooltip>
        }
      />
      {open ? <TreeView nodes={nodes} className="min-h-0 pb-2" /> : null}
    </div>
  );
}

/** An environment's tag, red when sends under it ask first. */
export function EnvTagChip({ tag }: { tag: string }): ReactNode {
  const envTags = useApiClientStore((s) => s.envTags);
  return <TreeRowTag danger={tagAsks(envTags, tag)}>{tag}</TreeRowTag>;
}
