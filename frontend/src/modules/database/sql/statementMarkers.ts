import { splitStatements, type SqlDialect } from "./statements";

/** What happened to a statement the last time it was run. */
export type StatementOutcome = "ok" | "error";

/**
 * Which line gets which mark.
 *
 * Keyed by the statement's TEXT rather than by its position, which is what
 * makes the marks survive editing: insert a line above and every offset
 * moves, but the statement you already ran is still the same statement.
 * Change the statement itself and its mark goes, which is right — the answer
 * you have is about something you no longer have.
 *
 * Pure, and separate from the gutter that draws it, because this is the part
 * with a rule in it. The gutter is twelve lines of CodeMirror.
 */
export function statementMarkers(
  sql: string,
  dialect: SqlDialect,
  outcomes: Readonly<Record<string, StatementOutcome>>,
): Map<number, StatementOutcome> {
  const marks = new Map<number, StatementOutcome>();
  if (Object.keys(outcomes).length === 0) return marks;

  for (const statement of splitStatements(sql, dialect)) {
    const outcome = outcomes[statement.text];
    if (outcome) {
      marks.set(lineOf(sql, statement.codeFrom), outcome);
    }
  }
  return marks;
}

/** 1-based, the way an editor counts. */
function lineOf(text: string, offset: number): number {
  let line = 1;
  for (let i = 0; i < offset && i < text.length; i += 1) {
    if (text[i] === "\n") line += 1;
  }
  return line;
}
