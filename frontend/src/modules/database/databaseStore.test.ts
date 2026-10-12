import { afterEach, beforeEach, describe, expect, mock, test } from "bun:test";
import type { RunVerdict } from "./databaseStore";
import { consoleFiles } from "./consoleFiles";

// This file mocks the bridge it depends on, rather than running against
// whatever mock happens to be installed.
//
// bun's mock.module is global for the whole run and last-writer-wins, and two
// other files in this directory replace this same module — one of them with a
// runQuery that returns a promise nobody ever resolves, which is the point of
// THAT file and a five-second timeout in this one. A test that reads the
// store's bookkeeping must not depend on which file loaded first.
//
// Spread first: a replacement that lists only what it needs deletes every
// other export from everyone else's view.
const bridge = await import("../../bridge/database");

mock.module("../../bridge/database", () => ({
  ...bridge,
  runQuery: async () => ({ columns: ["n"], rows: [["1"]], rowCount: 1, truncated: false }),
  listProfiles: async () => [],
  listConsoles: async () => [],
  listColumns: async () => [],
  getSchemaCache: async () => null,
  indexSchema: async () => null,
  closeSession: async () => {},
  applyEdits: async () => ({ applied: 0, failed: [] }),
  setTransactionMode: async () => {},
  commitTransaction: async () => {},
  rollbackTransaction: async () => {},
  transactionState: async () => ({ manual: false, open: false }),
}));

const { useDatabaseStore, visibleResult } = await import("./databaseStore");

beforeEach(() => {
  useDatabaseStore.setState({ consoles: {} });
});

describe("console slices", () => {
  test("a console that was never opened reads as empty, not undefined", () => {
    const console = useDatabaseStore.getState().consoleState("never-opened");
    expect(console.sql).toBe("");
    expect(visibleResult(console)).toBeNull();
    expect(console.runningQuery).toBe(false);
  });

  test("two consoles hold their own SQL", () => {
    const { setSql } = useDatabaseStore.getState();
    setSql("a", "SELECT 1;");
    setSql("b", "SELECT 2;");

    const state = useDatabaseStore.getState();
    expect(state.consoleState("a").sql).toBe("SELECT 1;");
    expect(state.consoleState("b").sql).toBe("SELECT 2;");
  });

  test("closing a console drops its slice", () => {
    const { setSql, closeConsole } = useDatabaseStore.getState();
    setSql("a", "SELECT 1;");
    closeConsole("a");
    expect(useDatabaseStore.getState().consoles["a"]).toBeUndefined();
  });

  // A result belongs to the console that asked for it. Without this, a slow
  // query in one tab lands in whichever console happens to be on screen.
  test("a result lands in the console that ran it", () => {
    useDatabaseStore.setState((s) => ({
      consoles: {
        ...s.consoles,
        a: {
          ...s.consoleState("a"),
          results: [
            {
              statement: "SELECT n",
              result: { columns: ["n"], rows: [["1"]], rowCount: 1, truncated: false },
            },
          ],
        },
      },
    }));
    expect(visibleResult(useDatabaseStore.getState().consoleState("b"))).toBeNull();
    expect(visibleResult(useDatabaseStore.getState().consoleState("a"))?.rowCount).toBe(1);
  });
});

// smoothness 5, "stale response": queryRunId is what stops a slow first run
// overwriting a faster second one. Per console now, so two consoles running at
// once cannot invalidate each other.
describe("run tokens", () => {
  test("each console counts its own runs", () => {
    const { setSql } = useDatabaseStore.getState();
    setSql("a", "SELECT 1;");
    setSql("b", "SELECT 2;");

    useDatabaseStore.setState((s) => ({
      consoles: {
        ...s.consoles,
        a: { ...s.consoleState("a"), queryRunId: 7 },
      },
    }));

    expect(useDatabaseStore.getState().consoleState("a").queryRunId).toBe(7);
    expect(useDatabaseStore.getState().consoleState("b").queryRunId).toBe(0);
  });
});

/**
 * A console's text is a file now — written as you type, read back on the
 * next launch. What the store has to get right is the two moments where that
 * can lose work.
 */
