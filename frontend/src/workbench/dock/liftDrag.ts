import type { DockviewApi, DockviewGroupPanel, IDockviewPanel } from "dockview-react";
import { canDrop, groupKindOf, type DropPosition, type PaneParams } from "./layoutRules";

/**
 * SPIKE — Claude Desktop's drag: the pane itself lifts off the grid and
 * follows the pointer, live, instead of a snapshot chip. Pointer events only,
 * so it does not depend on the webview's HTML5 drag-and-drop at all.
 *
 * 1. Past a 5px threshold the pane becomes a floating group at the size and
 *    place it had (its content keeps running: renderer "always", no re-mount).
 * 2. The floating container follows the pointer; the pane under the pointer is
 *    hit-tested and a drop zone is drawn if canDrop allows it.
 * 3. Release on a zone docks there. Release elsewhere, or Esc, restores the
 *    layout as it was before the drag.
 */

type Hint = { left: number; top: number; width: number; height: number; label: string };
type Target =
  | { kind: "group"; group: DockviewGroupPanel; position: DropPosition }
  | { kind: "edge"; position: "left" | "right" | "top" | "bottom" };

const THRESHOLD = 5;
const EDGE = 18;
const HEADER = 40;

const LABEL: Record<DropPosition, string> = {
  left: "Split left",
  right: "Split right",
  top: "Split above",
  bottom: "Split below",
  center: "Join as tab",
};

let hintEl: HTMLDivElement | null = null;
let hintOrigin = { x: 0, y: 0 };
export function setDropHintElement(el: HTMLDivElement | null) {
  hintEl = el;
}

function showHint(h: Hint | null) {
  if (!hintEl) return;
  if (!h) {
    hintEl.style.opacity = "0";
    return;
  }
  hintEl.style.opacity = "1";
  // Fixed to the viewport (the hint lives in document.body, above every
  // floating group but under the lifted pane): add the dock's own offset.
  hintEl.style.transform = `translate(${h.left + hintOrigin.x}px, ${h.top + hintOrigin.y}px)`;
  hintEl.style.width = `${h.width}px`;
  hintEl.style.height = `${h.height}px`;
  hintEl.dataset.label = h.label;
}

type FloatingGroup = { group: DockviewGroupPanel; position(bounds: { left: number; top: number }): void };

/**
 * dockview's floating window for a group. Not on the public API: DockviewApi
 * keeps its component private. Moving the container through its own
 * `position()` matters — panels rendered "always" (terminal, canvas, agent)
 * live in a separate overlay that dockview re-aligns only on its own layout
 * events; moving the element by hand leaves their content behind.
 */
function floatingOf(api: DockviewApi, group: DockviewGroupPanel): FloatingGroup | undefined {
  const component = (api as unknown as { component?: { floatingGroups?: readonly FloatingGroup[] } }).component;
  return component?.floatingGroups?.find((f) => f.group === group);
}

function rootOf(panel: IDockviewPanel): HTMLElement {
  return (panel.group.element.closest(".dockview-theme-devkit") as HTMLElement | null) ?? document.body;
}

function groupAt(api: DockviewApi, x: number, y: number, lifted: HTMLElement): DockviewGroupPanel | undefined {
  for (const el of document.elementsFromPoint(x, y)) {
    if (lifted.contains(el)) continue;
    const gv = el.closest(".dv-groupview");
    if (!gv || lifted.contains(gv)) continue;
    return api.groups.find((g) => g.element === gv);
  }
  return undefined;
}

