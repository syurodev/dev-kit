import type { CodeGraphNode, CodeGraphStep, CodeGraphStepFrame, CodeGraphSteps } from "@/bridge/codegraph";
import { shortLabel } from "../graph/labels";

/**
 * The flow outline as rows: the root method, then its steps in source order,
 * nested under the control structures they sit in, with a project call
 * expanding into the callee's own steps. Pure — the view windows these rows
 * and keeps the open/closed choices; nothing here fetches.
 *
 * Keys are paths of ids that stay put when a method's body changes around
 * them (root, call:Svc#send#0, fold:Log#info#1): a call is named by what it
 * calls and which occurrence of that it is in its method, never by its index,
 * so a row keeps its open state across a re-index that adds or removes steps
 * elsewhere. Opaque to the view: compare them, never parse them.
 */
export type OutlineRef = { reason: "shown-above" | "recursion"; key: string };

export type OutlineRow =
  | {
      kind: "method"; key: string; depth: number; nodeId: string; title: string; summary: string; line: number;
      open: boolean; expandable: boolean;
      /** Listed under a call as one of the methods it can run (an override, or the overridden method itself). */
      impl: boolean;
      /** The overridden method itself, listed first among its overrides because it has a body of its own. */
      base?: boolean;
      ref?: OutlineRef;
    }
  | {
      kind: "frame"; key: string; depth: number; frameKind: string; text: string; exits: boolean;
      /** How the branch leaves, when the index recorded it (see CodeGraphStepFrame.exit). */
      exit?: CodeGraphStepFrame["exit"];
    }
  | { kind: "call"; key: string; depth: number; ownerId: string; step: CodeGraphStep; title: string; targetId?: string; open: boolean; expandable: boolean; hidden: boolean; ref?: OutlineRef }
  | {
      kind: "fold"; key: string; depth: number; count: number; reasons: string; open: boolean;
      /** Every reason in the fold with its count, for the chip's tooltip. */
      counts: ReasonCount[];
    }
  | { kind: "more"; key: string; depth: number; nodeId: string };

export type OutlineInput = {
  rootId: string;
  nodes: ReadonlyMap<string, CodeGraphNode>;
  steps: CodeGraphSteps;
  /** Rows whose open state the person changed, by key. */
  toggled: Readonly<Record<string, boolean>>;
  /** See DEFAULT_OUTLINE_DEPTH. */
  depth?: number;
  /**
   * Every row open, folds included, whatever `toggled` says — the whole tree,
   * to find the rows leading to a node the person's choices keep closed.
   */
  openAll?: boolean;
};

/**
 * Levels of methods whose bodies are open by default, the root included: with
 * 3, the root's body, its callees' bodies and theirs are drawn, and the calls
 * inside those last bodies are drawn closed.
 */
export const DEFAULT_OUTLINE_DEPTH = 3;

export type ReasonCount = { reason: string; count: number };

/** Every reason with how often it occurs, the most frequent first, ties in the order met. */
export function countReasons(reasons: readonly string[]): ReasonCount[] {
  const counts = new Map<string, number>();
  for (const r of reasons) counts.set(r, (counts.get(r) ?? 0) + 1);
  return [...counts.entries()]
    .sort((a, b) => b[1] - a[1] || reasons.indexOf(a[0]) - reasons.indexOf(b[0]))
    .map(([reason, count]) => ({ reason, count }));
}

