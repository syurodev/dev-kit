import type { BudgetEdit, BudgetPage } from "../bridge/harness";
import { BudgetKind } from "../../bindings/devkit/internal/harnessconfig/models";

/**
 * Drafts of result-budget edits. Only what the person changed is kept, as an
 * intent per field: leave alone (no entry), set a value, or reset (remove the
 * rule's field so the next level applies). Nothing here resolves inheritance or
 * judges widening versus narrowing: the effective limits come from Go with each
 * row, and the review of the exact limits before and after is written by Go.
 */

export type Kind = "tool" | "group";
export type CutValue = "head" | "headTail";
export type Intent<T> = { op: "set"; value: T } | { op: "reset" };
export type BudgetDraftEntry = { bytes?: Intent<number>; cut?: Intent<CutValue> };
export type BudgetDraft = Record<string, BudgetDraftEntry>;

export const MIN_BYTES = 2048;
export const MAX_BYTES = 65536;
export const cutLabels: Record<CutValue, string> = { head: "Start only", headTail: "Start and end" };

export const draftKey = (kind: Kind, name: string) => `${kind}:${name}`;

function splitKey(key: string): [Kind, string] {
  const i = key.indexOf(":");
  return [key.slice(0, i) as Kind, key.slice(i + 1)];
}

/** The explicit rule the saved profile holds for a tool or group on the loaded pages. */
export type SavedRule = { bytes?: number; cut?: string };

export function savedRule(page: BudgetPage | null, kind: Kind, name: string): SavedRule | null {
  if (!page) {
    return null;
  }
  if (kind === "group") {
    const g = (page.groups ?? []).find((row) => row.group === name);
    return g ? { bytes: g.bytes ?? undefined, cut: g.cut || undefined } : null;
  }
  const t = (page.tools ?? []).find((row) => row.tool === name);
  return t ? { bytes: t.toolBytes ?? undefined, cut: t.toolCut || undefined } : null;
}

/** Parses a size typed in KiB into exact bytes, or says what is wrong. */
export function parseKiB(text: string): { ok: true; bytes: number } | { ok: false; error: string } {
  const trimmed = text.trim();
  if (!/^\d+(\.\d{1,3})?$/.test(trimmed)) {
    return { ok: false, error: "Enter a number of KiB, for example 16." };
  }
  const bytes = Number(trimmed) * 1024;
  if (!Number.isInteger(bytes)) {
    return { ok: false, error: "That is not a whole number of bytes." };
  }
  if (bytes < MIN_BYTES || bytes > MAX_BYTES) {
    return { ok: false, error: "A result limit is between 2 KiB and 64 KiB." };
  }
  return { ok: true, bytes };
}

export function formatKiB(bytes: number): string {
  const kib = bytes / 1024;
  return `${Number.isInteger(kib) ? kib : kib.toFixed(2)} KiB (${bytes.toLocaleString("en-US")} bytes)`;
}

/** An intent that changes nothing relative to the saved rule is not a pending edit. */
function isNoop(field: "bytes" | "cut", intent: Intent<number> | Intent<CutValue>, saved: SavedRule | null): boolean {
  if (!saved) {
    return false; // not on a loaded page: it cannot be shown to be redundant
  }
  const current = field === "bytes" ? saved.bytes : saved.cut;
  if (intent.op === "reset") {
    return current === undefined;
  }
  return current === intent.value;
}

/**
 * Sets (or with undefined clears) one field's intent. Choosing what is already
 * saved removes the pending edit, and a draft with no field left disappears.
 */
export function setIntent(
  draft: BudgetDraft,
  kind: Kind,
  name: string,
  field: "bytes" | "cut",
  intent: Intent<number> | Intent<CutValue> | undefined,
  saved: SavedRule | null,
): BudgetDraft {
  const key = draftKey(kind, name);
  const entry: BudgetDraftEntry = { ...(draft[key] ?? {}) };
  if (intent === undefined || isNoop(field, intent, saved)) {
    delete entry[field];
  } else if (field === "bytes") {
    entry.bytes = intent as Intent<number>;
  } else {
    entry.cut = intent as Intent<CutValue>;
  }
  const next = { ...draft };
  if (entry.bytes === undefined && entry.cut === undefined) {
    delete next[key];
  } else {
    next[key] = entry;
  }
  return next;
}

