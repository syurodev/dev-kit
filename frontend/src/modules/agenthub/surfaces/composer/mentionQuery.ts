import type { ContextFileItem } from "../AgentMenu";

/**
 * The "@" being typed, read off the text before the caret: an "@" at the
 * start of the text or after whitespace, then everything up to the caret with
 * no space in it. `start` is the offset of the "@" itself, so replacing
 * `[start, caret)` swaps the whole "@query" for the chip.
 *
 * Read from the text rather than opened by a keystroke, the way the
 * slash-command list is: the field keeps focus the whole time. Plate's own
 * mention combobox moved focus into an <input> of its own the moment "@"
 * landed — and an input method that had already begun composing the next
 * letter in the field (Vietnamese Telex commits "@" only when "s" arrives)
 * lost that letter with the focus: "@sche" became "@che".
 */
export function mentionQueryBefore(textBeforeCaret: string): { query: string; start: number } | null {
  const match = /(?:^|\s)@([^\s@]*)$/.exec(textBeforeCaret);
  if (!match) return null;
  return { query: match[1], start: textBeforeCaret.length - match[1].length - 1 };
}

/**
 * The files worth showing for a query, best first — the same order the
 * backend's listing uses (internal/agenthub listContextFiles): a name that
 * starts with the query, then a name that contains it, then a path that does,
 * shorter paths first within each. Only for a caller with nothing to ask
 * (no search); a search's own answer is already in this order.
 */
export function filterContextFiles(files: readonly ContextFileItem[], query: string): ContextFileItem[] {
  const needle = query.trim().toLowerCase();
  if (!needle) return [...files];
  const rank = (file: ContextFileItem) => {
    const name = (file.name || file.path.split("/").pop() || "").toLowerCase();
    if (name.startsWith(needle)) return 0;
    if (name.includes(needle)) return 1;
    if ((file.description ?? file.path).toLowerCase().includes(needle)) return 2;
    return -1;
  };
  return files
    .map((file) => ({ file, rank: rank(file), shown: file.description ?? file.path }))
    .filter((entry) => entry.rank >= 0)
    .sort((a, b) => a.rank - b.rank || a.shown.length - b.shown.length || a.shown.localeCompare(b.shown))
    .map((entry) => entry.file);
}
