/**
 * Moving a pane's host element between parents without losing where it was
 * scrolled to.
 *
 * Re-parenting is a removal and an insertion, and a removed element loses its
 * scroll boxes: the transcript a person had scrolled up in came back at the
 * top. `moveBefore` (Chromium 133+) moves an element with its state intact;
 * where it is missing (WebKit) the offsets are read before and written back
 * after — the only state a pane body keeps in the DOM that its store does not.
 */

type Scrolled = { el: Element; top: number; left: number };

type ScrollBox = Pick<Element, "scrollTop" | "scrollLeft">;

/** Every element under (and including) `root` that is scrolled away from 0,0. */
export function scrollOffsets(root: Element): Scrolled[] {
  const out: Scrolled[] = [];
  const visit = (el: Element) => {
    const box = el as ScrollBox;
    if (box.scrollTop !== 0 || box.scrollLeft !== 0) out.push({ el, top: box.scrollTop, left: box.scrollLeft });
  };
  visit(root);
  for (const el of root.querySelectorAll("*")) visit(el);
  return out;
}

export function restoreScroll(saved: readonly Scrolled[]): void {
  for (const { el, top, left } of saved) {
    const box = el as ScrollBox;
    box.scrollTop = top;
    box.scrollLeft = left;
  }
}

type MovableParent = Element & { moveBefore?: (node: Node, child: Node | null) => void };

/** Makes `host` the last child of `parent`, keeping its scroll positions. */
export function moveHost(parent: Element, host: Element): void {
  if (host.parentNode === parent) return;
  const target = parent as MovableParent;
  if (typeof target.moveBefore === "function" && host.isConnected && parent.isConnected) {
    try {
      target.moveBefore(host, null);
      return;
    } catch {
      // Different documents or a node moveBefore refuses: fall through.
    }
  }
  const saved = host.isConnected ? scrollOffsets(host) : [];
  parent.appendChild(host);
  restoreScroll(saved);
}
