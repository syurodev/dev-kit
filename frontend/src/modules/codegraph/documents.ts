import { getModule, viewIcon } from "@/modules/registry";
import { moduleTabs, useWorkbenchTabsStore, type WorkbenchTab } from "@/stores/workbenchTabs";
import { focusedGroup, shownTab, tabsOfGroup } from "@/stores/documentGroups";
import type { CodeGraphEntryPoint } from "@/bridge/codegraph";
import { baseName, folderLabels, normalizeRelPath } from "./workspace/identity";

/**
 * Code Graph's documents, each a tab.
 *
 * A flow, a file and the plan used to be one canvas the sidebar switched
 * between — so a flow and the file it passes through could never be on screen
 * together, and picking a file threw the graph away. As tabs they go wherever
 * tabs go: side by side in document groups (⌘\, ⌘-click, Open beside), kept
 * while you look at something else, and restored after a relaunch.
 *
 * Every document names its owner in its params — `workspaceId` and `folderId`
 * — so a tab means the same thing whatever the sidebar's scope, the primary
 * folder or the workspace in the title bar is now. Params are persisted: only
 * identifiers and relative paths go in, never text.
 *
 * A single click opens the module's preview tab, which the next single click
 * replaces, so browsing the rail does not leave a trail; a double-click, an
 * edit or Open in new tab keeps it.
 */
export const MODULE = "codegraph";
export const HOME_VIEW = "codegraph.explorer";
export const FLOW_VIEW = "codegraph.flow";
export const FILE_VIEW = "codegraph.file";
export const PLAN_VIEW = "codegraph.plan";

export type OpenOptions = { preview?: boolean; beside?: boolean };

function open(viewId: string, title: string, params: Record<string, unknown> | undefined, options: OpenOptions) {
  // Start the chunk in the click, not in the render that needs it.
  void getModule(MODULE)?.views.find((v) => v.id === viewId)?.preload?.().catch(() => undefined);
  return useWorkbenchTabsStore.getState().openTab({
    moduleId: MODULE,
    viewId,
    title,
    icon: viewIcon(MODULE, viewId),
    params,
    preview: options.preview ?? false,
    beside: options.beside,
  });
}

/** Who a document belongs to. */
export type DocOwner = { workspaceId: string; folderId: string };

export function openEntryPointFlow(owner: DocOwner, ep: CodeGraphEntryPoint, options: OpenOptions = { preview: true }): string {
  return open(FLOW_VIEW, `${ep.verb} ${ep.target}`, { ...owner, root: ep.entryNode, entryPoint: ep.id }, options);
}

/**
 * The entry point a flow tab was opened from. A tab restored from before
 * entry points had their name carries it as `endpoint`.
 */
export function entryPointParam(params: Record<string, unknown> | undefined): string | undefined {
  const id = params?.entryPoint ?? params?.endpoint;
  return typeof id === "string" ? id : undefined;
}

/**
 * A flow from a node alone: a class or method from the @ search, or a node
 * another module points at — drawn without an entry point in front of it.
 */
export function openNodeFlow(owner: DocOwner, nodeId: string, title: string, options: OpenOptions = { preview: true }): string {
  return open(FLOW_VIEW, title, { ...owner, root: nodeId }, options);
}

export function openFileDoc(owner: DocOwner & { path: string }, options: OpenOptions = { preview: true }): string {
  const path = normalizeRelPath(owner.path);
  const id = open(FILE_VIEW, baseName(path), { workspaceId: owner.workspaceId, folderId: owner.folderId, path }, options);
  syncFileTabTitles();
  return id;
}

export function openPlanDoc(owner: DocOwner, options: OpenOptions = {}): string {
  return open(PLAN_VIEW, "Plan", { ...owner }, options);
}

/** Keeps a preview tab — an edit, or a second look, says it is wanted. */
export function keepTab(id: string) {
  const tab = moduleTabs(useWorkbenchTabsStore.getState().modules, MODULE).openTabs.find((t) => t.id === id);
  if (tab?.preview) useWorkbenchTabsStore.getState().pinTab(id);
}

