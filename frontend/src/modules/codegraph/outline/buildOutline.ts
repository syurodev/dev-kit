import type { CodeGraphNode, CodeGraphStep, CodeGraphStepFrame, CodeGraphSteps } from "@/bridge/codegraph";
import { shortLabel } from "../graph/labels";

/**
 * The flow outline as rows: the root method, then its steps in source order,
 * nested under the control structures they sit in, with a project call
 * expanding into the callee's own steps. Pure — the view windows these rows
 * and keeps the open/closed choices; nothing here fetches.
 *
 * Keys are paths ("root/3/fold@5"), so a row keeps its open state while the
 * rows around it change.
 */
export type OutlineRef = { reason: "shown-above" | "recursion"; key: string };

export type OutlineRow =
  | { kind: "method"; key: string; depth: number; nodeId: string; title: string; summary: string; line: number; open: boolean; expandable: boolean; impl: boolean; ref?: OutlineRef }
  | { kind: "frame"; key: string; depth: number; frameKind: string; text: string; exits: boolean }
  | { kind: "call"; key: string; depth: number; ownerId: string; step: CodeGraphStep; title: string; targetId?: string; open: boolean; expandable: boolean; hidden: boolean; ref?: OutlineRef }
  | { kind: "fold"; key: string; depth: number; count: number; reasons: string; open: boolean }
  | { kind: "more"; key: string; depth: number; nodeId: string };

export type OutlineInput = {
  rootId: string;
  nodes: ReadonlyMap<string, CodeGraphNode>;
  steps: CodeGraphSteps;
  /** Rows whose open state the person changed, by key. */
  toggled: Readonly<Record<string, boolean>>;
  /** Levels of methods open by default, the root included. */
  depth?: number;
};

export const DEFAULT_OUTLINE_DEPTH = 3;

/** "logging, metrics" — the most frequent reasons first, three at most. */
export function summarizeReasons(reasons: string[]): string {
  const counts = new Map<string, number>();
  for (const r of reasons) counts.set(r, (counts.get(r) ?? 0) + 1);
  return [...counts.entries()]
    .sort((a, b) => b[1] - a[1] || reasons.indexOf(a[0]) - reasons.indexOf(b[0]))
    .slice(0, 3)
    .map(([r]) => r)
    .join(", ");
}

/** The first sentence of a doc comment, for a row or a card. */
export function firstSentence(summary: string): string {
  const text = summary.trim();
  const end = text.search(/[.!?](\s|$)/);
  return end >= 0 ? text.slice(0, end + 1) : text;
}

