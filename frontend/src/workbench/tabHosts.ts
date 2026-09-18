import { useLayoutEffect, type RefObject } from "react";

/**
 * One detached element per open tab, which the tab's view is portalled into.
 *
 * The view renders once, into its host, and whichever document group shows
 * the tab adopts the host into its card. Moving a tab to another group, or a
 * group closing, moves an element — the editor, the grid, the canvas inside
 * never remount. A host no group is showing waits in the parking lot, a
 * display: none box, which is what `hidden` did for inactive tabs before.
 */
const hosts = new Map<string, HTMLDivElement>();
let parking: HTMLElement | null = null;

export function tabHost(id: string): HTMLDivElement {
  let host = hosts.get(id);
  if (!host) {
    host = document.createElement("div");
    // `contents`: the view's own box takes part in the group's flex column,
    // as it did under the old `contents` wrapper.
    host.className = "contents";
    host.dataset.tabHost = id;
    hosts.set(id, host);
  }
  return host;
}

export function setParking(el: HTMLElement | null) {
  parking = el;
  if (!el) return;
  for (const host of hosts.values()) if (!host.parentNode) el.appendChild(host);
}

function park(host: HTMLElement) {
  if (parking) parking.appendChild(host);
  else host.remove();
}

/** Parks every host not on screen, and forgets the hosts of closed tabs. */
export function syncTabHosts(openIds: ReadonlySet<string>) {
  for (const [id, host] of hosts) {
    if (!openIds.has(id)) {
      host.remove();
      hosts.delete(id);
    } else if (!host.parentNode) {
      park(host);
    }
  }
}

/** Shows `tabId`'s host inside `slot` for as long as this group shows it. */
export function useAdoptTab(slot: RefObject<HTMLElement | null>, tabId: string) {
  useLayoutEffect(() => {
    const el = slot.current;
    if (!el || !tabId) return;
    const host = tabHost(tabId);
    el.appendChild(host);
    return () => {
      // Only if it is still ours: another group may have adopted it already.
      if (host.parentNode === el) park(host);
    };
  }, [slot, tabId]);
}
