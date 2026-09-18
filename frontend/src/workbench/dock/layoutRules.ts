/**
 * SPIKE — the rules of docs/dev-kit/layout.mdx as pure functions, so they can
 * be tested without dockview and kept if the renderer changes.
 */

/** A module id from the registry (`codegraph`, `notes`, …). */
export type ModuleId = string;
export type PaneKind = "document" | "context" | "companion";

export type PaneParams = {
  kind: PaneKind;
  /** Documents and context panes belong to a module; companions do not. */
  module?: ModuleId;
  title: string;
};

export type DropLocation = "tab" | "header_space" | "content" | "edge";
export type DropPosition = "top" | "bottom" | "left" | "right" | "center";

export type DropTarget = {
  /** Undefined for the whole-layout edges. */
  groupKind?: PaneKind;
  groupModule?: ModuleId;
  location: DropLocation;
  position: DropPosition;
};

/** Dropping here would put the dragged item *inside* the target group. */
function joinsGroup(t: DropTarget): boolean {
  return t.location === "tab" || t.location === "header_space" || (t.location === "content" && t.position === "center");
}

/**
 * layout.mdx § Kéo-thả có cấu trúc and § Document group:
 * - a pane never becomes a tab of a document group (it may split beside it);
 * - a document never enters a pane group, and only joins a document group of
 *   its own module — beside another module's group it opens a new group;
 * - the whole-layout edges take anything.
 */
export function canDrop(dragged: PaneParams, target: DropTarget): boolean {
  if (target.location === "edge" || target.groupKind === undefined) return true;
  if (dragged.kind === "document") {
    if (target.groupKind !== "document") return false;
    if (joinsGroup(target)) return target.groupModule === dragged.module;
    return true;
  }
  if (target.groupKind === "document") return !joinsGroup(target);
  return true;
}

/** A group's kind is the kind of its panels — the drop rules keep them uniform. */
export function groupKindOf(panels: PaneParams[]): { kind?: PaneKind; module?: ModuleId } {
  const first = panels[0];
  return first ? { kind: first.kind, module: first.module } : {};
}

export type Rect = { left: number; top: number; right: number; bottom: number };
export type Direction = "left" | "right" | "above" | "below";

/**
 * Where a context pane sits relative to a reference rect, for remembering it
 * per module and putting it back after a module switch. Picks the side with
 * the clearest separation; overlapping rects (same group) are "within".
 */
export function relativeDirection(pane: Rect, ref: Rect): Direction | "within" {
  const gaps: [Direction, number][] = [
    ["right", pane.left - ref.right],
    ["left", ref.left - pane.right],
    ["below", pane.top - ref.bottom],
    ["above", ref.top - pane.bottom],
  ];
  const best = gaps.reduce((a, b) => (b[1] > a[1] ? b : a));
  return best[1] >= -2 ? best[0] : "within";
}

export type Placement = {
  panelId: string;
  /** "document" = the module's document group; otherwise an earlier context pane. */
  reference: "document" | string;
  direction: Direction | "within";
  size?: number;
};
