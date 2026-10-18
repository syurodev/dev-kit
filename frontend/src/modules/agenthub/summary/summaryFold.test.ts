import { afterEach, beforeEach, describe, expect, test } from "bun:test";

import { resetSummaryFold, setSummarySectionOpen } from "./summaryFold";

const KEY = "devkit.agenthub.summary.opened";

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
  test("an opened section is remembered in storage, and folding it forgets it", () => {
    const storage = fakeStorage();
    globals.localStorage = storage;
    setSummarySectionOpen("environment", true);
    setSummarySectionOpen("connectors", true);
    expect(JSON.parse(storage.data.get(KEY) ?? "[]")).toEqual(["environment", "connectors"]);
    setSummarySectionOpen("environment", false);
    expect(JSON.parse(storage.data.get(KEY) ?? "[]")).toEqual(["connectors"]);
  });

  test("what was opened last time is open now, and nothing else", () => {
    const storage = fakeStorage('["plan"]');
    globals.localStorage = storage;
    resetSummaryFold();
    setSummarySectionOpen("goal", true);
    expect(JSON.parse(storage.data.get(KEY) ?? "[]")).toEqual(["plan", "goal"]);
  });

  test("the list the old build kept (what was folded) is not read as what is open", () => {
    const storage = fakeStorage();
    storage.data.set("devkit.agenthub.summary.closed", '["plan"]');
    globals.localStorage = storage;
    resetSummaryFold();
    setSummarySectionOpen("goal", true);
    expect(JSON.parse(storage.data.get(KEY) ?? "[]")).toEqual(["goal"]);
  });

  test("junk or missing storage starts everything folded and never throws", () => {
    globals.localStorage = fakeStorage("{not json");
    resetSummaryFold();
    expect(() => setSummarySectionOpen("plan", true)).not.toThrow();
    globals.localStorage = undefined;
    resetSummaryFold();
    expect(() => setSummarySectionOpen("plan", true)).not.toThrow();
  });

  test("storage that refuses a write still opens for this window", () => {
    globals.localStorage = {
      getItem: () => null,
      setItem: () => {
        throw new Error("quota");
      },
    };
    expect(() => setSummarySectionOpen("plan", true)).not.toThrow();
  });
});
