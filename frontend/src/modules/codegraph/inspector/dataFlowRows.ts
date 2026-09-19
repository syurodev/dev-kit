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

export type DataFlowEvent = {
  key: string;
  line: number;
  kind: "read" | "write" | "call";
  /** Non-empty only for a resolved call — the node to jump to. */
  targetId: string;
  /** The callee's name, for a call: what the pill says. */
  targetLabel: string;
  /** Non-empty when this step hit a wall; already human-readable. */
  wall: string;
};

/** One method's stretch of the walk: where it is, and what it did there. */
export type DataFlowGroup = {
  key: string;
  nodeId: string;
  label: string;
  filePath: string;
  /** The first line it touched — where "open file" lands. */
  line: number;
  depth: number;
  events: DataFlowEvent[];
};

/**
 * Folds a trace into one group per method visit, each with its events.
 *
 * A group re-opens on revisit rather than merging: the same method reached
 * again after a deeper call is a different point in the walk, and collapsing
 * the two would imply an ordering the trace does not claim.
 */
export function toDataFlowGroups(steps: CodeGraphDataFlowStep[]): DataFlowGroup[] {
  // A callee's name comes from its own steps further down the walk.
  const names = new Map(steps.map((s) => [s.nodeId, s.label]));
  const groups: DataFlowGroup[] = [];
  let open: DataFlowGroup | null = null;

  steps.forEach((s, i) => {
    if (!open || open.nodeId !== s.nodeId || open.depth !== s.depth) {
      open = { key: `g${i}`, nodeId: s.nodeId, label: s.label, filePath: s.filePath, line: s.line, depth: s.depth, events: [] };
      groups.push(open);
    }
    const resolved = s.unresolved === "";
    open.events.push({
      key: `e${i}`,
      line: s.line,
      kind: s.kind,
      targetId: resolved ? s.targetId : "",
      targetLabel: s.kind === "call" ? (names.get(s.targetId) ?? s.targetId) : "",
      wall: resolved ? "" : (WALL_LABELS[s.unresolved] ?? s.unresolved),
    });
  });

  return groups;
}

export type DataFlowFilter = "all" | "read" | "write";

/** How many methods read the field, how many write it, and walls hit. */
export function summarizeFlow(groups: DataFlowGroup[]) {
  const methodsWith = (kind: "read" | "write") =>
    new Set(groups.filter((g) => g.events.some((e) => e.kind === kind)).map((g) => g.nodeId)).size;
  return {
    readers: methodsWith("read"),
    writers: methodsWith("write"),
    walls: groups.reduce((n, g) => n + g.events.filter((e) => e.wall).length, 0),
  };
}

/**
 * The groups a filter keeps. "write" answers the question a field trace is
 * usually opened for — who changes this — so it drops the reads and the
 * calls in between, and every method that does not write.
 */
export function filterFlow(groups: DataFlowGroup[], filter: DataFlowFilter): DataFlowGroup[] {
  if (filter === "all") return groups;
  return groups.flatMap((g) => {
    const events = g.events.filter((e) => e.kind === filter);
    return events.length > 0 ? [{ ...g, events }] : [];
  });
}
