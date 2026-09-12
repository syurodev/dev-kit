import { describe, expect, test } from "bun:test";
import { dialectOf, splitStatements, statementAt } from "./statements";

const pg = dialectOf("postgres");
const mysql = dialectOf("mysql");

const texts = (sql: string, dialect = pg) => splitStatements(sql, dialect).map((s) => s.text);

/**
 * A semicolon only ends a statement when it is not inside something.
 *
 * Every case here is a place a naive `sql.split(";")` sends half a statement
 * to the database — which, for the DELETE below, would be a DELETE with no
 * WHERE against a live table.
 */
describe("where one statement ends", () => {
  test("the everyday console: one statement, no terminator", () => {
    expect(texts("SELECT * FROM orders")).toEqual(["SELECT * FROM orders"]);
  });

  test("two statements", () => {
    expect(texts("SELECT 1;\nSELECT 2")).toEqual(["SELECT 1", "SELECT 2"]);
  });

  test("a trailing semicolon does not make an empty second statement", () => {
    expect(texts("SELECT 1;")).toEqual(["SELECT 1"]);
    expect(texts("SELECT 1;\n\n  \n")).toEqual(["SELECT 1"]);
  });

  test("a note after the last statement is a note, not a statement", () => {
    expect(texts("SELECT 1; -- done")).toEqual(["SELECT 1"]);
    expect(texts("SELECT 1;\n/* nothing\n   to see */")).toEqual(["SELECT 1"]);
  });

  test("a semicolon inside a string is not the end of anything", () => {
    expect(texts("DELETE FROM notes WHERE body = 'a; b' AND id = 1")).toEqual([
      "DELETE FROM notes WHERE body = 'a; b' AND id = 1",
    ]);
  });

  test("a doubled quote does not close the string", () => {
    expect(texts("SELECT 'it''s here; really' AS x")).toEqual([
      "SELECT 'it''s here; really' AS x",
    ]);
  });

  test("a semicolon inside a comment is not the end of anything", () => {
    expect(texts("SELECT 1 -- ; not here\n + 1")).toEqual(["SELECT 1 -- ; not here\n + 1"]);
    expect(texts("SELECT /* ; */ 1")).toEqual(["SELECT /* ; */ 1"]);
  });

  test("a quoted identifier can hold one too", () => {
    expect(texts('SELECT "odd;name" FROM t')).toEqual(['SELECT "odd;name" FROM t']);
    expect(texts("SELECT `odd;name` FROM t", mysql)).toEqual(["SELECT `odd;name` FROM t"]);
  });

  // Postgres function bodies are full of semicolons and the whole body is one
  // statement. Splitting it would send half a function definition.
  test("a dollar-quoted body is one statement", () => {
    const sql = "CREATE FUNCTION f() RETURNS int AS $$ BEGIN; RETURN 1; END; $$ LANGUAGE plpgsql";
    expect(texts(sql)).toEqual([sql]);
  });

  test("a tagged dollar quote closes on its own tag", () => {
    const sql = "SELECT $body$ a; $notme$ b $body$ AS x";
    expect(texts(sql)).toEqual([sql]);
  });

  // MySQL reads \' as an escaped quote; Postgres, with standard strings, does
  // not — the backslash there is just a character and the quote closes.
  test("backslash escapes follow the engine", () => {
    expect(texts("SELECT 'a\\'; b' AS x", mysql)).toEqual(["SELECT 'a\\'; b' AS x"]);
    expect(texts("SELECT 'a\\'; b' AS x", pg)).toEqual(["SELECT 'a\\'", "b' AS x"]);
  });

  test("an unterminated string does not run off into an infinite loop", () => {
    expect(texts("SELECT 'never closed")).toEqual(["SELECT 'never closed"]);
    expect(texts("SELECT /* never closed")).toEqual(["SELECT /* never closed"]);
  });

  test("nothing in, nothing out", () => {
    expect(texts("")).toEqual([]);
    expect(texts("   \n\n")).toEqual([]);
    expect(texts("-- just a thought")).toEqual([]);
  });
});

/**
 * The offsets are what the gutter marker and the caret both work from, so
 * they have to point at the statement itself rather than at the whitespace
 * in front of it.
 */
describe("where a statement sits in the text", () => {
  test("the range covers the statement and not the blank line above it", () => {
    const sql = "\n\n  SELECT 1;\n";
    const [statement] = splitStatements(sql, pg);
    expect(sql.slice(statement.from, statement.to)).toBe("SELECT 1");
  });

  /*
   * The comment a statement opens with stays in the text — it is what the
   * user wrote — but the marker in the gutter goes on the line that ran. A
   * tick beside "-- takings by day" is a tick beside a note.
   */
  test("the marker's offset skips the note the statement opens with", () => {
    const sql = "-- takings by day\n-- careful, prod\nSELECT sum(total) FROM orders";
    const [statement] = splitStatements(sql, pg);
    expect(statement.from).toBe(0);
    expect(sql.slice(statement.codeFrom)).toBe("SELECT sum(total) FROM orders");
  });

  test("and equals the start when there is no note", () => {
    const [statement] = splitStatements("  SELECT 1", pg);
    expect(statement.codeFrom).toBe(statement.from);
  });
});

describe("the statement the caret is in", () => {
  const sql = "SELECT 1;\nSELECT 2;\nSELECT 3";
  const statements = splitStatements(sql, pg);
  const at = (caret: number) => statementAt(statements, caret)?.text;

  test("inside it", () => {
    expect(at(3)).toBe("SELECT 1");
    expect(at(sql.indexOf("SELECT 2") + 2)).toBe("SELECT 2");
    expect(at(sql.length)).toBe("SELECT 3");
  });

  // You finish typing, you press ⌘↵, and the caret is one newline past the
  // semicolon. That still means the statement you just finished.
  test("just after it", () => {
    expect(at(sql.indexOf("\nSELECT 2"))).toBe("SELECT 1");
  });

  test("above everything, it means the first one", () => {
    const withHeader = splitStatements("-- notes\nSELECT 1", pg);
    expect(statementAt(withHeader, 2)?.text).toBe("-- notes\nSELECT 1");
  });

  test("an empty console has nothing to run", () => {
    expect(statementAt(splitStatements("  ", pg), 1)).toBeUndefined();
  });
});