function resolveTarget(api: DockviewApi, dragged: PaneParams, x: number, y: number, root: DOMRect, lifted: HTMLElement): { target: Target; hint: Hint } | null {
  // The whole-layout edges first: a thin band along the dock's border.
  const edges: [("left" | "right" | "top" | "bottom"), boolean][] = [
    ["left", x - root.left < EDGE],
    ["right", root.right - x < EDGE],
    ["top", y - root.top < EDGE],
    ["bottom", root.bottom - y < EDGE],
  ];
  const edge = edges.find(([, hit]) => hit)?.[0];
  if (edge) {
    if (!canDrop(dragged, { location: "edge", position: edge })) return null;
    const w = root.width * 0.3;
    const h = root.height * 0.3;
    const box = {
      left: edge === "right" ? root.width - w : 0,
      top: edge === "bottom" ? root.height - h : 0,
      width: edge === "left" || edge === "right" ? w : root.width,
      height: edge === "top" || edge === "bottom" ? h : root.height,
    };
    return { target: { kind: "edge", position: edge }, hint: { ...box, label: edge === "left" || edge === "right" ? "New column" : "New row" } };
  }

  const group = groupAt(api, x, y, lifted);
  if (!group) return null;
  const r = group.element.getBoundingClientRect();
  const k = groupKindOf(group.panels.map((p) => p.params as PaneParams));
  const inHeader = y - r.top < HEADER;
  const fx = (x - r.left) / r.width;
  const fy = (y - r.top) / r.height;
  const position: DropPosition = inHeader ? "center" : fx < 0.25 ? "left" : fx > 0.75 ? "right" : fy < 0.25 ? "top" : fy > 0.75 ? "bottom" : "center";
  const location = inHeader ? "tab" : "content";
  if (!canDrop(dragged, { groupKind: k.kind, groupModule: k.module, location, position })) return null;

  const left = r.left - root.left;
  const top = r.top - root.top;
  const half = { left: { left, top, width: r.width / 2, height: r.height }, right: { left: left + r.width / 2, top, width: r.width / 2, height: r.height }, top: { left, top, width: r.width, height: r.height / 2 }, bottom: { left, top: top + r.height / 2, width: r.width, height: r.height / 2 }, center: { left, top, width: r.width, height: r.height } }[position];
  return { target: { kind: "group", group, position }, hint: { ...half, label: LABEL[position] } };
}

export function startLiftDrag(api: DockviewApi, panel: IDockviewPanel, down: PointerEvent) {
  if (down.button !== 0) return;
  const dragged = panel.params as PaneParams;
  const start = { x: down.clientX, y: down.clientY };
  const snapshot = api.toJSON();
  const originRect = panel.group.element.getBoundingClientRect();
  const offset = { x: down.clientX - originRect.left, y: down.clientY - originRect.top };
  let lifted: HTMLElement | null = null;
  let root: DOMRect | null = null;
  let target: Target | null = null;

  const lift = () => {
    const rootEl = rootOf(panel);
    root = rootEl.getBoundingClientRect();
    hintOrigin = { x: root.left, y: root.top };
    const wasFloating = panel.group.api.location.type === "floating";
    if (!wasFloating) {
      api.addFloatingGroup(panel, {
        x: originRect.left - root.left,
        y: originRect.top - root.top,
        width: originRect.width,
        height: originRect.height,
      });
    }
    lifted = panel.group.element.closest(".dv-resize-container") as HTMLElement | null;
    lifted?.classList.add("dk-lifted");
    document.body.classList.add("dk-lift-dragging");
    window.getSelection()?.removeAllRanges();
  };

  const move = (ev: PointerEvent) => {
    if (!lifted) {
      if (Math.hypot(ev.clientX - start.x, ev.clientY - start.y) < THRESHOLD) return;
      lift();
      if (!lifted) return cleanup();
    }
    ev.preventDefault(); // no text selection trailing the drag (Safari)
    const r = root!;
    const left = ev.clientX - offset.x - r.left;
    const top = ev.clientY - offset.y - r.top;
    const fg = floatingOf(api, panel.group);
    if (fg) fg.position({ left, top });
    else {
      lifted.style.left = `${left}px`;
      lifted.style.top = `${top}px`;
    }
    const resolved = resolveTarget(api, dragged, ev.clientX, ev.clientY, r, lifted);
    target = resolved?.target ?? null;
    showHint(resolved?.hint ?? null);
  };

  const finish = (commit: boolean) => {
    const t = commit ? target : null;
    cleanup();
    if (!lifted) return; // a click, not a drag
    if (t?.kind === "group") panel.api.moveTo({ group: t.group, position: t.position });
    else if (t?.kind === "edge") {
      const g = api.addGroup({ direction: t.position === "top" ? "above" : t.position === "bottom" ? "below" : t.position });
      panel.api.moveTo({ group: g, position: "center" });
    } else {
      // Not a drop zone: put everything back exactly as it was.
      api.fromJSON(snapshot, { reuseExistingPanels: true });
    }
  };

  const up = () => finish(true);
  const key = (ev: KeyboardEvent) => {
    if (ev.key === "Escape") finish(false);
  };
  function cleanup() {
    window.removeEventListener("pointermove", move);
    window.removeEventListener("pointerup", up);
    window.removeEventListener("keydown", key, true);
    lifted?.classList.remove("dk-lifted");
    document.body.classList.remove("dk-lift-dragging");
    showHint(null);
  }

  window.addEventListener("pointermove", move);
  window.addEventListener("pointerup", up);
  window.addEventListener("keydown", key, true);
}
