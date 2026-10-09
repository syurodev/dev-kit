import { getModule, viewIcon } from "@/modules/registry";
import { moduleTabs, useWorkbenchTabsStore, type WorkbenchTabsState } from "@/stores/workbenchTabs";
import type { RequestInput } from "@/bridge/apiclient";
import { useApiClientStore } from "./apiclientStore";
import { emptyRequest } from "./requestModel";

export const MODULE = "apiclient";
export const REQUEST_VIEW = "apiclient.request";
export const VARIABLES_VIEW = "apiclient.variables";

function preload(viewId: string) {
  void getModule(MODULE)?.views.find((v) => v.id === viewId)?.preload?.().catch(() => undefined);
}

/**
 * Opens the request view on a draft of one Code Graph endpoint.
 *
 * The endpoint id travels, not the draft: a tab that reopens after a restart
 * re-asks for a draft of the current index rather than restoring one built
 * against an index that has since changed.
 */
export function openEndpointDraft(
  endpointId: string,
  title: string,
  options: { beside?: boolean; /** The folder whose index lists the endpoint. */ project?: string } = {},
): string {
  preload(REQUEST_VIEW);
  return useWorkbenchTabsStore.getState().openTab({
    moduleId: MODULE,
    viewId: REQUEST_VIEW,
    title,
    icon: viewIcon(MODULE, REQUEST_VIEW),
    params: options.project ? { endpointId, project: options.project } : { endpointId },
    preview: false,
    beside: options.beside,
  });
}

/**
 * Opens the request view on one saved request — the sidebar, `api.reveal`
 * (docs/dev-kit/cross-module.mdx), a ToolResultView card's "Open" for
 * `api.send`. The id travels, not a snapshot of the request: reopening after a
 * restart asks for whatever that id currently holds.
 *
 * Single click previews and double click keeps, like every other rail.
 */
export function openRequest(
  requestId: string,
  title: string,
  options: { beside?: boolean; preview?: boolean } = {},
): string {
  preload(REQUEST_VIEW);
  return useWorkbenchTabsStore.getState().openTab({
    moduleId: MODULE,
    viewId: REQUEST_VIEW,
    title,
    icon: viewIcon(MODULE, REQUEST_VIEW),
    params: { requestId },
    preview: options.preview ?? false,
    beside: options.beside,
  });
}

let newCounter = 0;

/**
 * Opens an editor on a request that is not saved yet — blank, a pasted curl,
 * a duplicate. It is kept, not previewed: it is about to be typed into.
 *
 * Its contents wait in the store under the tab's own key; a tab restored
 * after a restart finds nothing there and opens blank, which is the honest
 * answer for something that was never saved.
 */
export function openNewRequest(initial?: Partial<RequestInput>, options: { beside?: boolean } = {}): string {
  preload(REQUEST_VIEW);
  newCounter += 1;
  const newId = `${Date.now().toString(36)}-${newCounter}`;
  const request = { ...emptyRequest(initial?.collectionId ?? ""), ...initial, id: "" };
  useApiClientStore.getState().setPending(newId, request);
  return useWorkbenchTabsStore.getState().openTab({
    moduleId: MODULE,
    viewId: REQUEST_VIEW,
    title: request.name || "New request",
    icon: viewIcon(MODULE, REQUEST_VIEW),
    params: { newId },
    preview: false,
    beside: options.beside,
  });
}

export type VariablesScope = "globals" | "environment" | "collection";

/**
 * Opens the variables editor for one scope. `id` is the environment or
 * collection; "new" for an environment not created yet.
 */
export function openVariables(scope: VariablesScope, id: string, title: string, options: { beside?: boolean } = {}): string {
  preload(VARIABLES_VIEW);
  return useWorkbenchTabsStore.getState().openTab({
    moduleId: MODULE,
    viewId: VARIABLES_VIEW,
    title,
    icon: viewIcon(MODULE, VARIABLES_VIEW),
    params: scope === "globals" ? { scope } : { scope, id },
    preview: false,
    beside: options.beside,
  });
}

/**
 * Closes a tab — the editor of a request just deleted, a new one just saved.
 * Empty params mean the view's home tab, the one opened with none.
 */
export function closeTabByParams(viewId: string, params: Record<string, unknown>) {
  const tabs = useWorkbenchTabsStore.getState();
  const entries = Object.entries(params);
  const tab = (tabs.modules[MODULE]?.openTabs ?? []).find(
    (candidate) =>
      candidate.viewId === viewId &&
      (entries.length === 0
        ? Object.keys(candidate.params ?? {}).length === 0
        : entries.every(([key, value]) => candidate.params?.[key] === value)),
  );
  if (tab) tabs.closeTab(tab.id);
}

/** Rail selection and shortcuts follow the focused document group. */
export function activeApiClientTab(state: WorkbenchTabsState) {
  const tabs = moduleTabs(state.modules, MODULE);
  const group = state.groups.find((g) => g.id === state.focusedGroupId && g.moduleId === MODULE)
    ?? state.groups.find((g) => g.moduleId === MODULE);
  return tabs.openTabs.find((t) => t.id === (group?.activeTabId ?? tabs.activeTabId));
}

export function renameApiDocument(viewId: string, params: Record<string, unknown>, title: string) {
  useWorkbenchTabsStore.setState((state) => {
    const tabs = moduleTabs(state.modules, MODULE);
    const matches = (tab: (typeof tabs.openTabs)[number]) => tab.viewId === viewId && Object.entries(params).every(([key, value]) => tab.params?.[key] === value);
    if (!tabs.openTabs.some((tab) => matches(tab) && tab.title !== title)) return state;
    return { modules: { ...state.modules, [MODULE]: { ...tabs, openTabs: tabs.openTabs.map((tab) => matches(tab) ? { ...tab, title } : tab) } } };
  });
}
