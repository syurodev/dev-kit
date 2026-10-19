/**
 * A chip's name in two parts: a head that may be cut with an ellipsis, and a
 * tail that is always shown. The tail is the extension and the last few
 * characters before it, because the end of a file name is what tells files
 * apart ("…(consolidated from ADR-0001…0054)-191026.pdf"); the start of a long
 * name is the part most alike. A name that fits as one piece is all head.
 */
export const NAME_TAIL_CHARS = 8;
/** Names shorter than this are not worth splitting. */
const SPLIT_FROM = 20;

export function splitName(name: string): { head: string; tail: string } {
  if (name.length < SPLIT_FROM) return { head: name, tail: "" };
  const dot = name.lastIndexOf(".");
  // A leading dot (".env") or a long "extension" is part of the name, not an extension.
  const ext = dot > 0 && name.length - dot <= 8 ? name.slice(dot) : "";
  const stem = ext ? name.slice(0, dot) : name;
  const keep = Math.min(NAME_TAIL_CHARS, Math.floor(stem.length / 2));
  return { head: stem.slice(0, stem.length - keep), tail: stem.slice(stem.length - keep) + ext };
}