/** "logging, metrics" — the most frequent reasons first, three at most. */
export function summarizeReasons(reasons: string[]): string {
  return countReasons(reasons)
    .slice(0, 3)
    .map((r) => r.reason)
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
  const isOpen = (key: string, fallback: boolean) => (input.openAll ? true : (input.toggled[key] ?? fallback));

  const root = input.nodes.get(input.rootId);
  if (!root) return rows;
  const rootOpen = isOpen("root", true);
  rows.push(methodRow(root, "root", 0, rootOpen, hasSteps(root.id), false));
  if (rootOpen) {
    firstAt.set(root.id, "root");
    body(root.id, "root", 1, 0, new Set([root.id]));
  }
  return rows;

  function methodRow(n: CodeGraphNode, key: string, depth: number, open: boolean, expandable: boolean, impl: boolean, ref?: OutlineRef, base?: boolean): OutlineRow {
    return {
      kind: "method", key, depth, nodeId: n.id, title: shortLabel(n.label, n.kind), summary: firstSentence(n.summary), line: n.line,
      open, expandable, impl, ...(base ? { base: true } : {}), ref,
    };
  }

  function body(ownerId: string, parentKey: string, depth: number, level: number, path: ReadonlySet<string>) {
    const list = input.steps.steps[ownerId] ?? [];
    const ids = stepIds(list);
    const frameKeys = new Set<string>();
    // Calls drawn at this body's own level, by position, and how many rows
    // there were right after each call's row: a closure passed to one is that
    // call's body.
    const drawn = new Map<string, number>();
    let stack: CodeGraphStepFrame[] = [];
    let pending: { id: string; step: CodeGraphStep }[] = [];

    const unique = (key: string) => {
      let k = key;
      for (let n = 2; frameKeys.has(k); n++) k = `${key}~${n}`;
      frameKeys.add(k);
      return k;
    };
    const flush = () => {
      if (pending.length === 0) return;
      const key = childKey(parentKey, "fold", pending[0].id);
      const open = isOpen(key, false);
      const d = depth + stack.length;
      const reasons = pending.map((p) => p.step.reason);
      rows.push({ kind: "fold", key, depth: d, count: pending.length, reasons: summarizeReasons(reasons), counts: countReasons(reasons), open });
      if (open) for (const p of pending) callRow(ownerId, p.step, childKey(parentKey, "call", p.id), d + 1, level, path, true);
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
        // A closure passed to a call whose row is the last thing drawn reads
        // as that row's body, one level in. Anything drawn after the call (its
        // callee's open body, a later argument's call, a fold) would put the
        // closure's steps under the wrong row, so then it gets a heading.
        if (f.kind === "closure" && drawn.get(f.id) === rows.length) continue;
        rows.push({
          kind: "frame", key: unique(childKey(parentKey, "frame", f.id)), depth: depth + i, frameKind: f.kind, text: f.text, exits: !!f.exits,
          ...(f.exit ? { exit: f.exit } : {}),
        });
      }
      stack = frames;
      if (!step.visible) {
        pending.push({ id: ids[index], step });
        return;
      }
      // The count right after the call's own row, before any body it opens.
      drawn.set(`${step.line}:${step.col}`, rows.length + 1);
      callRow(ownerId, step, childKey(parentKey, "call", ids[index]), depth + frames.length, level, path, false);
    });
    flush();
    if (truncated.has(ownerId)) rows.push({ kind: "more", key: childKey(parentKey, "more", ""), depth, nodeId: ownerId });
  }

  function callRow(ownerId: string, step: CodeGraphStep, key: string, depth: number, level: number, path: ReadonlySet<string>, hidden: boolean) {
    const target = step.to ? input.nodes.get(step.to) : undefined;
    // A call into a method with no body (a repository or client interface)
    // names a project node it cannot expand into.
    const named = target ?? (step.target ? input.nodes.get(step.target) : undefined);
    const title = step.text || (named ? shortLabel(named.label, named.kind) : step.calleeKey || "call");
    // What the call can run: its overrides that are in this graph, after the
    // overridden method itself when that has a body of its own.
    const overrides = target ? (step.impls ?? []).filter((id) => id !== target.id && input.nodes.has(id)) : [];
    const variants = target && overrides.length > 0 ? (hasSteps(target.id) ? [target.id, ...overrides] : overrides) : [];
    let ref: OutlineRef | undefined;
    if (target && path.has(target.id)) ref = { reason: "recursion", key: firstAt.get(target.id) ?? "root" };
    else if (target && variants.length === 0 && firstAt.has(target.id)) ref = { reason: "shown-above", key: firstAt.get(target.id)! };
    const expandable = !!target && !ref && (variants.length > 0 || hasSteps(target.id));
    const open = expandable && isOpen(key, level + 1 < limit);
    rows.push({ kind: "call", key, depth, ownerId, step, title, targetId: named?.id, open, expandable, hidden, ref });
    if (!open || !target) return;

    if (variants.length > 0) {
      for (const id of variants) {
        const variant = input.nodes.get(id)!;
        const variantKey = childKey(key, "impl", id);
        const seen = firstAt.get(id);
        const variantRef: OutlineRef | undefined = path.has(id)
          ? { reason: "recursion", key: seen ?? "root" }
          : seen ? { reason: "shown-above", key: seen } : undefined;
        const variantOpen = !variantRef && hasSteps(id) && isOpen(variantKey, level + 1 < limit);
        rows.push(methodRow(variant, variantKey, depth + 1, variantOpen, !variantRef && hasSteps(id), true, variantRef, id === target.id));
        if (variantOpen) {
          firstAt.set(id, variantKey);
          body(id, variantKey, depth + 2, level + 1, new Set([...path, id]));
        }
      }
      return;
    }
    firstAt.set(target.id, key);
    body(target.id, key, depth + 1, level + 1, new Set([...path, target.id]));
  }
}

/**
 * Joins a key's segments. A callee key is source text (a Lua require path, a
 * TypeScript file, a Java signature) and may hold "/", "#", "@" or ":", so the
 * joiner is a control character no key holds, and every segment says what kind
 * it is, so a callee named like a fold cannot be taken for one.
 */
const KEY_JOIN = "\u0001";
function childKey(parent: string, kind: "call" | "fold" | "frame" | "impl" | "more", id: string): string {
  return `${parent}${KEY_JOIN}${kind}:${id}`;
}

/**
 * Each step's id within its method: what it calls and which occurrence of that
 * it is ("Svc#send#1"), so the id survives steps added or removed elsewhere.
 */
function stepIds(list: readonly CodeGraphStep[]): string[] {
  const seen = new Map<string, number>();
  return list.map((s) => {
    const base = s.calleeKey || s.to || s.target || s.text;
    const n = seen.get(base) ?? 0;
    seen.set(base, n + 1);
    return `${base}#${n}`;
  });
}
