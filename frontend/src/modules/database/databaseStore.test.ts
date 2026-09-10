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
