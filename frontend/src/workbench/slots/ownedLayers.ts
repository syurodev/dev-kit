/**
 * Closing the floating layers a hidden pane left open.
 *
 * Radix portals menus, popovers and selects to the body, so a pane that is
 * hidden rather than unmounted (the Agent companion) leaves them on screen —
 * and a modal menu leaves the whole app's pointer events off. They are closed
 * the way the person would close them, with Escape, which Radix delivers to
 * the topmost layer only; so Escape is pressed only while the topmost layer
 * is one the pane owns, one per frame, a few times at most.
 *
 * Ownership: a trigger inside the pane names its open content in
 * `aria-controls` (Popover, DropdownMenu, Select), and a submenu's trigger
 * sits inside a content already owned. A hover card has no such link; it
 * closes on its own once the pointer is off a trigger that is no longer there.
 */

const OPEN_TRIGGERS = "[aria-expanded='true'][aria-controls]";
const POPPER = "[data-radix-popper-content-wrapper]";

type Scope = Pick<ParentNode, "querySelectorAll">;

function controlled(scope: Scope, into: Set<string>): void {
  for (const trigger of scope.querySelectorAll(OPEN_TRIGGERS)) {
    const id = trigger.getAttribute("aria-controls");
    if (id) into.add(id);
  }
}

/** Whether the topmost open popper layer was opened from inside `host`. */
export function topLayerOwnedBy(host: Scope, doc: Scope): boolean {
  const owned = new Set<string>();
  controlled(host, owned);
  let top: Element | null = null;
  // In open order: a content owned so far owns what is opened from inside it.
  for (const wrapper of doc.querySelectorAll(POPPER)) {
    const content = wrapper.firstElementChild;
    if (!content) continue;
    top = content;
    if (owned.has(content.id)) controlled(content, owned);
  }
  return top !== null && owned.has(top.id);
}

const TRIES = 4;

export function dismissOwnedLayers(
  host: Scope,
  doc: Scope & Partial<Pick<Document, "dispatchEvent">>,
  io: { escape?: () => void; frame?: (fn: () => void) => void } = {},
  tries = TRIES,
): void {
  if (tries <= 0 || !topLayerOwnedBy(host, doc)) return;
  const escape =
    io.escape ?? (() => doc.dispatchEvent?.(new KeyboardEvent("keydown", { key: "Escape", bubbles: true, cancelable: true })));
  const frame = io.frame ?? ((fn: () => void) => void requestAnimationFrame(fn));
  escape();
  // The layer leaves the DOM when React commits its close: look again next frame.
  frame(() => dismissOwnedLayers(host, doc, io, tries - 1));
}
