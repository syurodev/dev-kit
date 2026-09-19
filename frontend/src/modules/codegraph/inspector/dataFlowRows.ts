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

/**
 * Walls that mean a use may be missing from the answer. A cycle or a repeat
 * is a method already on the list, so nothing was lost there.
 */
const LOSSY = new Set(["library", "interface", "unresolved", "depth", "limit"]);

/** One line of a method that touches the field. */
export type FieldAccess = { line: number; code: string };

/** One method that reads (or writes) the field, and where. */
export type FieldUser = {
  nodeId: string;
  label: string;
  filePath: string;
  /** The method whose call led the trace here, when it was not a start. */
  via: string;
  accesses: FieldAccess[];
};

/** A call the trace could not follow: uses past it may be missing. */
export type FieldGap = { label: string; line: number; reason: string };

export type FieldUsage = {
  writers: FieldUser[];
  readers: FieldUser[];
  gaps: FieldGap[];
};

/**
 * A trace, turned into the answer it is for: which methods change the
 * field, which read it, and where the trace could not see.
 *
 * The trace itself is a walk — methods in call-graph order, calls between
 * them — which is right for a machine and hard to read. Grouped by role, the
 * list says what it means; the walk survives as each method's "via".
 */
export function toFieldUsage(steps: CodeGraphDataFlowStep[]): FieldUsage {
  const writers = new Map<string, FieldUser>();
  const readers = new Map<string, FieldUser>();
  const gaps: FieldGap[] = [];
  // The method whose call is open at each depth — who led the walk here.
  const callerAt: string[] = [];

  for (const s of steps) {
    callerAt[s.depth] = s.label;
    if (s.kind === "call") {
      if (s.unresolved && LOSSY.has(s.unresolved)) {
        gaps.push({ label: s.label, line: s.line, reason: WALL_LABELS[s.unresolved as keyof typeof WALL_LABELS] ?? s.unresolved });
      }
      continue;
    }
    const into = s.kind === "write" ? writers : readers;
    let user = into.get(s.nodeId);
    if (!user) {
      user = { nodeId: s.nodeId, label: s.label, filePath: s.filePath, via: s.depth > 0 ? (callerAt[s.depth - 1] ?? "") : "", accesses: [] };
      into.set(s.nodeId, user);
    }
    if (!user.accesses.some((a) => a.line === s.line)) user.accesses.push({ line: s.line, code: s.code ?? "" });
  }

  return { writers: [...writers.values()], readers: [...readers.values()], gaps };
}

/** The one-line answer over the list. */
export function describeUsage(field: string, usage: FieldUsage): string {
  const w = usage.writers.length;
  const r = usage.readers.length;
  const methods = (n: number) => `${n} ${n === 1 ? "method" : "methods"}`;
  if (w === 0 && r === 0) return `Nothing indexed reads or writes ${field}.`;
  if (w === 0) return `${field} is read in ${methods(r)} and never written — it behaves as a constant.`;
  if (r === 0) return `${field} is written in ${methods(w)} and never read back.`;
  return `${field} is written in ${methods(w)} and read in ${methods(r)}.`;
}

/** The field as a whole word: `id` must not match inside `userId`. */
function fieldPattern(field: string, flags = "") {
  const escaped = field.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  return new RegExp(`(?<![\\w$])(${escaped})(?![\\w$])`, flags);
}

/** The code split around the field's name, so a row can light up the part that matters. */
export function splitOnField(code: string, field: string): { text: string; hit: boolean }[] {
  if (!field) return [{ text: code, hit: false }];
  const parts = code.split(fieldPattern(field, "g"));
  return parts.filter((p) => p !== "").map((p) => ({ text: p, hit: p === field }));
}

/**
 * A long line cut from the left so the field stays on screen: the rest of
 * the row truncates at the right, and the field is the part worth seeing.
 * Keeps `lead` characters of context before it.
 */
export function focusOnField(code: string, field: string, lead = 24): string {
  const at = field ? code.search(fieldPattern(field)) : -1;
  if (at <= lead) return code;
  return `…${code.slice(at - lead).trimStart()}`;
}
