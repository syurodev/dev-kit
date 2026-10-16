import { afterEach, beforeEach, describe, expect, test } from "bun:test";

import { resetSummaryFold, setSummarySectionOpen } from "./summaryFold";

const KEY = "devkit.agenthub.summary.closed";

function fakeStorage(initial?: string) {
  const data = new Map<string, string>(initial === undefined ? [] : [[KEY, initial]]);
  return {
    data,
    getItem: (k: string) => data.get(k) ?? null,
    setItem: (k: string, v: string) => void data.set(k, v),
  };
}

const globals = globalThis as unknown as { localStorage?: unknown };
const original = globals.localStorage;

beforeEach(() => resetSummaryFold());
afterEach(() => {
  globals.localStorage = original;
  resetSummaryFold();
});

describe("summaryFold", () => {
  test("a folded section is remembered in storage, and opening it forgets it", () => {
    const storage = fakeStorage();
    globals.localStorage = storage;
    setSummarySectionOpen("environment", false);
    setSummarySectionOpen("connectors", false);
    expect(JSON.parse(storage.data.get(KEY) ?? "[]")).toEqual(["environment", "connectors"]);
    setSummarySectionOpen("environment", true);
    expect(JSON.parse(storage.data.get(KEY) ?? "[]")).toEqual(["connectors"]);
  });

  test("what was folded last time is folded now", () => {
    const storage = fakeStorage('["plan"]');
    globals.localStorage = storage;
    resetSummaryFold();
    setSummarySectionOpen("goal", false);
    expect(JSON.parse(storage.data.get(KEY) ?? "[]")).toEqual(["plan", "goal"]);
  });

  test("junk or missing storage starts everything open and never throws", () => {
    globals.localStorage = fakeStorage("{not json");
    resetSummaryFold();
    expect(() => setSummarySectionOpen("plan", false)).not.toThrow();
    globals.localStorage = undefined;
    resetSummaryFold();
    expect(() => setSummarySectionOpen("plan", true)).not.toThrow();
  });

  test("storage that refuses a write still folds for this window", () => {
    globals.localStorage = {
      getItem: () => null,
      setItem: () => {
        throw new Error("quota");
      },
    };
    expect(() => setSummarySectionOpen("plan", false)).not.toThrow();
  });
});