/** What a Code Graph tab shows, read off its view and params. */
export type ActiveDoc = {
  kind: "home" | "flow" | "file" | "plan";
  /** Absent on a tab saved before documents named their owner (see resolveOwner). */
  workspaceId?: string;
  folderId?: string;
  root?: string;
  entryPoint?: string;
  path?: string;
};

const str = (v: unknown) => (typeof v === "string" && v ? v : undefined);

export function describeDocument(tab: WorkbenchTab | null): ActiveDoc {
  return describeParams(tab?.viewId, tab?.params);
}

/** describeDocument for a view that has its params but not its tab — a document rendering itself. */
export function describeParams(viewId: string | undefined, params: Record<string, unknown> | undefined): ActiveDoc {
  const p = params ?? {};
  const owner = { workspaceId: str(p.workspaceId), folderId: str(p.folderId) };
  switch (viewId) {
    case FLOW_VIEW:
      return { kind: "flow", ...owner, root: str(p.root), entryPoint: entryPointParam(p) };
    case FILE_VIEW:
      return { kind: "file", ...owner, path: str(p.path) };
    case PLAN_VIEW:
      return { kind: "plan", ...owner };
    default:
      return { kind: "home" };
  }
}

export function documentsOf(): Array<{ tab: WorkbenchTab; doc: ActiveDoc }> {
  return moduleTabs(useWorkbenchTabsStore.getState().modules, MODULE).openTabs.map((tab) => ({ tab, doc: describeDocument(tab) }));
}

/**
 * The document in front in the focused group — the one Back/Forward, the
 * breadcrumb and the Outline are about. Not the module's last-active tab: with
 * documents split across groups only the focused group's is the one you are
 * working in.
 */
export function focusedDocument(): { tab: WorkbenchTab; doc: ActiveDoc; groupId: string } | null {
  const state = useWorkbenchTabsStore.getState();
  const group = focusedGroup(state);
  if (!group || group.moduleId !== MODULE) return null;
  const id = shownTab(state, group);
  const tab = tabsOfGroup(state, group).find((t) => t.id === id);
  return tab ? { tab, doc: describeDocument(tab), groupId: group.id } : null;
}

/**
 * Closes the tabs of a folder that is no longer part of the workspace — except
 * files with unsaved edits, which stay (their buffers are orphans the person
 * can copy out, save elsewhere or discard).
 */
export function closeFolderDocuments(folderId: string, keepFile: (path: string) => boolean) {
  const store = useWorkbenchTabsStore.getState();
  for (const { tab, doc } of documentsOf()) {
    if (doc.folderId !== folderId) continue;
    if (doc.kind === "file" && doc.path && keepFile(doc.path)) continue;
    store.closeTab(tab.id);
  }
}

/**
 * File tab titles: the file's name, and its folder only when another open file
 * has the same name — so `config.ts` in two folders reads as two files, while
 * every other tab stays as short as it was.
 */
export function syncFileTabTitles(): void {
  useWorkbenchTabsStore.setState((state) => {
    const tabs = moduleTabs(state.modules, MODULE);
    const files = tabs.openTabs.filter((t) => t.viewId === FILE_VIEW);
    const byName = new Map<string, Set<string>>();
    for (const t of files) {
      const d = describeDocument(t);
      const name = baseName(d.path ?? "");
      byName.set(name, (byName.get(name) ?? new Set()).add(`${d.folderId}\u0000${d.path}`));
    }
    const labels = folderLabelsFor();
    let changed = false;
    const openTabs = tabs.openTabs.map((t) => {
      if (t.viewId !== FILE_VIEW) return t;
      const d = describeDocument(t);
      const name = baseName(d.path ?? "");
      const ambiguous = (byName.get(name)?.size ?? 0) > 1;
      const label = ambiguous && d.folderId ? labels[d.folderId] : undefined;
      const title = label ? `${name} · ${label}` : name;
      if (t.title === title) return t;
      changed = true;
      return { ...t, title };
    });
    return changed ? { modules: { ...state.modules, [MODULE]: { ...tabs, openTabs } } } : state;
  });
}

let labelSource: () => Record<string, string> = () => ({});
/** The store registers how to label folders, so documents.ts need not import it. */
export function setFolderLabelSource(source: () => Record<string, string>) {
  labelSource = source;
}
function folderLabelsFor(): Record<string, string> {
  return labelSource();
}
export { folderLabels };
