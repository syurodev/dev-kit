/**
 * Where one statement ends and the next begins.
 *
 * A console holds a script, and until now the whole of it went to the driver
 * as one string — which is not a thing either driver will run. pgx uses the
 * extended protocol, so two statements come back as "cannot insert multiple
 * commands into a prepared statement"; MySQL refuses them unless the DSN
 * opts into multi-statements, which is the switch that turns every injection
 * into a stacked one. So the split happens HERE, and each statement is sent
 * on its own through the call that already exists.
 *
 * DataGrip's model, and the reason it works: the default is not "run the
 * script", it is "run the statement the caret is in". Several results are the
 * exception; one result from the statement you are looking at is the everyday
 * case, and that needs no new bridge, no new cancel semantics and no DSN flag.
 *
 * A semicolon only ends a statement when it is not inside something. The
 * something depends on the engine, which is why this takes a dialect rather
 * than guessing.
 */

export type Statement = {
  /** Offsets into the original text, so the caret can be mapped back. */
  from: number;
  to: number;
  /**
   * Where the SQL itself starts, past any comment the statement opens with.
   *
   * The comment stays in `text` — it is what the user wrote and it costs
   * nothing to send — but a tick in the gutter beside "-- takings by day"
   * is a tick beside a note. The marker goes on the line that ran.
   */
  codeFrom: number;
  /** The statement itself, trimmed, without its terminating semicolon. */
  text: string;
};

export type SqlDialect = {
  /** MySQL and MariaDB read \' inside a string; Postgres and SQLite do not. */
  backslashEscapes: boolean;
  /** Postgres $tag$ … $tag$ bodies, which hold semicolons and quotes. */
  dollarQuotes: boolean;
};

export function dialectOf(engine: string | undefined): SqlDialect {
  const id = (engine || "postgres").toLowerCase();
  return {
    backslashEscapes: id === "mysql" || id === "mariadb",
    dollarQuotes: id === "postgres",
  };
}

/**
 * Every statement in the text, in order.
 *
 * A run of whitespace and comments between two semicolons is not a
 * statement: `SELECT 1; -- done` is one statement and a note, not two.
 *
 * Known limit: MySQL's DELIMITER command is not honoured, so the body of a
 * CREATE PROCEDURE splits at its inner semicolons. That is a client-side
 * command with its own parsing rules, and getting it half right is worse
 * than not claiming it — a procedure body is one of the few places where
 * splitting wrongly changes what runs.
 */
export function splitStatements(sql: string, dialect: SqlDialect): Statement[] {
  const statements: Statement[] = [];
  let start = 0;
  let i = 0;

  const push = (from: number, to: number) => {
    const text = sql.slice(from, to);
    if (!isOnlyComments(text)) {
      // The offsets are trimmed to the real text so a marker in the gutter
      // lands on the statement's own line, not on the blank line above it.
      const lead = text.length - text.trimStart().length;
      const tail = text.length - text.trimEnd().length;
      statements.push({
        from: from + lead,
        to: to - tail,
        codeFrom: from + firstCodeOffset(text),
        text: text.trim(),
      });
    }
  };

  while (i < sql.length) {
    const char = sql[i];

    if (char === "-" && sql[i + 1] === "-") {
      i = endOfLine(sql, i);
      continue;
    }
    if (char === "/" && sql[i + 1] === "*") {
      const close = sql.indexOf("*/", i + 2);
      i = close === -1 ? sql.length : close + 2;
      continue;
    }
    if (char === "'" || char === '"' || char === "`") {
      i = endOfQuoted(sql, i, char, dialect);
      continue;
    }
    if (dialect.dollarQuotes && char === "$") {
      const end = endOfDollarQuoted(sql, i);
      if (end !== null) {
        i = end;
        continue;
      }
    }
    if (char === ";") {
      push(start, i);
      start = i + 1;
    }
    i += 1;
  }

  // Whatever follows the last semicolon, including a script that has none —
  // which is the everyday console, where nobody types a terminator.
  push(start, sql.length);
  return statements;
}

/**
 * The statement the caret is in, or the one it most recently left.
 *
 * "Most recently left" is what makes the blank line after a statement still
 * belong to it: you finish typing, you press ⌘↵, and the caret is one
 * newline past the semicolon. Falling back to the first statement covers the
 * caret sitting in a comment header above everything.
 */
export function statementAt(statements: readonly Statement[], caret: number): Statement | undefined {
  if (statements.length === 0) return undefined;
  const inside = statements.find((s) => caret >= s.from && caret <= s.to);
  if (inside) return inside;
  let before: Statement | undefined;
  for (const statement of statements) {
    if (statement.to < caret) before = statement;
  }
  return before ?? statements[0];
}

/**
 * True when the chunk is only whitespace and comments.
 *
 * No dialect needed: a chunk reaching here came out of the scanner above
 * with its quotes already balanced, so the first character that is not
 * whitespace or a comment means there is a statement in it.
 */
function isOnlyComments(text: string): boolean {
  return firstCodeOffset(text) === text.length;
}

/**
 * The offset of the first character that is neither whitespace nor comment,
 * or the length when there is none — which is also what says a chunk holds
 * no statement at all.
 */
function firstCodeOffset(text: string): number {
  let i = 0;
  while (i < text.length) {
    const char = text[i];
    if (/\s/.test(char)) {
      i += 1;
      continue;
    }
    if (char === "-" && text[i + 1] === "-") {
      i = endOfLine(text, i);
      continue;
    }
    if (char === "/" && text[i + 1] === "*") {
      const close = text.indexOf("*/", i + 2);
      i = close === -1 ? text.length : close + 2;
      continue;
    }
    return i;
  }
  return text.length;
}

function endOfLine(text: string, from: number): number {
  const newline = text.indexOf("\n", from);
  return newline === -1 ? text.length : newline + 1;
}

/** Past the closing quote. A doubled quote inside is an escaped one. */
function endOfQuoted(text: string, from: number, quote: string, dialect: SqlDialect): number {
  let i = from + 1;
  while (i < text.length) {
    const char = text[i];
    if (dialect.backslashEscapes && char === "\\") {
      i += 2;
      continue;
    }
    if (char === quote) {
      if (text[i + 1] === quote) {
        i += 2;
        continue;
      }
      return i + 1;
    }
    i += 1;
  }
  return text.length;
}

/** Past the closing $tag$, or null when this $ is not opening one. */
function endOfDollarQuoted(text: string, from: number): number | null {
  const open = /^\$[A-Za-z_]*\$/.exec(text.slice(from));
  if (!open) return null;
  const tag = open[0];
  const close = text.indexOf(tag, from + tag.length);
  return close === -1 ? text.length : close + tag.length;
}
