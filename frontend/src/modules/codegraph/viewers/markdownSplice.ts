/**
 * Saving a Markdown file that was edited visually without rewriting it.
 *
 * Plate holds a tree, and writing the tree back produces ITS Markdown: tables
 * re-padded, a hard-wrapped paragraph turned into hard line breaks (`\` at each
 * line end, which also changes how it reads), spacing normalised. Saved whole,
 * one typo in a README would show up in git as hundreds of changed lines. So the
 * file is never written from the tree. Instead:
 *
 *   - `norm(text)` is what Plate would write for `text` (deserialise, serialise);
 *   - the file as it was, put through `norm`, is the BASE; the editor's current
 *     output is the EDIT; the difference between those two is what the person did;
 *   - that difference is applied to the original text, replacing only the blocks
 *     it touches (a block is a run of lines between blank lines, fences kept
 *     whole). Every other byte of the file stays as written.
 *
 * A block that was edited is written the way Plate writes it; a block that was
 * not is not touched. If applying the difference does not reproduce the editor's
 * content (checked by running the result through `norm` again), the editor's
 * output is returned whole instead: formatting may change, an edit is never lost.
 */

export type Norm = (markdown: string) => string;

type Block = { start: number; end: number };

const FENCE = /^ {0,3}(`{3,}|~{3,})/;

/** Runs of non-blank lines, a fenced code block (blank lines and all) counting as one. */
export function splitBlocks(lines: string[]): Block[] {
  const blocks: Block[] = [];
  let start = -1;
  let fence = "";
  lines.forEach((line, i) => {
    if (fence) {
      const m = FENCE.exec(line);
      if (m && m[1][0] === fence[0] && m[1].length >= fence.length && line.trim() === m[1]) fence = "";
    } else if (line.trim() !== "") {
      const m = FENCE.exec(line);
      if (m) fence = m[1];
    }
    const blank = !fence && line.trim() === "";
    if (blank) {
      if (start >= 0) blocks.push({ start, end: i });
      start = -1;
    } else if (start < 0) {
      start = i;
    }
  });
  if (start >= 0) blocks.push({ start, end: lines.length });
  return blocks;
}

/**
 * For each line of `a`, the index of the line of `b` it is matched with in a
 * longest common subsequence, or -1. The common start and end are matched first,
 * so an edit in a long file costs only the stretch between them.
 */
export function matchLines(a: string[], b: string[]): Int32Array {
  const map = new Int32Array(a.length).fill(-1);
  let head = 0;
  while (head < a.length && head < b.length && a[head] === b[head]) {
    map[head] = head;
    head++;
  }
  let ta = a.length;
  let tb = b.length;
  while (ta > head && tb > head && a[ta - 1] === b[tb - 1]) {
    ta--;
    tb--;
    map[ta] = tb;
  }
  const n = ta - head;
  const m = tb - head;
  if (n === 0 || m === 0 || n * m > 4_000_000) return map;
  // lcs[i][j]: length of the LCS of a[head+i..ta) and b[head+j..tb).
  const width = m + 1;
  const lcs = new Uint16Array((n + 1) * width);
  for (let i = n - 1; i >= 0; i--) {
    for (let j = m - 1; j >= 0; j--) {
      lcs[i * width + j] =
        a[head + i] === b[head + j] ? lcs[(i + 1) * width + j + 1] + 1 : Math.max(lcs[(i + 1) * width + j], lcs[i * width + j + 1]);
    }
  }
  let i = 0;
  let j = 0;
  while (i < n && j < m) {
    if (a[head + i] === b[head + j]) {
      map[head + i] = head + j;
      i++;
      j++;
    } else if (lcs[(i + 1) * width + j] >= lcs[i * width + j + 1]) i++;
    else j++;
  }
  return map;
}

/** A line's letters and digits only: what survives Plate's rewriting of punctuation and spacing. */
const keyOf = (line: string) => line.replace(/[^\p{L}\p{N}]+/gu, "");

type Line = { text: string; at: number };
const nonBlank = (lines: string[]): Line[] => lines.flatMap((text, at) => (text.trim() === "" ? [] : [{ text, at }]));

type Hunk = { a0: number; a1: number; c0: number; c1: number };

/** The stretches where `a` and `c` differ, as half-open ranges over both. */
function hunksOf(match: Int32Array, aLen: number, cLen: number): { hunks: Hunk[]; mapC: Int32Array } {
  const hunks: Hunk[] = [];
  let a = 0;
  let c = 0;
  while (a < aLen || c < cLen) {
    if (a < aLen && match[a] === c) {
      a++;
      c++;
      continue;
    }
    const a0 = a;
    const c0 = c;
    while (a < aLen && match[a] === -1) a++;
    // c advances to the next matched line of a (or the end).
    c = a < aLen ? match[a] : cLen;
    hunks.push({ a0, a1: a, c0, c1: c });
  }
  return { hunks, mapC: match };
}

/**
 * `src` with `edited` (the editor's current output) applied block by block.
 * `norm` must be the same function that produced `edited`'s dialect.
 */
export type Spliced = {
  text: string;
  /** The editor's own writing was used for the whole file: its formatting, not the file's, is what is saved. */
  reformatted: boolean;
};

export function spliceMarkdown(src: string, edited: string, norm: Norm): Spliced {
  const base = norm(src);
  if (base.trim() === edited.trim()) return { text: src, reformatted: false };

  const srcLines = src.split("\n");
  const blocks = splitBlocks(srcLines);
  if (blocks.length === 0) return { text: edited, reformatted: true };

  // Which block each line of the base came from. The base is Plate's writing of the file, so its
  // lines are the file's lines with the punctuation and spacing changed (`*` for `-`, padded table
  // cells, a trailing `\`) — never re-wrapped. Lines are therefore matched by their letters and
  // digits alone, in order.
  const A = nonBlank(base.split("\n"));
  const srcLinesKeyed: Array<{ key: string; block: number }> = [];
  blocks.forEach((b, block) => {
    for (let i = b.start; i < b.end; i++) {
      const key = keyOf(srcLines[i]);
      if (key) srcLinesKeyed.push({ key, block });
    }
  });
  const aKeyed: Array<{ key: string; a: number }> = [];
  A.forEach((l, a) => {
    const key = keyOf(l.text);
    if (key) aKeyed.push({ key, a });
  });
  const keyMatch = matchLines(
    aKeyed.map((l) => l.key),
    srcLinesKeyed.map((l) => l.key),
  );
  const prov = new Array<number>(A.length).fill(-1);
  aKeyed.forEach((l, i) => {
    if (keyMatch[i] >= 0) prov[l.a] = srcLinesKeyed[keyMatch[i]].block;
  });
  // A line with no letters or digits (a fence, `|---|`, `---`) goes with the lines around it in the
  // base's own block: an opening fence with what follows, anything else with what precedes it.
  const baseLines = base.split("\n");
  const baseBlockOf = new Map<number, number>();
  splitBlocks(baseLines).forEach((b, id) => {
    for (let i = b.start; i < b.end; i++) baseBlockOf.set(i, id);
  });
  const inSameBlock = (i: number, j: number) => baseBlockOf.get(A[i].at) === baseBlockOf.get(A[j].at);
  for (let i = 0; i < prov.length; i++) {
    if (prov[i] >= 0) continue;
    const opening = i + 1 < prov.length && inSameBlock(i, i + 1) && (i === 0 || !inSameBlock(i - 1, i));
    const neighbours = opening ? [i + 1, i - 1] : [i - 1, i + 1];
    for (const j of neighbours) {
      if (j >= 0 && j < prov.length && inSameBlock(i, j) && prov[j] >= 0) {
        prov[i] = prov[j];
        break;
      }
    }
  }
  // What is still unplaced (a block of its own with no letters) sits with the line before it.
  for (let i = 0; i < prov.length; i++) if (prov[i] < 0 && i > 0) prov[i] = prov[i - 1];
  for (let i = prov.length - 1; i >= 0; i--) if (prov[i] < 0 && i + 1 < prov.length) prov[i] = prov[i + 1];

  const editedLines = edited.split("\n");
  const C = nonBlank(editedLines);
  const { hunks, mapC } = hunksOf(
    matchLines(
      A.map((l) => l.text),
      C.map((l) => l.text),
    ),
    A.length,
    C.length,
  );

  // The blocks each hunk touches; hunks that share a block become one group.
  type Group = { b0: number; b1: number; c0: number; c1: number; a0: number; a1: number };
  const groups: Group[] = [];
  const insertions = new Map<number, Hunk[]>(); // after block index (-1: at the top)
  for (const h of hunks) {
    let b0 = Infinity;
    let b1 = -Infinity;
    for (let a = h.a0; a < h.a1; a++) {
      if (prov[a] >= 0) {
        b0 = Math.min(b0, prov[a]);
        b1 = Math.max(b1, prov[a]);
      }
    }
    if (h.a0 === h.a1) {
      // Pure insertion: inside a block if both neighbours are in the same one, else between blocks.
      const before = h.a0 > 0 ? prov[h.a0 - 1] : -1;
      const after = h.a0 < A.length ? prov[h.a0] : -1;
      if (before >= 0 && before === after) {
        b0 = b1 = before;
      } else {
        const list = insertions.get(before) ?? [];
        list.push(h);
        insertions.set(before, list);
        continue;
      }
    }
    if (b0 === Infinity) return { text: edited, reformatted: true };
    const last = groups[groups.length - 1];
    if (last && b0 <= last.b1) {
      last.b1 = Math.max(last.b1, b1);
      last.c0 = Math.min(last.c0, h.c0);
      last.c1 = Math.max(last.c1, h.c1);
      last.a0 = Math.min(last.a0, h.a0);
      last.a1 = Math.max(last.a1, h.a1);
    } else {
      groups.push({ b0, b1, c0: h.c0, c1: h.c1, a0: h.a0, a1: h.a1 });
    }
  }

  // A group replaces whole blocks, so it takes in every line of them — not only the changed ones.
  const hunkAt = (a: number) => hunks.find((h) => a >= h.a0 && a < h.a1);
  for (const g of groups) {
    let first = Infinity;
    let last = -Infinity;
    prov.forEach((b, a) => {
      if (b >= g.b0 && b <= g.b1) {
        first = Math.min(first, a);
        last = Math.max(last, a);
      }
    });
    const hf = hunkAt(first);
    const hl = hunkAt(last);
    g.c0 = Math.min(g.c0, hf ? hf.c0 : mapC[first]);
    g.c1 = Math.max(g.c1, hl ? hl.c1 : mapC[last] + 1);
  }
  groups.sort((x, y) => x.b0 - y.b0);
  for (let i = 1; i < groups.length; i++) if (groups[i].b0 <= groups[i - 1].b1) return { text: edited, reformatted: true };

  const edText = (c0: number, c1: number) => (c1 > c0 ? editedLines.slice(C[c0].at, C[c1 - 1].at + 1).join("\n") : "");

  // Assemble: untouched blocks verbatim with the blank lines they had, touched ones as the editor wrote them.
  const out: Array<{ text: string; gap: number }> = [];
  const gapAfter = (i: number) => (i + 1 < blocks.length ? blocks[i + 1].start - blocks[i].end : 0);
  const insert = (after: number) => {
    for (const h of insertions.get(after) ?? []) out.push({ text: edText(h.c0, h.c1), gap: 1 });
  };
  insert(-1);
  let i = 0;
  while (i < blocks.length) {
    const g = groups.find((x) => x.b0 === i);
    if (g) {
      const text = edText(g.c0, g.c1);
      if (text.trim() !== "") out.push({ text, gap: gapAfter(g.b1) });
      for (let b = g.b0; b <= g.b1; b++) insert(b);
      i = g.b1 + 1;
    } else {
      out.push({ text: srcLines.slice(blocks[i].start, blocks[i].end).join("\n"), gap: gapAfter(i) });
      insert(i);
      i++;
    }
  }
  // Blank lines before the first block, and the newlines after the last, are kept as they were.
  const lead = "\n".repeat(blocks[0].start);
  const tail = "\n".repeat(srcLines.length - blocks[blocks.length - 1].end);
  const body = out.map((p, k) => (k < out.length - 1 ? `${p.text}${"\n".repeat(Math.max(1, p.gap) + 1)}` : p.text)).join("");
  const result = `${lead}${body}${tail}`;

  // Never lose an edit: if the splice does not read as the editor's content, hand back the editor's own output.
  // `edited` is what the editor wrote; `norm` of it is what it settles on (not always the same: a `* [ ]`
  // task item or a stray rule is only fully rewritten the second time). The result may match either.
  const got = norm(result).trim();
  return got === edited.trim() || got === norm(edited).trim() ? { text: result, reformatted: false } : { text: edited, reformatted: true };
}

/**
 * Whether Plate would drop or rewrite text of `src` (not just its spacing and punctuation): the
 * letters and digits of the file, in order, are not the ones of what Plate writes back. Content
 * Plate cannot represent (an HTML block it cannot read, a reference-style link definition, an
 * entity) vanishes or changes on a round trip, and a save from the editor would delete it — so such
 * a file is not edited visually. Whole-file, not line by line: Plate moves line breaks around inline
 * code, which is only layout.
 */
export function losesText(src: string, norm: Norm): boolean {
  const letters = (text: string) => text.replace(/[^\p{L}\p{N}]+/gu, "");
  return letters(src) !== letters(norm(src));
}