export function buildOutline(input: OutlineInput): OutlineRow[] {
  const limit = input.depth ?? DEFAULT_OUTLINE_DEPTH;
  const rows: OutlineRow[] = [];
  const firstAt = new Map<string, string>();
  const truncated = new Set(input.steps.truncated);
  // Something to expand into: a method whose steps were read and are not empty.
  const hasSteps = (id: string) => (input.steps.steps[id]?.length ?? 0) > 0;
  const isOpen = (key: string, fallback: boolean) => input.toggled[key] ?? fallback;

  const root = input.nodes.get(input.rootId);
  if (!root) return rows;
  const rootOpen = isOpen("root", true);
  rows.push(methodRow(root, "root", 0, rootOpen, hasSteps(root.id), false));
  if (rootOpen) {
    firstAt.set(root.id, "root");
    body(root.id, "root", 1, 0, new Set([root.id]));
  }
  return rows;

  function methodRow(n: CodeGraphNode, key: string, depth: number, open: boolean, expandable: boolean, impl: boolean, ref?: OutlineRef): OutlineRow {
    return { kind: "method", key, depth, nodeId: n.id, title: shortLabel(n.label, n.kind), summary: firstSentence(n.summary), line: n.line, open, expandable, impl, ref };
  }

  function body(ownerId: string, parentKey: string, depth: number, level: number, path: ReadonlySet<string>) {
    const list = input.steps.steps[ownerId] ?? [];
    const usedKeys = new Set<string>();
    let stack: CodeGraphStepFrame[] = [];
    let pending: { index: number; step: CodeGraphStep }[] = [];
    let lastCallAt = "";

    const unique = (key: string) => {
      let k = key;
      for (let n = 2; usedKeys.has(k); n++) k = `${key}~${n}`;
      usedKeys.add(k);
      return k;
    };
    const flush = () => {
      if (pending.length === 0) return;
      const key = `${parentKey}/fold@${pending[0].index}`;
      const open = isOpen(key, false);
      const d = depth + stack.length;
      rows.push({ kind: "fold", key, depth: d, count: pending.length, reasons: summarizeReasons(pending.map((p) => p.step.reason)), open });
      if (open) for (const p of pending) callRow(ownerId, p.step, `${parentKey}/${p.index}`, d + 1, level, path, true);
      pending = [];
    };

    list.forEach((step, index) => {
      const frames = step.frames;
      let common = 0;
      while (common < stack.length && common < frames.length && stack[common].id === frames[common].id) common++;
      const sameGroup = common === stack.length && common === frames.length;
      if (!sameGroup || step.visible) flush();
      for (let i = common; i < frames.length; i++) {
        const f = frames[i];
        // A closure passed to the call just drawn is that call's body.
        if (f.kind === "closure" && f.id === lastCallAt) continue;
        rows.push({ kind: "frame", key: unique(`${parentKey}/frame@${f.id}`), depth: depth + i, frameKind: f.kind, text: f.text, exits: !!f.exits });
      }
      stack = frames;
      if (!step.visible) {
        pending.push({ index, step });
        return;
      }
      callRow(ownerId, step, `${parentKey}/${index}`, depth + frames.length, level, path, false);
      lastCallAt = `${step.line}:${step.col}`;
    });
    flush();
    if (truncated.has(ownerId)) rows.push({ kind: "more", key: `${parentKey}/more`, depth, nodeId: ownerId });
  }

  function callRow(ownerId: string, step: CodeGraphStep, key: string, depth: number, level: number, path: ReadonlySet<string>, hidden: boolean) {
    const target = step.to ? input.nodes.get(step.to) : undefined;
    // A call into a method with no body (a repository or client interface)
    // names a project node it cannot expand into.
    const named = target ?? (step.target ? input.nodes.get(step.target) : undefined);
    const title = step.text || (named ? shortLabel(named.label, named.kind) : step.calleeKey || "call");
    const impls = step.impls ?? [];
    let ref: OutlineRef | undefined;
    if (target && path.has(target.id)) ref = { reason: "recursion", key: firstAt.get(target.id) ?? "root" };
    const expandable = !!target && !ref && (impls.length > 0 || hasSteps(target.id));
    const open = expandable && isOpen(key, level + 1 < limit);
    if (target && open && impls.length === 0 && firstAt.has(target.id)) ref = { reason: "shown-above", key: firstAt.get(target.id)! };
    rows.push({ kind: "call", key, depth, ownerId, step, title, targetId: named?.id, open: open && !ref, expandable: expandable && !ref, hidden, ref });
    if (!open || ref || !target) return;

    if (impls.length > 0) {
      for (const implId of impls) {
        const impl = input.nodes.get(implId);
        if (!impl) continue;
        const implKey = `${key}/impl:${implId}`;
        const seen = firstAt.get(implId);
        const implRef: OutlineRef | undefined = path.has(implId)
          ? { reason: "recursion", key: seen ?? "root" }
          : seen ? { reason: "shown-above", key: seen } : undefined;
        const implOpen = !implRef && hasSteps(implId) && isOpen(implKey, level + 1 < limit);
        rows.push(methodRow(impl, implKey, depth + 1, implOpen, !implRef && hasSteps(implId), true, implRef));
        if (implOpen) {
          firstAt.set(implId, implKey);
          body(implId, implKey, depth + 2, level + 1, new Set([...path, implId]));
        }
      }
      return;
    }
    firstAt.set(target.id, key);
    body(target.id, key, depth + 1, level + 1, new Set([...path, target.id]));
  }
}