/**
 * Keeps unsaved edits across a refresh: an intent the saved profile now satisfies
 * is done, one whose tool or group the server no longer registers is dropped, and
 * everything else — including an edit for a row not on the loaded pages — stays
 * until the person discards it.
 */
export function rebaseBudgets(draft: BudgetDraft, page: BudgetPage | null): BudgetDraft {
  let next: BudgetDraft = {};
  const known = (kind: Kind, name: string) =>
    !page || !page.tools
      ? true
      : kind === "group"
        ? (page.groups ?? []).length === 0 || (page.groups ?? []).some((g) => g.group === name)
        : true;
  for (const [key, entry] of Object.entries(draft)) {
    const [kind, name] = splitKey(key);
    if (!known(kind, name)) {
      continue;
    }
    const saved = savedRule(page, kind, name);
    for (const field of ["bytes", "cut"] as const) {
      const intent = entry[field];
      if (intent) {
        next = setIntent(next, kind, name, field, intent, saved);
      }
    }
  }
  return next;
}

export const isBudgetDirty = (draft: BudgetDraft) => Object.keys(draft).length > 0;

export function pendingCount(draft: BudgetDraft): number {
  return Object.values(draft).reduce((n, e) => n + (e.bytes ? 1 : 0) + (e.cut ? 1 : 0), 0);
}

/** The edits to prepare, in a stable order, exactly as the person left them. */
export function buildBudgetEdits(draft: BudgetDraft): BudgetEdit[] {
  return Object.keys(draft)
    .sort()
    .map((key) => {
      const [kind, name] = splitKey(key);
      const e = draft[key];
      const edit: BudgetEdit = { kind: kind === "group" ? BudgetKind.BudgetGroup : BudgetKind.BudgetTool, name };
      if (e.bytes?.op === "set") {
        edit.bytes = e.bytes.value;
      } else if (e.bytes?.op === "reset") {
        edit.resetBytes = true;
      }
      if (e.cut?.op === "set") {
        edit.cut = e.cut.value;
      } else if (e.cut?.op === "reset") {
        edit.resetCut = true;
      }
      return edit;
    });
}

export function describeEntry(entry: BudgetDraftEntry): string {
  const parts: string[] = [];
  if (entry.bytes) {
    parts.push(entry.bytes.op === "reset" ? "size: inherit" : `size: ${formatKiB(entry.bytes.value)}`);
  }
  if (entry.cut) {
    parts.push(entry.cut.op === "reset" ? "cut: inherit" : `cut: ${cutLabels[entry.cut.value]}`);
  }
  return parts.join(" · ");
}

export function sourceLabel(source: string | undefined): string {
  switch (source) {
    case "tool":
      return "set on the tool";
    case "group":
      return "from the group";
    default:
      return "default";
  }
}

/**
 * A small, deterministic illustration of what the two cut modes keep, built from
 * obviously fake lines. It is not the cutter and not the person's data: the real
 * cut is done on the server, and what a review lists is exact.
 */
export function samplePreview(mode: CutValue, bytes: number): string[] {
  const lines = Array.from({ length: 24 }, (_, i) => `sample line ${String(i + 1).padStart(2, "0")} — fake output`);
  const keep = Math.max(2, Math.min(10, Math.round(bytes / 4096)));
  if (keep >= lines.length) {
    return lines;
  }
  if (mode === "head") {
    return [...lines.slice(0, keep), `[… ${lines.length - keep} lines left out]`];
  }
  const head = Math.ceil(keep / 2);
  const tail = keep - head;
  return [...lines.slice(0, head), `[… ${lines.length - keep} lines in the middle left out]`, ...lines.slice(lines.length - tail)];
}
