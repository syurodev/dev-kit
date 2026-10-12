/**
 * The composer's list and inline-format editing, as pure functions over a
 * block's text — no Plate, no DOM — so each rule is a test.
 *
 * A prompt leaves the composer exactly as typed (composerSerialization.ts), so
 * lists and emphasis are plain markdown text, not document nodes: "- one" is
 * what the person sees and what the agent reads, and nothing about a list can
 * be lost or rewritten on the way out. What the composer adds is the editing
 * that makes typing them pleasant — keep the list going on a new line, end it
 * on an empty item, toggle the markers on selected lines, wrap a selection.
 *
 * Offsets are into one text leaf (a paragraph holds its lines as "\n" breaks).
 */

export type ListKind = "bullet" | "ordered";

export type ListMarker = {
  indent: string;
  kind: ListKind;
  /** The marker as written: "-", "*", "3.", "3)". */
  text: string;
  /** For an ordered marker, its number. */
  number: number;
  /** Characters the marker takes up, indent and the space after it included. */
  length: number;
};

const MARKER = /^([ \t]*)(?:([-*+•])|(\d{1,3})([.)]))[ \t]+/;

/** The list marker a line starts with, if it does. "-x" and "1.5" are not lists. */
export function readListMarker(line: string): ListMarker | null {
  const match = MARKER.exec(line);
  if (!match) return null;
  const [whole, indent, bullet, digits, delimiter] = match;
  return bullet
    ? { indent, kind: "bullet", text: bullet, number: 0, length: whole.length }
    : { indent, kind: "ordered", text: `${digits}${delimiter}`, number: Number(digits), length: whole.length };
}

/** The marker the next item takes: the same bullet, or the next number. */
export function nextMarker(marker: ListMarker): string {
  if (marker.kind === "bullet") return marker.text;
  return `${marker.number + 1}${marker.text.slice(-1)}`;
}

export type Continuation =
  /** Insert this at the caret: a line break and the next marker. */
  | { kind: "continue"; insert: string }
  /** The item is empty: take the marker away and end the list. */
  | { kind: "end"; from: number; to: number };

/**
 * What Shift+Enter does on a line of a list, given the caret's offset within
 * that line. Null when the line is not a list item, or the caret is inside the
 * marker itself (there, it is an ordinary line break).
 */
export function continueList(line: string, caret: number): Continuation | null {
  const marker = readListMarker(line);
  if (!marker || caret < marker.length) return null;
  if (line.slice(marker.length).trim() === "") return { kind: "end", from: 0, to: line.length };
  return { kind: "continue", insert: `\n${marker.indent}${nextMarker(marker)} ` };
}

export type TextEdit = {
  /** Replace [from, to) of the leaf's text with `text`. */
  from: number;
  to: number;
  text: string;
  /** Where the selection goes afterwards, as offsets into the new leaf text. */
  select: { start: number; end: number };
};

function lineBounds(text: string, start: number, end: number): { from: number; to: number } {
  const from = text.lastIndexOf("\n", start - 1) + 1;
  const next = text.indexOf("\n", end);
  return { from, to: next === -1 ? text.length : next };
}

/**
 * Turns the lines a selection touches into a list of `kind`, or back into plain
 * lines when they all already are one. Existing markers (of either kind) are
 * replaced rather than stacked, an ordered list is numbered 1, 2, 3, indentation
 * is kept, and a blank line in the middle is left alone — except that a single
 * empty line gets a marker, so the shortcut starts a list from nothing.
 */
export function toggleList(text: string, start: number, end: number, kind: ListKind): TextEdit {
  const { from, to } = lineBounds(text, start, end);
  const lines = text.slice(from, to).split("\n");
  const items = lines.filter((line) => line.trim() !== "");
  const lone = lines.length === 1;
  const remove = items.length > 0 && items.every((line) => readListMarker(line)?.kind === kind);

  let n = 0;
  const out = lines.map((line) => {
    const marker = readListMarker(line);
    const body = marker ? line.slice(marker.length) : line.trimStart();
    const indent = marker ? marker.indent : line.slice(0, line.length - line.trimStart().length);
    if (remove) return marker ? indent + body : line;
    if (line.trim() === "" && !lone) return line;
    n += 1;
    return `${indent}${kind === "bullet" ? "-" : `${n}.`} ${body}`;
  });
  const replaced = out.join("\n");
  const caret = from + replaced.length;
  return { from, to, text: replaced, select: lone ? { start: caret, end: caret } : { start: from, end: caret } };
}

/** Wraps the selection in `mark` ("**", "*", "`"), or unwraps it when it already is. */
export function toggleWrap(text: string, start: number, end: number, mark: string): TextEdit {
  const len = mark.length;
  const selected = text.slice(start, end);
  // "*" must not mistake the edge of "**bold**" for its own: a single mark
  // counts only when no second one sits next to it.
  const bare = (index: number) => len > 1 || text[index] !== mark;

  const around =
    start >= len &&
    text.slice(start - len, start) === mark &&
    text.slice(end, end + len) === mark &&
    bare(start - len - 1) &&
    bare(end + len);
  if (around) {
    return { from: start - len, to: end + len, text: selected, select: { start: start - len, end: end - len } };
  }
  if (selected.length > 2 * len && selected.startsWith(mark) && selected.endsWith(mark) && bare(len) && bare(selected.length - len - 1)) {
    const inner = selected.slice(len, selected.length - len);
    return { from: start, to: end, text: inner, select: { start, end: start + inner.length } };
  }
  if (start === end) {
    return { from: start, to: end, text: mark + mark, select: { start: start + len, end: start + len } };
  }
  return { from: start, to: end, text: mark + selected + mark, select: { start: start + len, end: end + len } };
}

/** A formatting shortcut, read off a key event: Cmd/Ctrl+B, +I, +Shift+7, +Shift+8. */
export type FormatShortcut =
  | { kind: "wrap"; mark: string }
  | { kind: "list"; list: ListKind };

export function formatShortcut(e: {
  key: string;
  code?: string;
  metaKey: boolean;
  ctrlKey: boolean;
  shiftKey: boolean;
  altKey: boolean;
}): FormatShortcut | null {
  if (!(e.metaKey || e.ctrlKey) || e.altKey) return null;
  if (e.shiftKey) {
    if (e.code === "Digit8") return { kind: "list", list: "bullet" };
    if (e.code === "Digit7") return { kind: "list", list: "ordered" };
    return null;
  }
  const key = e.key.toLowerCase();
  if (key === "b") return { kind: "wrap", mark: "**" };
  if (key === "i") return { kind: "wrap", mark: "*" };
  return null;
}
