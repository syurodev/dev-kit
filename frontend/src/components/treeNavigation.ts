import type { FlatRow, TreeNodeData } from "./tree-view";

/** A reveal follows actual tree nodes, including compacted directory chains. */
export function treePath(nodes: TreeNodeData[], id: string): TreeNodeData[] | null {
  for (const node of nodes) {
    if (node.id === id) return [node];
    const child = node.children && treePath(node.children, id);
    if (child) return [node, ...child];
  }
  return null;
}

export function treeKeyboardTarget(rows: FlatRow[], current: number, key: string): number | null {
  const selectable = (index: number) => index >= 0 && index < rows.length && !rows[index].placeholder;
  if (key === "Home") return rows.findIndex((row) => !row.placeholder);
  if (key === "End") {
    for (let i = rows.length - 1; i >= 0; i--) if (selectable(i)) return i;
    return null;
  }
  if (!selectable(current)) return rows.findIndex((row) => !row.placeholder);
  const row = rows[current];
  if (key === "ArrowDown" || key === "ArrowUp") {
    const step = key === "ArrowDown" ? 1 : -1;
    for (let i = current + step; i >= 0 && i < rows.length; i += step) if (selectable(i)) return i;
    return current;
  }
  if (key === "ArrowRight" && row.open && selectable(current + 1) && rows[current + 1].level > row.level) return current + 1;
  if (key === "ArrowLeft" && !row.open) {
    for (let i = current - 1; i >= 0; i--) if (selectable(i) && rows[i].level < row.level) return i;
  }
  return null;
}