describe("consoles that outlive the window", () => {
  beforeEach(() => {
    useDatabaseStore.setState({
      consoles: {},
      profiles: [
        { id: "p1", name: "prod", host: "", port: 0, database: "shop", username: "", sslMode: "", env: "", readOnly: false },
      ],
      selectedProfileId: "p1",
      selectedSchema: "public",
    });
  });

  test("typing stamps the console with where it would run", () => {
    useDatabaseStore.getState().setSql("c1", "SELECT 1");
    const console = useDatabaseStore.getState().consoleState("c1");
    expect(console.originProfileId).toBe("p1");
    expect(console.originDatabase).toBe(useDatabaseStore.getState().profiles.find(p => p.id === "p1")?.database ?? "");
    expect(console.targetProfileId).toBe("p1");
  });

  /*
   * Hydration is a bridge call, and the window renders long before it lands.
   * A file from the last session arriving on top of a query the user has
   * already started typing is the worst way this feature could fail, so an
   * untouched slice is the only one it fills.
   */
  const onDisk = (id: string, sql: string) => ({
    id,
    profileId: "p2",
    database: "analytics",
    sql,
    updatedAt: "2026-09-12T00:00:00Z",
  });

  test("a file never lands on top of something already typed", async () => {
    useDatabaseStore.getState().setSql("c1", "SELECT typed");

    await useDatabaseStore.getState().hydrateConsoles(async () => [onDisk("c1", "SELECT from disk")]);

    expect(useDatabaseStore.getState().consoleState("c1").sql).toBe("SELECT typed");
  });

  test("a file fills a console nobody has touched, and says where it came from", async () => {
    await useDatabaseStore.getState().hydrateConsoles(async () => [onDisk("c9", "SELECT restored")]);

    const console = useDatabaseStore.getState().consoleState("c9");
    expect(console.sql).toBe("SELECT restored");
    expect(console.originProfileId).toBe("p2");
    expect(console.originDatabase).toBe("analytics");
  });

  // There is no banner for this and there should not be: the user did not ask
  // for the read and cannot act on its failure.
  test("consoles that cannot be read leave the window alone", async () => {
    await useDatabaseStore.getState().hydrateConsoles(async () => {
      throw new Error("read-only file system");
    });
    expect(useDatabaseStore.getState().consoles).toEqual({});
  });

  test("closing a console drops its slice", () => {
    useDatabaseStore.getState().setSql("c2", "SELECT 1");
    useDatabaseStore.getState().closeConsole("c2");
    expect(useDatabaseStore.getState().consoles.c2).toBeUndefined();
  });

  /*
   * Closing a tab used to delete the file, which was defensible only while
   * nothing could list them: a console nobody can ever find again is a leak,
   * not a document. The Consoles rail is what changed it, and this is the
   * line that says so — closing releases, only deleting deletes.
   */
  test("closing keeps the file; deleting is its own act", () => {
    const calls: string[] = [];
    const release = consoleFiles.release;
    const forget = consoleFiles.forget;
    consoleFiles.release = (id: string) => calls.push(`release:${id}`);
    consoleFiles.forget = (id: string) => calls.push(`forget:${id}`);
    try {
      useDatabaseStore.getState().closeConsole("c3");
      useDatabaseStore.getState().deleteConsoleFile("c3");
    } finally {
      consoleFiles.release = release;
      consoleFiles.forget = forget;
    }
    expect(calls).toEqual(["release:c3", "forget:c3"]);
  });
});

/**
 * Running one statement out of a script, and remembering how it went.
 *
 * The whole text was going to the driver until now, which is not a thing
 * either driver will run: pgx prepares, so two statements come back as
 * "cannot insert multiple commands into a prepared statement", and MySQL
 * refuses unless the DSN opts into multi-statements — the flag that turns
 * every injection into a stacked one.
 *
 * What is pinned here is the FILING — which statement the outcome is
 * recorded against. Not the outcome itself: whether a run answers in this
 * process depends on what the rest of the suite has done to the bridge, and
 * a test that reads "error" today and "ok" tomorrow is a test of the suite's
 * ordering rather than of this code.
 */
describe("one statement at a time", () => {
  beforeEach(() => {
    useDatabaseStore.setState({
      consoles: {},
      profiles: [
        { id: "p1", name: "prod", host: "", port: 0, database: "shop", username: "", sslMode: "", env: "", readOnly: false },
      ],
      selectedProfileId: "p1",
    });
  });

  test("the outcome is filed under the statement that ran, not the console", async () => {
    await useDatabaseStore.getState().executeQuery("c1", "SELECT 2");

    const console = useDatabaseStore.getState().consoleState("c1");
    expect(Object.keys(console.statementRuns)).toEqual(["SELECT 2"]);
    expect(console.lastStatement, "what the pill is talking about").toBe("SELECT 2");
  });

  test("a second statement adds a mark, it does not replace the first", async () => {
    await useDatabaseStore.getState().executeQuery("c1", "SELECT 1");
    await useDatabaseStore.getState().executeQuery("c1", "SELECT 2");

    expect(Object.keys(useDatabaseStore.getState().consoleState("c1").statementRuns).sort()).toEqual(
      ["SELECT 1", "SELECT 2"],
    );
  });

  test("no statement means the whole console, which is what one statement is", async () => {
    useDatabaseStore.getState().setSql("c2", "SELECT 9");
    await useDatabaseStore.getState().executeQuery("c2");

    expect(useDatabaseStore.getState().consoleState("c2").lastStatement).toBe("SELECT 9");
  });
});

/**
 * A script, top to bottom, stopping at the first statement that fails.
 *
 * The bridge is absent in a test, so the first statement never comes back
 * clean — which is the case worth pinning: the run stops there and the
 * statements below it are left alone. A script that pressed on would run
 * the DELETE that the failed SELECT above it was supposed to justify.
 */
