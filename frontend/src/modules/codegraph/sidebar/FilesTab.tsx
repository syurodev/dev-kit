import { useMemo, useRef } from "react";
import { FileCode2, Folder, FolderCode, FolderGit2, GitBranch, Package, RotateCcw } from "lucide-react";
import { SidebarInset } from "@/components/ui/sidebar-shell";
import { TreeView, type TreeNodeData } from "@/components/tree-view";
import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuSeparator, ContextMenuTrigger } from "@/components/ui/context-menu";
import { cn } from "@/lib/utils";
import type { WorkspaceService } from "@/bridge/workspace";
import type { CodeGraphGitMark } from "@/bridge/workspaceCode";
import type { FolderData } from "../codegraphStore";
import { isSourceRoot, type FileBranch } from "./treeNodes";
import { folderView, hiddenNotice, filtersActive, type FileFilter, type FlowFiles, type FolderView } from "./filesModel";
import { DOT, PrimaryBadge } from "./WorkspaceHeader";
import { folderHealth, type Tone } from "../workspace/indexHealth";
import { docKey, treeDirId, treeFileId } from "../workspace/identity";

const TOGGLE = "inline-flex h-5.5 shrink-0 items-center rounded-sm border px-2 text-code-11";

const MARK_TONE: Record<CodeGraphGitMark, string> = {
  M: "text-status-warning",
  A: "text-status-success",
  D: "text-status-danger",
  R: "text-status-info",
};

export type { FileFilter };

export type FilesTabActions = {
  onFilter: (next: FileFilter) => void;
  onToggleFolder: (id: string) => void;
  onToggleRoot: (folderId: string) => void;
  /** A click: the file in the preview tab. */
  onSelectFile: (folderId: string, path: string) => void;
  /** A double click: the file in a tab that stays. */
  onKeepFile: (folderId: string, path: string) => void;
  onOpenBeside: (folderId: string, path: string) => void;
  onFindInFolder: (folderId: string) => void;
  onShowOnly: (folderId: string) => void;
  onReindexFolder: (folderId: string) => void;
  onMakePrimary: (folderId: string) => void;
  /** A draft agent session whose working folder is this one. */
  onOpenAgent: (folderId: string) => void;
  onCopyPath: (text: string) => void;
  onResetFilters: () => void;
};

const OPEN_DEFAULT: FileFilter = { codeOnly: true, changed: false, inFlow: false };

/**
 * The workspace's files: one root per folder in the sidebar's scope, each with
 * its own expanded state, branch, change count and index health (Figma
 * `CodeGraph / Sidebar — 5 Files`, extended). Three toggles filter every root —
 * code only, changed only, only what the flow in front passes through — and a
 * root that a toggle empties says so on its own row and offers the reset, rather
 * than looking like an empty folder.
 */
