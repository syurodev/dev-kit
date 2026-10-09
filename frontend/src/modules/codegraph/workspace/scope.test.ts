import { describe, expect, it } from "bun:test";
import { normalizeScope, scopeFolderIds, scopeSummary, scopesEqual, toggleScopeFolder, WHOLE_WORKSPACE } from "./scope";

const folders = [
  { id: "a", name: "api", path: "/w/api" },
  { id: "b", name: "web", path: "/w/web" },
  { id: "c", name: "lib", path: "/w/lib" },
];
const labels = { a: "api", b: "web", c: "lib" };

describe("discovery scope", () => {
  it("whole workspace follows the folder list", () => {
    expect(scopeFolderIds(WHOLE_WORKSPACE, folders)).toEqual(["a", "b", "c"]);
    expect(scopeFolderIds(WHOLE_WORKSPACE, folders.slice(0, 2))).toEqual(["a", "b"]);
  });

  it("a subset keeps the workspace's order, not the order it was picked in", () => {
    expect(scopeFolderIds({ kind: "folders", ids: ["c", "a"] }, folders)).toEqual(["a", "c"]);
  });

  it("drops folders that left, and falls back to the whole workspace rather than an empty view", () => {
    expect(normalizeScope({ kind: "folders", ids: ["a", "gone"] }, folders)).toEqual({ kind: "folders", ids: ["a"] });
    expect(normalizeScope({ kind: "folders", ids: ["gone"] }, folders)).toEqual(WHOLE_WORKSPACE);
    expect(normalizeScope({ kind: "folders", ids: ["a", "b", "c"] }, folders)).toEqual(WHOLE_WORKSPACE);
  });

  it("toggling from the whole workspace narrows it; toggling the last one back widens", () => {
    const narrowed = toggleScopeFolder(WHOLE_WORKSPACE, "b", folders);
    expect(narrowed).toEqual({ kind: "folders", ids: ["a", "c"] });
    expect(toggleScopeFolder(narrowed, "b", folders)).toEqual(WHOLE_WORKSPACE);
    // Removing the only member does not leave an empty scope.
    expect(toggleScopeFolder({ kind: "folders", ids: ["a"] }, "a", folders)).toEqual(WHOLE_WORKSPACE);
  });

  it("says what it covers", () => {
    expect(scopeSummary(WHOLE_WORKSPACE, folders, labels)).toBe("Whole workspace · 3 folders");
    expect(scopeSummary({ kind: "folders", ids: ["a"] }, folders, labels)).toBe("Viewing: api");
    expect(scopeSummary({ kind: "folders", ids: ["a", "b"] }, folders, labels)).toBe("Viewing: api, web");
    expect(scopeSummary(WHOLE_WORKSPACE, folders.slice(0, 1), labels)).toBe("Whole workspace · 1 folder");
  });

  it("compares by value", () => {
    expect(scopesEqual({ kind: "folders", ids: ["a"] }, { kind: "folders", ids: ["a"] })).toBe(true);
    expect(scopesEqual(WHOLE_WORKSPACE, { kind: "folders", ids: ["a"] })).toBe(false);
  });
});
