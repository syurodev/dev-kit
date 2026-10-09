/**
 * The smallest single replacement that turns `before` into `after`: the common
 * prefix and suffix are kept, only the middle is replaced.
 *
 * An editor told "the file on disk changed" used to replace its whole document,
 * and a whole-document replacement moves the caret to an edge and loses the
 * scroll position even when one line differs. Replacing only the changed span
 * leaves everything around it — the caret, the selection, the viewport — where
 * the person left it, which is what "reload without losing my place" means.
 *
 * Splits are made on UTF-16 code units, so a surrogate pair is never cut: the
 * prefix and suffix are backed off when they would end inside one.
 */
export function minimalChange(before: string, after: string): { from: number; to: number; insert: string } {
  if (before === after) return { from: 0, to: 0, insert: "" };
  const max = Math.min(before.length, after.length);
  let start = 0;
  while (start < max && before.charCodeAt(start) === after.charCodeAt(start)) start++;
  if (start > 0 && isHighSurrogate(before.charCodeAt(start - 1))) start--;

  let endBefore = before.length;
  let endAfter = after.length;
  while (endBefore > start && endAfter > start && before.charCodeAt(endBefore - 1) === after.charCodeAt(endAfter - 1)) {
    endBefore--;
    endAfter--;
  }
  if (endBefore < before.length && isLowSurrogate(before.charCodeAt(endBefore))) {
    endBefore++;
    endAfter++;
  }
  return { from: start, to: endBefore, insert: after.slice(start, endAfter) };
}

const isHighSurrogate = (c: number) => c >= 0xd800 && c <= 0xdbff;
const isLowSurrogate = (c: number) => c >= 0xdc00 && c <= 0xdfff;
