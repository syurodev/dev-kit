import { beforeEach, describe, expect, test } from "bun:test";
import type { StoredConsole } from "@/bridge/database";
import { useDatabaseStore } from "./databaseStore";
import { ensureConsolesHydrated, resetConsoleHydration } from "./consoleHydration";

const onDisk = (id: string, sql: string): StoredConsole => ({ id, profileId: "", database: "", sql, updatedAt: "" });

beforeEach(() => {
  resetConsoleHydration();
  useDatabaseStore.setState({ consoles: {} });
});

describe("reading the console files back, once", () => {
  test("the rail and an Open in Database share one read", async () => {
    let reads = 0;
    const load = async () => {
      reads += 1;
      return [onDisk("c1", "select from disk")];
    };
    await Promise.all([ensureConsolesHydrated(load), ensureConsolesHydrated(load)]);
    await ensureConsolesHydrated(load);
    expect(reads).toBe(1);
    expect(useDatabaseStore.getState().consoleState("c1").sql).toBe("select from disk");
  });

  test("a read that failed is tried again by the next caller", async () => {
    let reads = 0;
    await ensureConsolesHydrated(async () => {
      reads += 1;
      throw new Error("not yet");
    });
    await ensureConsolesHydrated(async () => {
      reads += 1;
      return [onDisk("c2", "select 2")];
    });
    expect(reads).toBe(2);
    expect(useDatabaseStore.getState().consoleState("c2").sql).toBe("select 2");
  });
});
