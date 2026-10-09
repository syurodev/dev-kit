/**
 * Line endings, kept honest.
 *
 * CodeMirror splits a document on any of `\n`, `\r\n` or `\r` and joins it back
 * with one separator, so a CRLF file read into it and written out again comes
 * back as LF — and, worse, differs from the text read from disk the instant the
 * editor mounts, which used to mark every CRLF file dirty before a key was
 * pressed. The fix is to say which separator the document uses (the editor's
 * `lineSeparator`), so what the editor hands back is byte for byte what it was
 * given.
 *
 * A file whose endings are all `\r\n`, or all `\n`, round-trips exactly. A file
 * that mixes them (or has bare `\r`) cannot be held faithfully in one document:
 * it is normalized to its dominant ending when opened, flagged `mixed`, and
 * saved that way only if the person edits it.
 */
export type Eol = "\n" | "\r\n";

export type EolInfo = { text: string; eol: Eol; mixed: boolean };

export function detectEol(raw: string): EolInfo {
  let crlf = 0;
  let lf = 0;
  let cr = 0;
  for (let i = 0; i < raw.length; i++) {
    const c = raw.charCodeAt(i);
    if (c === 13) {
      if (raw.charCodeAt(i + 1) === 10) {
        crlf++;
        i++;
      } else {
        cr++;
      }
    } else if (c === 10) {
      lf++;
    }
  }
  if (cr === 0 && (crlf === 0 || lf === 0)) {
    return { text: raw, eol: crlf > 0 ? "\r\n" : "\n", mixed: false };
  }
  const eol: Eol = crlf > lf + cr ? "\r\n" : "\n";
  return { text: raw.replace(/\r\n|\r|\n/g, eol), eol, mixed: true };
}
