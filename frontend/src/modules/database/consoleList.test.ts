import { describe, expect, test } from "bun:test";
import { consoleLabel, groupConsoles, UNLINKED_LABEL, whenOf } from "./consoleList";
import type { ConnectionProfile, StoredConsole } from "../../bridge/database";

const file = (id: string, profileId: string, sql = "SELECT 1"): StoredConsole => ({
  id,
  profileId,
  database: "public",
  sql,
  updatedAt: "2026-09-12T10:00:00Z",
});

const profile = (id: string, name: string): ConnectionProfile => ({
  id,
  name,
  host: "",
  port: 0,
  database: "",
  username: "",
  sslMode: "",
  env: "",
  readOnly: false,
});

/**
 * The rail shows what the console DOES, not what it is called. A file name
 * of `console-3-x8f2la` is the same kind of nothing for every console you
 * have.
 */
describe("a console's line in the rail", () => {
  test("is the first thing it actually does", () => {
    expect(consoleLabel("SELECT * FROM orders;")).toBe("SELECT * FROM orders;");
  });

  // A console that opens with a note about what it is for still has to be
  // told apart from the other four, and the note is rarely what does that.
  test("skips the note at the top and finds the statement", () => {
    expect(consoleLabel("-- takings, by day\n-- careful, prod\n\nSELECT day, sum(total)\nFROM orders")).toBe(
      "SELECT day, sum(total)",
    );
  });

  test("collapses the whitespace a formatter leaves behind", () => {
    expect(consoleLabel("   SELECT\t\t1   ")).toBe("SELECT 1");
  });

  test("has nothing to say about an empty console", () => {
    expect(consoleLabel("")).toBe("");
    expect(consoleLabel("\n\n   \n")).toBe("");
    expect(consoleLabel("-- nothing but a comment")).toBe("");
  });

  test("does not run a paragraph into the rail", () => {
    expect(consoleLabel("SELECT " + "x".repeat(400)).length).toBe(120);
  });
});

describe("how long ago", () => {
  const now = Date.parse("2026-09-12T12:00:00Z");
  test.each([
    ["2026-09-12T11:59:40Z", "now"],
    ["2026-09-12T11:35:00Z", "25m"],
    ["2026-09-12T09:00:00Z", "3h"],
    ["2026-09-10T12:00:00Z", "2d"],
  ])("%s → %s", (iso, expected) => {
    expect(whenOf(iso, now)).toBe(expected);
  });

  // Past a week nobody is counting days: they are looking for the one from
  // before the release.
  test("stops counting days after a week", () => {
    expect(whenOf("2026-08-02T12:00:00Z", now)).not.toContain("d");
  });

  test("says nothing rather than NaN when there is no stamp", () => {
    expect(whenOf("")).toBe("");
    expect(whenOf("not a date")).toBe("");
  });
});

describe("consoles under the connection they were written for", () => {
  const profiles = [profile("p1", "prod"), profile("p2", "staging")];

  test("in the connections' own order, so both rail lists read the same way", () => {
    const groups = groupConsoles([file("a", "p2"), file("b", "p1")], profiles);
    expect(groups.map((g) => g.label)).toEqual(["prod", "staging"]);
  });

  test("a connection with no consoles is not a heading", () => {
    const groups = groupConsoles([file("a", "p1")], profiles);
    expect(groups).toHaveLength(1);
  });

  /*
   * The consoles with no other way in: from a deleted connection, from a
   * folder someone pointed the setting at, or from the crash that left the
   * file without its tab. Hiding them is how they stay lost.
   */
  test("everything else is gathered at the end, not dropped", () => {
    const groups = groupConsoles(
      [file("a", "p1"), file("orphan", "deleted-profile"), file("never", "")],
      profiles,
    );
    const last = groups[groups.length - 1];
    expect(last.label).toBe(UNLINKED_LABEL);
    expect(last.files.map((f) => f.id)).toEqual(["orphan", "never"]);
  });

  test("and is absent when there is nothing in it", () => {
    const groups = groupConsoles([file("a", "p1")], profiles);
    expect(groups.some((g) => g.label === UNLINKED_LABEL)).toBe(false);
  });
});