describe("running the whole script", () => {
  const realExecuteQuery = useDatabaseStore.getState().executeQuery;
  afterEach(() => useDatabaseStore.setState({ executeQuery: realExecuteQuery }));

  beforeEach(() => {
    useDatabaseStore.setState({
      consoles: {},
      profiles: [
        { id: "p1", name: "prod", host: "", port: 0, database: "shop", username: "", sslMode: "", env: "", readOnly: false },
      ],
      selectedProfileId: "p1",
    });
  });

  /*
   * Stubbed rather than run: what is being pinned is the SEQUENCING — where
   * the loop stops — and whether a real bridge answers in this process
   * depends on what the rest of the suite has done to it.
   */
  const withVerdicts = (verdict: (sql: string) => RunVerdict) => {
    const calls: string[] = [];
    useDatabaseStore.setState({
      executeQuery: async (_id: string, sql?: string) => {
        calls.push(sql ?? "");
        return verdict(sql ?? "");
      },
    });
    return calls;
  };

  test("it stops at the first statement that fails", async () => {
    const calls = withVerdicts((sql) => (sql === "SELECT 2" ? "error" : "ok"));

    await useDatabaseStore.getState().runScript("c1", ["SELECT 1", "SELECT 2", "SELECT 3"]);

    expect(calls, "the statement after the failure ran anyway").toEqual(["SELECT 1", "SELECT 2"]);
  });

  // Stop is the user saying stop, not the user saying skip this one.
  test("a cancel ends the script, it does not step over it", async () => {
    const calls = withVerdicts((sql) => (sql === "SELECT 1" ? "cancelled" : "ok"));

    await useDatabaseStore.getState().runScript("c1", ["SELECT 1", "SELECT 2"]);

    expect(calls).toEqual(["SELECT 1"]);
  });

  test("and otherwise runs every one, in order", async () => {
    const calls = withVerdicts(() => "ok");

    await useDatabaseStore.getState().runScript("c1", ["SELECT 1", "SELECT 2", "SELECT 3"]);

    expect(calls).toEqual(["SELECT 1", "SELECT 2", "SELECT 3"]);
  });

  /*
   * The marks answer "what happened this time". A script that stops at the
   * second statement must not leave the third wearing a tick from the run
   * before — the honest answer for a statement that never ran is nothing.
   */
  test("last time's marks do not survive into this run", async () => {
    useDatabaseStore.setState({
      consoles: {
        c2: {
          ...useDatabaseStore.getState().consoleState("c2"),
          statementRuns: { "SELECT stale": { outcome: "ok", durationMs: 4 } },
        },
      },
    });

    await useDatabaseStore.getState().runScript("c2", ["SELECT 1"]);

    expect(useDatabaseStore.getState().consoleState("c2").statementRuns["SELECT stale"]).toBeUndefined();
  });

  /*
   * Keep old grids while the next run is pending. executeQuery replaces them
   * on the first success; this sequencing stub never produces a result.
   */
  test("last run's grids stay until the first new result replaces them", async () => {
    withVerdicts(() => "ok");
    useDatabaseStore.setState({
      consoles: {
        c4: {
          ...useDatabaseStore.getState().consoleState("c4"),
          results: [
            {
              statement: "SELECT stale",
              result: { columns: [], rows: [], rowCount: 3, truncated: false },
            },
          ],
          activeResult: 0,
        },
      },
    });

    await useDatabaseStore.getState().runScript("c4", ["SELECT 1"]);

    expect(useDatabaseStore.getState().consoleState("c4").results[0]?.statement).toBe("SELECT stale");
  });

  test("an empty console is not a script", async () => {
    await useDatabaseStore.getState().runScript("c3", []);
    expect(useDatabaseStore.getState().consoles.c3).toBeUndefined();
  });
});

/**
 * Which grid is on screen, when a script produced several.
 *
 * A function and not a field: a field would be the same fact written twice,
 * and the two would disagree the first time one was updated without the
 * other.
 */
describe("the grid on screen", () => {
  const grid = (rowCount: number) => ({ columns: ["n"], rows: [], rowCount, truncated: false });
  const console = (results: { statement: string; result: ReturnType<typeof grid> }[], active: number) => ({
    ...useDatabaseStore.getState().consoleState("anything"),
    results,
    activeResult: active,
  });

  test("is the active one", () => {
    const state = console(
      [
        { statement: "SELECT 1", result: grid(1) },
        { statement: "SELECT 2", result: grid(2) },
      ],
      1,
    );
    expect(visibleResult(state)?.rowCount).toBe(2);
  });

  test("is nothing when the run produced nothing", () => {
    expect(visibleResult(console([], 0))).toBeNull();
  });

  // A script that fails on its third statement leaves the index pointing at
  // a tab that is no longer there when the next run replaces the list.
  test("is nothing when the index has outrun the list", () => {
    expect(visibleResult(console([{ statement: "SELECT 1", result: grid(1) }], 4))).toBeNull();
  });
});


test("clearing a console target does not fall back to Explorer on typing", () => {
  useDatabaseStore.setState({selectedProfileId: "other", consoles: {}});
  const state = useDatabaseStore.getState();
  state.bindConsole("cleared", "");
  state.setSql("cleared", "select 1");
  expect(useDatabaseStore.getState().consoleState("cleared").targetProfileId).toBe("");
});
