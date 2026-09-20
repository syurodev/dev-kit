import { describe, expect, test } from "bun:test";
import { effectiveFolder } from "./workFolder";

describe("effectiveFolder", () => {
  test("an unpinned module works in the current folder", () => {
    expect(effectiveFolder({ current: "/work/a", pins: {} }, "git")).toBe("/work/a");
  });

  test("a pinned module stays on its own folder while the current one differs", () => {
    const state = { current: "/work/b", pins: { git: "/work/a" } };
    expect(effectiveFolder(state, "git")).toBe("/work/a");
    expect(effectiveFolder(state, "agenthub")).toBe("/work/b");
  });

  test("no folder open is the empty string, not undefined", () => {
    expect(effectiveFolder({ current: "", pins: {} }, "tasks")).toBe("");
  });
});
