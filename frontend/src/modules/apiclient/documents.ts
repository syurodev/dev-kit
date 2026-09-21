import { getModule, viewIcon } from "@/modules/registry";
import { useWorkbenchTabsStore } from "@/stores/workbenchTabs";

export const MODULE = "apiclient";
export const REQUEST_VIEW = "apiclient.request";

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
  options: { beside?: boolean } = {},
): string {
  void getModule(MODULE)?.views.find((v) => v.id === REQUEST_VIEW)?.preload?.().catch(() => undefined);
  return useWorkbenchTabsStore.getState().openTab({
    moduleId: MODULE,
    viewId: REQUEST_VIEW,
    title,
    icon: viewIcon(MODULE, REQUEST_VIEW),
    params: { endpointId },
    preview: false,
    beside: options.beside,
  });
}

/**
 * Opens the request view on one saved request — `api.reveal`
 * (docs/dev-kit/cross-module.mdx), a ToolResultView card's "Open" for
 * `api.send`. The id travels, not a snapshot of the request: reopening
 * after a restart asks for whatever that id currently holds.
 */
export function openRequest(requestId: string, title: string, options: { beside?: boolean } = {}): string {
  void getModule(MODULE)?.views.find((v) => v.id === REQUEST_VIEW)?.preload?.().catch(() => undefined);
  return useWorkbenchTabsStore.getState().openTab({
    moduleId: MODULE,
    viewId: REQUEST_VIEW,
    title,
    icon: viewIcon(MODULE, REQUEST_VIEW),
    params: { requestId },
    preview: false,
    beside: options.beside,
  });
}
