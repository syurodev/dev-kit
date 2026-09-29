import { getModule, viewIcon } from "@/modules/registry";
import { moduleTabs, useWorkbenchTabsStore, type WorkbenchTab } from "@/stores/workbenchTabs";
import type { CodeGraphEntryPoint } from "@/bridge/codegraph";

/**
 * Code Graph's documents, each a tab.
 *
 * A flow, a file and the plan used to be one canvas the sidebar switched
 * between — so a flow and the file it passes through could never be on screen
 * together, and picking a file threw the graph away. As tabs they go wherever
 * tabs go: side by side in document groups (⌘\, ⌘-click, Open beside), kept
 * while you look at something else, and restored after a relaunch.
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

type OpenOptions = { preview?: boolean; beside?: boolean };

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

export function openEntryPointFlow(ep: CodeGraphEntryPoint, options: OpenOptions = { preview: true }): string {
  return open(FLOW_VIEW, `${ep.verb} ${ep.target}`, { root: ep.entryNode, entryPoint: ep.id }, options);
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
export function openNodeFlow(nodeId: string, title: string, options: OpenOptions = { preview: true }): string {
  return open(FLOW_VIEW, title, { root: nodeId }, options);
}

export function openFileDoc(path: string, options: OpenOptions = { preview: true }): string {
  return open(FILE_VIEW, path.slice(path.lastIndexOf("/") + 1), { path }, options);
}

export function openPlanDoc(options: OpenOptions = {}): string {
  return open(PLAN_VIEW, "Plan", undefined, options);
}

/** Keeps a preview tab — an edit, or a second look, says it is wanted. */
export function keepTab(id: string) {
  const tab = moduleTabs(useWorkbenchTabsStore.getState().modules, MODULE).openTabs.find((t) => t.id === id);
  if (tab?.preview) useWorkbenchTabsStore.getState().pinTab(id);
}

/** What a Code Graph tab shows, read off its view and params. */
export type ActiveDoc = { kind: "home" | "flow" | "file" | "plan"; root?: string; entryPoint?: string; path?: string };

export function describeDocument(tab: WorkbenchTab | null): ActiveDoc {
  const p = tab?.params ?? {};
  switch (tab?.viewId) {
    case FLOW_VIEW:
      return {
        kind: "flow",
        root: typeof p.root === "string" ? p.root : undefined,
        entryPoint: entryPointParam(p),
      };
    case FILE_VIEW:
      return { kind: "file", path: typeof p.path === "string" ? p.path : undefined };
    case PLAN_VIEW:
      return { kind: "plan" };
    default:
      return { kind: "home" };
  }
}

/**
 * Closes the documents a project switch makes meaningless: every flow, and
 * every file without unsaved edits. A file with edits stays, marked as from
 * the old project, so they can be copied out (codegraphActions switchProject).
 */
export function closeProjectDocuments(keepFile: (path: string) => boolean) {
  const store = useWorkbenchTabsStore.getState();
  for (const tab of moduleTabs(store.modules, MODULE).openTabs) {
    const doc = describeDocument(tab);
    if (doc.kind === "flow" || (doc.kind === "file" && !(doc.path && keepFile(doc.path)))) {
      store.closeTab(tab.id);
    }
  }
}

