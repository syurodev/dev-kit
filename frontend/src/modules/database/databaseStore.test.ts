import { beforeEach, describe, expect, test } from "bun:test";
import { useDatabaseStore } from "./databaseStore";

beforeEach(() => {
  useDatabaseStore.setState({ consoles: {} });
});

describe("console slices", () => {
  test("a console that was never opened reads as empty, not undefined", () => {
    const console = useDatabaseStore.getState().consoleState("never-opened");
    expect(console.sql).toBe("");
    expect(console.queryResult).toBeNull();
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
        a: { ...s.consoleState("a"), queryResult: { columns: ["n"], rows: [["1"]], rowCount: 1, truncated: false } },
      },
    }));
    expect(useDatabaseStore.getState().consoleState("b").queryResult).toBeNull();
    expect(useDatabaseStore.getState().consoleState("a").queryResult?.rowCount).toBe(1);
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
    expect(console.originDatabase).toBe("public");
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
});
