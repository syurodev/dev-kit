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

/**
 * Where each view inside a host was scrolled to when it last left the screen.
 *
 * A host keeps its view mounted, but not its scroll: a browser drops an
 * element's scroll offset when its box goes away, and both things that happen
 * to a host do that — parking puts it under display: none, and adopting it
 * re-inserts the node. Without this, coming back to a tab found the editor at
 * line 1, which go-to-definition (jump away, come back) made hard to miss.
 *
 * Kept per host rather than by the views themselves so every module gets it:
 * an editor, a result grid and a plain scrolling list all lose their place
 * the same way. Keyed weakly, so a closed tab's host takes its entry with it;
 * the entry itself lives only until the host is next adopted.
 */
type ScrollOffset = { el: Element; top: number; left: number };
const scrollMemory = new WeakMap<HTMLElement, ScrollOffset[]>();

function isOnScreen(host: HTMLElement): boolean {
  return host.isConnected && host.parentNode !== parking;
}

/** Records the scroll offsets under `host`. Only meaningful while it is on screen. */
export function rememberScroll(host: HTMLElement) {
  // A parked host reads zero everywhere — that is the loss, not the state —
  // so recording then would overwrite the real offsets with nothing.
  if (!isOnScreen(host)) return;
  const offsets: ScrollOffset[] = [];
  for (const el of host.querySelectorAll("*")) {
    if (el.scrollTop !== 0 || el.scrollLeft !== 0) {
      offsets.push({ el, top: el.scrollTop, left: el.scrollLeft });
    }
  }
  scrollMemory.set(host, offsets);
}

/** Puts back what rememberScroll recorded, for the elements that are still there. */
export function restoreScroll(host: HTMLElement) {
  const offsets = scrollMemory.get(host);
  if (!offsets) return;
  scrollMemory.delete(host);
  for (const { el, top, left } of offsets) {
    // Unmounted by its view since: nothing to put back.
    if (!host.contains(el)) continue;
    el.scrollTop = top;
    el.scrollLeft = left;
  }
}

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
  rememberScroll(host);
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
    // Straight from another group's card, with no stop in the parking lot:
    // the move itself resets the offsets, so they are taken here instead.
    rememberScroll(host);
    el.appendChild(host);
    // In this layout effect, so the first paint is already at the old place.
    restoreScroll(host);
    return () => {
      // Only if it is still ours: another group may have adopted it already.
      if (host.parentNode === el) park(host);
    };
  }, [slot, tabId]);
}
