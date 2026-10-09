export type DiffLine = {
  kind: "same" | "removed" | "added";
  text: string;
};

/**
 * Line diff between two note bodies, for the version history view.
 *
 * A longest-common-subsequence walk rather than a line-by-line comparison:
 * inserting one line at the top would otherwise mark every line below it as
 * both removed and added, which is the opposite of what a history is for.
 *
 * Lines, not words: the panel answers "what changed between these two saves",
 * which reads better whole-line — the call ResolvePanel's markers already make.
 */
export function lineDiff(before: string, after: string): DiffLine[] {
  const a = before.split("\n");
  const b = after.split("\n");

  // lcs[i][j] = length of the longest common subsequence of a[i:] and b[j:].
  const lcs: number[][] = Array.from({ length: a.length + 1 }, () =>
    new Array<number>(b.length + 1).fill(0),
  );
  for (let i = a.length - 1; i >= 0; i--) {
    for (let j = b.length - 1; j >= 0; j--) {
      lcs[i][j] = a[i] === b[j] ? lcs[i + 1][j + 1] + 1 : Math.max(lcs[i + 1][j], lcs[i][j + 1]);
    }
  }

  const out: DiffLine[] = [];
  let i = 0;
  let j = 0;
  while (i < a.length && j < b.length) {
    if (a[i] === b[j]) {
      out.push({ kind: "same", text: a[i] });
      i++;
      j++;
    } else if (lcs[i + 1][j] >= lcs[i][j + 1]) {
      out.push({ kind: "removed", text: a[i] });
      i++;
    } else {
      out.push({ kind: "added", text: b[j] });
      j++;
    }
  }
  for (; i < a.length; i++) out.push({ kind: "removed", text: a[i] });
  for (; j < b.length; j++) out.push({ kind: "added", text: b[j] });
  return out;
}
