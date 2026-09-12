import { describe, expect, test } from "bun:test";
import { consoleStatus, statementWarning } from "./consoleStatus";

/**
 * What the pill beside Run says, as data.
 *
 * Pulled out of the component because it is the whole of the feature: which of
 * four things happened, in how long, and in one line short enough to sit in a
 * toolbar. A component that decided this inline would be a component nobody
 * could test the wording of.
 */
describe("consoleStatus", () => {
  test("while running it is only the clock", () => {
    const status = consoleStatus({ running: true, elapsedSec: 4.04, sql: "SELECT 1" });
    expect(status).toEqual({ kind: "running", time: "4.0s" });
  });

  test("nothing has run yet, so there is nothing to say", () => {
    expect(consoleStatus({ running: false, elapsedSec: 0, sql: "SELECT 1" })).toEqual({
      kind: "idle",
    });
  });

  // Seconds, not the raw milliseconds the result header prints, and for one
  // reason: the number was already on screen counting in seconds. Changing its
  // unit at the moment it stops makes the reader re-read it.
  test("the duration keeps the unit it was counting in", () => {
    const status = consoleStatus({
      running: false,
      elapsedSec: 0,
      durationMs: 20012,
      rowCount: 1,
      sql: "SELECT SLEEP(20)",
    });
    expect(status).toEqual({ kind: "success", time: "20.0s", message: "1 row" });
  });

  test("rows are counted in the plural people actually use", () => {
    const status = consoleStatus({ running: false, elapsedSec: 0, durationMs: 120, rowCount: 4, sql: "SELECT 1" });
    expect(status.kind === "success" && status.message).toBe("4 rows");
  });

  test("an error leads with its first line and keeps the rest to expand", () => {
    const status = consoleStatus({
      running: false,
      elapsedSec: 0,
      durationMs: 31,
      error: "ERROR: relation \"user\" does not exist\nLINE 1: SELECT * FROM user\n                      ^",
      sql: "SELECT * FROM user",
    });
    expect(status).toEqual({
      kind: "error",
      time: "0.0s",
      message: 'ERROR: relation "user" does not exist',
      detail: 'ERROR: relation "user" does not exist\nLINE 1: SELECT * FROM user\n                      ^',
    });
  });

  /**
   * The bug this pair came from: idle was decided by "is there a duration",
   * and the store only recorded one when a statement SUCCEEDED. So a failure
   * left the message in the store and the pill said nothing at all — the
   * backend had the engine's sentence, the frontend showed a blank toolbar.
   *
   * An error is by itself proof that something ran.
   */
  test("an error shows even when nothing timed it", () => {
    const status = consoleStatus({
      running: false,
      elapsedSec: 0,
      error: "Error 1054 (42S22): Unknown column 'hello' in 'SELECT'",
      sql: "SELECT hello",
    });
    expect(status.kind).toBe("error");
    expect(status.kind === "error" && status.message).toContain("Unknown column 'hello'");
  });

  test("and still says nothing when truly nothing has run", () => {
    expect(consoleStatus({ running: false, elapsedSec: 0, sql: "SELECT 1" }).kind).toBe("idle");
  });

  // A statement that changed every row it could reach succeeded, and saying so
  // and nothing else is how you find out tomorrow.
  test("a write with no WHERE succeeded and is still worth saying out loud", () => {
    const status = consoleStatus({
      running: false,
      elapsedSec: 0,
      durationMs: 90,
      rowCount: 0,
      sql: "UPDATE employees SET active = 0",
    });
    expect(status).toEqual({ kind: "warning", time: "0.1s", message: "UPDATE with no WHERE" });
  });

  test("an error outranks the warning, because it is what happened", () => {
    const status = consoleStatus({
      running: false,
      elapsedSec: 0,
      durationMs: 5,
      error: "syntax error at or near \"SETT\"",
      sql: "UPDATE employees SETT active = 0",
    });
    expect(status.kind).toBe("error");
  });
});

/**
 * The one warning worth a pre-emptive line, and the two ways a naive version
 * cries wolf: the word WHERE inside a string, and the word UPDATE inside a
 * comment.
 */
describe("statementWarning", () => {
  test.each([
    ["UPDATE employees SET active = 0", "UPDATE with no WHERE"],
    ["update employees set active = 0", "UPDATE with no WHERE"],
    ["DELETE FROM sessions", "DELETE with no WHERE"],
    ["  delete from sessions ;  ", "DELETE with no WHERE"],
  ])("%s", (sql, expected) => {
    expect(statementWarning(sql)).toBe(expected);
  });

  test.each([
    ["UPDATE employees SET active = 0 WHERE id = 7"],
    ["DELETE FROM sessions WHERE expires_at < now()"],
    ["SELECT * FROM employees"],
    ["INSERT INTO audit (note) VALUES ('delete from sessions')"],
    [""],
  ])("%s is not worth warning about", (sql) => {
    expect(statementWarning(sql)).toBeNull();
  });

  // The string is stripped before the check, so a WHERE that only exists
  // inside quotes must not count as one.
  test("a WHERE inside a literal is not a WHERE", () => {
    expect(statementWarning("UPDATE notes SET body = 'where did it go'")).toBe(
      "UPDATE with no WHERE",
    );
  });

  test("a statement that is only a comment says nothing", () => {
    expect(statementWarning("-- UPDATE employees SET active = 0")).toBeNull();
    expect(statementWarning("/* DELETE FROM sessions */ SELECT 1")).toBeNull();
  });
});
