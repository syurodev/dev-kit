import type { CodeGraphDataFlowStep } from "@/bridge/codegraph";

/**
 * Human wording for each wall the trace can hit. The strings say what was lost
 * and why, because a trace that just stops is indistinguishable from a trace
 * that finished — see codegraph.mdx § "Ba bức tường".
 */
export const WALL_LABELS = {
  library: "library — bytecode, no body to read",
  interface: "interface — implementation not knowable statically",
  unresolved: "unresolved — the solver could not resolve this call",
  depth: "depth limit reached",
  cycle: "cycle — already on this path",
  lombok: "lombok — generated accessor, read as the field",
  repeat: "repeat — this method was already traced above",
  limit: "step limit reached — the trace was cut short",
} as const;

export type DataFlowRow =
  | { type: "header"; key: string; nodeId: string; label: string; filePath: string; depth: number }
  | {
      type: "event";
      key: string;
      nodeId: string;
      line: number;
      kind: "read" | "write" | "call";
      depth: number;
      /** Non-empty only for a resolved call — the node to jump to. */
      targetId: string;
      /** Non-empty when this step hit a wall; already human-readable. */
      wall: string;
    };

/**
 * Flattens a trace into rows, inserting a header whenever the method changes.
 *
 * The header re-opens on revisit rather than merging: the same method reached
 * again after a deeper call is a different point in the walk, and collapsing
 * the two would imply an ordering the trace does not claim.
 */
export function toDataFlowRows(steps: CodeGraphDataFlowStep[]): DataFlowRow[] {
  const rows: DataFlowRow[] = [];
  let openMethod = "";
  let openDepth = -1;

  steps.forEach((s, i) => {
    if (s.nodeId !== openMethod || s.depth !== openDepth) {
      rows.push({
        type: "header",
        key: `h${i}`,
        nodeId: s.nodeId,
        label: s.label,
        filePath: s.filePath,
        depth: s.depth,
      });
      openMethod = s.nodeId;
      openDepth = s.depth;
    }
    rows.push({
      type: "event",
      key: `e${i}`,
      nodeId: s.nodeId,
      line: s.line,
      kind: s.kind,
      depth: s.depth,
      targetId: s.unresolved === "" ? s.targetId : "",
      wall: s.unresolved === "" ? "" : (WALL_LABELS[s.unresolved] ?? s.unresolved),
    });
  });

  return rows;
}