export function FilesTab({
  folders,
  labels,
  primaryId,
  data,
  query,
  selected,
  unsaved,
  workspaceId,
  flow,
  filter,
  expanded,
  collapsedRoots,
  actions,
}: {
  folders: WorkspaceService[];
  labels: Record<string, string>;
  primaryId: string;
  data: Record<string, FolderData>;
  query: string;
  selected: { folderId: string; path: string } | null;
  /** Keys (docKey) of buffers with unsaved edits. */
  unsaved: ReadonlySet<string>;
  workspaceId: string;
  flow: FlowFiles;
  filter: FileFilter;
  expanded: string[];
  collapsedRoots: string[];
  actions: FilesTabActions;
}) {
  // Per-folder views are cached by what they were computed from, so one folder's
  // file list changing does not rebuild the others' trees.
  const cache = useRef(new Map<string, { inputs: unknown[]; view: FolderView }>());
  const views = useMemo(() => {
    const out = new Map<string, FolderView>();
    for (const f of folders) {
      const d = data[f.id];
      if (!d) continue;
      const unsupported = d.entries?.state === "unsupported";
      const flowHere = flow && flow.folderId === f.id ? flow.files : null;
      const inputs = [d.files, d.git, query, filter.codeOnly, filter.changed, filter.inFlow, unsupported, flowHere];
      const hit = cache.current.get(f.id);
      if (hit && hit.inputs.every((v, i) => Object.is(v, inputs[i]))) {
        out.set(f.id, hit.view);
        continue;
      }
      const view = folderView(f.id, d.files, query, filter, { unsupported, git: d.git, flow });
      cache.current.set(f.id, { inputs, view });
      out.set(f.id, view);
    }
    return out;
  }, [folders, data, query, filter, flow]);

  const nodes = useMemo(() => {
    const single = folders.length === 1;
    const open = (view: FolderView) => new Set([...expanded, ...(view.forceExpanded ?? [])]);

    const fileTrailing = (folderId: string, path: string, git: Record<string, CodeGraphGitMark>) => {
      const dirty = unsaved.has(docKey(workspaceId, folderId, path));
      const mark = git[path];
      if (!dirty && !mark) return null;
      return (
        <span className="flex items-center gap-1.5">
          {dirty ? (
            <span aria-label="unsaved" title="Unsaved edits" className="text-text-primary">
              ●
            </span>
          ) : null}
          {mark ? <span className={MARK_TONE[mark]}>{mark}</span> : null}
        </span>
      );
    };

    const branchNode = (f: WorkspaceService, view: FolderView, d: FolderData, b: FileBranch, isOpen: Set<string>): TreeNodeData =>
      b.isDir
        ? {
            id: treeDirId(f.id, b.path),
            label: b.label ?? b.name,
            title: b.path,
            icon: b.isPackage ? <Package /> : isSourceRoot(b.path) ? <FolderCode /> : <Folder />,
            expandable: b.children.length > 0,
            expanded: isOpen.has(treeDirId(f.id, b.path)) || (view.forceExpanded?.has(b.path) ?? false),
            onToggle: () => actions.onToggleFolder(treeDirId(f.id, b.path)),
            children: b.children.map((c) => branchNode(f, view, d, c, isOpen)),
          }
        : {
            id: treeFileId(f.id, b.path),
            label: b.name,
            title: `${labels[f.id]} › ${b.path}`,
            icon: <FileCode2 />,
            selected: selected?.folderId === f.id && selected.path === b.path,
            onSelect: () => actions.onSelectFile(f.id, b.path),
            onActivate: () => actions.onKeepFile(f.id, b.path),
            trailing: fileTrailing(f.id, b.path, d.git),
            wrap: (row) => (
              <ContextMenu key={`file:${f.id}:${b.path}`}>
                <ContextMenuTrigger asChild>{row}</ContextMenuTrigger>
                <ContextMenuContent className="w-56">
                  <ContextMenuItem onSelect={() => actions.onKeepFile(f.id, b.path)}>Open in new tab</ContextMenuItem>
                  <ContextMenuItem onSelect={() => actions.onOpenBeside(f.id, b.path)}>Open beside</ContextMenuItem>
                  <ContextMenuSeparator />
                  <ContextMenuItem onSelect={() => actions.onCopyPath(b.path)}>Copy relative path</ContextMenuItem>
                  <ContextMenuItem onSelect={() => actions.onFindInFolder(f.id)}>Find in {labels[f.id]}…</ContextMenuItem>
                  <ContextMenuItem onSelect={() => actions.onOpenAgent(f.id)}>Open agent in {labels[f.id]}</ContextMenuItem>
                </ContextMenuContent>
              </ContextMenu>
            ),
          };

    const rootNode = (f: WorkspaceService): TreeNodeData => {
      const d = data[f.id];
      const view = views.get(f.id);
      const open = !collapsedRoots.includes(f.id);
      const health = folderHealth(d?.state ?? undefined, d?.entries ?? undefined, d?.indexStatus ?? undefined, Date.now());
      const isOpen = view ? new Set([...expanded, ...(view.forceExpanded ?? [])]) : new Set<string>();
      const loading = d?.filesStatus === "loading" && d.files.length === 0;
      const notice = view ? hiddenNotice(view) : "";
      const changed = d?.state && d.state.changed > 0 ? d.state.changed : 0;
      const root: TreeNodeData = {
        id: `root:${f.id}`,
        label: labels[f.id],
        title: f.path,
        icon: d?.state?.isRepo ? <FolderGit2 /> : <Folder />,
        count: view ? view.shown : undefined,
        sticky: 0,
        expandable: true,
        expanded: open,
        onToggle: () => actions.onToggleRoot(f.id),
        loading: open && loading,
        emptyLabel: (() => {
          if (!d || loading) return undefined;
          if (!d.state?.accessible && d.state) return <span className="text-status-danger">{d.state.error || "Folder unavailable"}</span>;
          if (d.filesStatus === "error") return <span className="text-status-danger">Could not list files: {d.filesError}</span>;
          if (view && view.total > 0 && view.shown === 0) {
            return (
              <span>
                {notice || "No files match"}
                {view.hiddenBy.length > 0 ? (
                  <>
                    {" · "}
                    <button type="button" className="underline underline-offset-2 hover:text-text-primary" onClick={actions.onResetFilters}>
                      Reset filters
                    </button>
                  </>
                ) : null}
              </span>
            );
          }
          return view && view.total === 0 ? "No files" : undefined;
        })(),
        trailing: (
          <span className="flex items-center gap-1.5">
            {f.id === primaryId && folders.length > 1 ? <PrimaryBadge /> : null}
            {d?.state?.isRepo && d.state.branch ? (
              <span className="hidden max-w-24 items-center gap-0.5 truncate text-code-11 text-text-tertiary @[18rem]/rail:inline-flex" title={`Branch ${d.state.branch}`}>
                <GitBranch className="size-2.5 shrink-0" aria-hidden />
                <span className="truncate">{d.state.branch}</span>
              </span>
            ) : null}
            {changed > 0 ? <span title={`${changed} changed file${changed === 1 ? "" : "s"}`} className="text-code-11 text-status-warning">{changed}</span> : null}
            <span className={cn("size-1.5 shrink-0 rounded-full", DOT[health.tone as Tone])} title={`${health.label}. ${health.detail}`} aria-label={health.label} />
          </span>
        ),
        wrap: (row) => (
          <ContextMenu key={`root:${f.id}`}>
            <ContextMenuTrigger asChild>{row}</ContextMenuTrigger>
            <ContextMenuContent className="w-60">
              <ContextMenuItem onSelect={() => actions.onFindInFolder(f.id)}>Find in this folder…</ContextMenuItem>
              {folders.length > 1 ? <ContextMenuItem onSelect={() => actions.onShowOnly(f.id)}>Show only this folder</ContextMenuItem> : null}
              <ContextMenuItem onSelect={() => actions.onReindexFolder(f.id)}>Reindex this folder</ContextMenuItem>
              <ContextMenuItem onSelect={() => actions.onOpenAgent(f.id)}>Open agent here</ContextMenuItem>
              {f.id !== primaryId ? <ContextMenuItem onSelect={() => actions.onMakePrimary(f.id)}>Make primary</ContextMenuItem> : null}
              <ContextMenuSeparator />
              <ContextMenuItem onSelect={() => actions.onCopyPath(f.path)}>Copy path</ContextMenuItem>
            </ContextMenuContent>
          </ContextMenu>
        ),
        children: open && view && d ? view.branches.map((b) => branchNode(f, view, d, b, isOpen)) : [],
      };
      return root;
    };

    // One folder in scope: its files directly, like the project tree used to be —
    // a lone root heading says nothing. The folder's problems still have to show.
    if (single) {
      const f = folders[0];
      const d = data[f.id];
      const view = views.get(f.id);
      if (d && view && view.shown > 0) {
        const isOpen = open(view);
        return view.branches.map((b) => branchNode(f, view, d, b, isOpen));
      }
    }
    return folders.map(rootNode);
  }, [folders, labels, primaryId, data, views, selected, unsaved, workspaceId, expanded, collapsedRoots, actions]);

  const hiding = useMemo(() => folders.reduce((n, f) => {
    const v = views.get(f.id);
    return n + (v && !query.trim() ? v.total - v.shown : 0);
  }, 0), [folders, views, query]);

  const changedCount = folders.reduce((n, f) => n + Object.keys(data[f.id]?.git ?? {}).length, 0);
  const allUnsupported = folders.length > 0 && folders.every((f) => data[f.id]?.entries?.state === "unsupported");
  const toggle = (key: keyof FileFilter, label: string, hint: string, disabled = false) => (
    <button
      type="button"
      title={hint}
      aria-pressed={filter[key]}
      disabled={disabled}
      onClick={() => actions.onFilter({ ...filter, [key]: !filter[key] })}
      className={cn(
        TOGGLE,
        filter[key]
          ? "border-(--border-default) bg-surface-raised text-text-primary"
          : "border-border-subtle text-text-tertiary hover:text-text-secondary",
        "disabled:opacity-50",
      )}
    >
      {label}
    </button>
  );

  return (
    <div className="@container/rail flex min-h-0 flex-1 flex-col gap-3">
      <SidebarInset className="flex flex-wrap items-center gap-1" role="group" aria-label="Filter files">
        {allUnsupported
          ? null
          : toggle("codeOnly", "CODE", "Source code only, in any language — hides build files, config, resources and docs. Not applied to folders no analyzer reads.")}
        {toggle("changed", changedCount > 0 ? `CHANGED · ${changedCount}` : "CHANGED", "Files with uncommitted changes, per each folder's own git status")}
        {toggle("inFlow", "IN FLOW", "Files the flow in front passes through (only in that flow's folder)", flow === null && !filter.inFlow)}
      </SidebarInset>
      {filtersActive(filter, OPEN_DEFAULT) || hiding > 0 ? (
        <SidebarInset>
          <p role="status" className="flex items-center gap-1.5 text-caption-11 text-text-tertiary">
            {hiding > 0 ? `${hiding} file${hiding === 1 ? "" : "s"} hidden by filters` : "Filters changed"}
            {filtersActive(filter, OPEN_DEFAULT) ? (
              <button type="button" className="inline-flex items-center gap-0.5 underline underline-offset-2 hover:text-text-primary" onClick={actions.onResetFilters}>
                <RotateCcw className="size-2.5" aria-hidden />
                Reset
              </button>
            ) : null}
          </p>
        </SidebarInset>
      ) : null}
      {nodes.length > 0 ? (
        <TreeView nodes={nodes} keyboardNavigation revealNodeId={selected ? treeFileId(selected.folderId, selected.path) : null} className="min-h-0 flex-1 scrollbar-gutter-stable" />
      ) : (
        <p className="min-h-0 flex-1 px-2 py-3 text-center text-body-12 text-text-tertiary">
          {folders.length === 0 ? "No folders in view." : "No files match."}
        </p>
      )}
    </div>
  );
}
