import type { DiffLine } from "@/lib/lineDiff";

/** A run of unchanged lines folded out of a diff, and how many it hides. */
export type DiffGap = { kind: "gap"; count: number };
export type DiffRow = DiffLine | DiffGap;

/** A diff line that remembers which line of each side it came from; -1 for none. */
export type SourcedDiffLine = DiffLine & { before: number; after: number };

/**
 * Keeps `context` unchanged lines either side of every change and folds the
 * rest into gaps, the way a unified diff shows hunks. A whole-file diff of a
 * one-line edit would otherwise be the whole file.
 */
export function foldContext<T extends DiffLine>(lines: T[], context = 3): Array<T | DiffGap> {
  const keep = new Array<boolean>(lines.length).fill(false);
  lines.forEach((line, i) => {
    if (line.kind === "same") return;
    for (let k = Math.max(0, i - context); k <= Math.min(lines.length - 1, i + context); k++) keep[k] = true;
  });
  const out: Array<T | DiffGap> = [];
  let hidden = 0;
  lines.forEach((line, i) => {
    if (keep[i]) {
      if (hidden) out.push({ kind: "gap", count: hidden });
      hidden = 0;
      out.push(line);
    } else {
      hidden++;
    }
  });
  if (hidden) out.push({ kind: "gap", count: hidden });
  return out;
}

/**
 * Numbers each diff line with the line it came from on either side, so a
 * caller can look up that line's syntax colours (the diff is over the text,
 * the highlighting over each whole file).
 */
export function sourceLines(lines: DiffLine[]): SourcedDiffLine[] {
  let before = 0;
  let after = 0;
  return lines.map((line) => ({
    ...line,
    before: line.kind === "added" ? -1 : before++,
    after: line.kind === "removed" ? -1 : after++,
  }));
}

export function diffStats(lines: DiffLine[]): { added: number; removed: number } {
  let added = 0;
  let removed = 0;
  for (const line of lines) {
    if (line.kind === "added") added++;
    else if (line.kind === "removed") removed++;
  }
  return { added, removed };
}

/**
 * lineDiff for whole files. The lines both sides share at the top and bottom
 * are matched first and only the middle goes through the LCS, which is
 * quadratic: a one-line edit to a 5,000-line file is then a diff of a few
 * lines, not 25 million table cells.
 */
export function wholeFileDiff(
  before: string,
  after: string,
  diff: (a: string, b: string) => DiffLine[],
): DiffLine[] {
  const a = before.split("\n");
  const b = after.split("\n");
  let start = 0;
  while (start < a.length && start < b.length && a[start] === b[start]) start++;
  let end = 0;
  while (end < a.length - start && end < b.length - start && a[a.length - 1 - end] === b[b.length - 1 - end]) end++;
  const head: DiffLine[] = a.slice(0, start).map((text) => ({ kind: "same", text }));
  const tail: DiffLine[] = a.slice(a.length - end).map((text) => ({ kind: "same", text }));
  const midA = a.slice(start, a.length - end);
  const midB = b.slice(start, b.length - end);
  const middle: DiffLine[] =
    midA.length === 0
      ? midB.map((text) => ({ kind: "added", text }))
      : midB.length === 0
        ? midA.map((text) => ({ kind: "removed", text }))
        : diff(midA.join("\n"), midB.join("\n"));
  return [...head, ...middle, ...tail];
}

/** One row of a two-pane diff: the line each side shows, or nothing. */
export type DiffPair = { left: SourcedDiffLine | null; right: SourcedDiffLine | null };

/**
 * Lays a diff out as two panes, a row at a time — Figma's 08 Diff example D:
 * the file on the left, what it becomes on the right, changed lines facing
 * each other.
 *
 * Unchanged lines appear on both sides. A run of removals is zipped against
 * the run of additions that follows it, so a line and its replacement share a
 * row; whichever run is longer leaves blanks opposite its tail.
 */
export function splitPairs(lines: SourcedDiffLine[]): DiffPair[] {
  const pairs: DiffPair[] = [];
  for (let i = 0; i < lines.length; ) {
    if (lines[i].kind === "same") {
      pairs.push({ left: lines[i], right: lines[i] });
      i++;
      continue;
    }
    const removed: SourcedDiffLine[] = [];
    const added: SourcedDiffLine[] = [];
    while (i < lines.length && lines[i].kind === "removed") removed.push(lines[i++]);
    while (i < lines.length && lines[i].kind === "added") added.push(lines[i++]);
    for (let k = 0; k < Math.max(removed.length, added.length); k++) {
      pairs.push({ left: removed[k] ?? null, right: added[k] ?? null });
    }
  }
  return pairs;
}

/** A run of changed rows, for the ribbon drawn in the gutter between the panes. */
export type DiffRun = { start: number; end: number; kind: "modified" | "added" | "removed" };

/** The changed runs of a two-pane diff, as row index ranges (end exclusive). */
export function changedRuns(pairs: DiffPair[]): DiffRun[] {
  const runs: DiffRun[] = [];
  let start = -1;
  const changed = (pair: DiffPair) => pair.left?.kind === "removed" || pair.right?.kind === "added";
  for (let i = 0; i <= pairs.length; i++) {
    if (i < pairs.length && changed(pairs[i])) {
      if (start === -1) start = i;
      continue;
    }
    if (start === -1) continue;
    const run = pairs.slice(start, i);
    const hasRemoved = run.some((pair) => pair.left?.kind === "removed");
    const hasAdded = run.some((pair) => pair.right?.kind === "added");
    runs.push({
      start,
      end: i,
      kind: hasRemoved && hasAdded ? "modified" : hasAdded ? "added" : "removed",
    });
    start = -1;
  }
  return runs;
}
