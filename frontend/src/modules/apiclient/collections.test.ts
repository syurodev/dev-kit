import { describe, expect, it } from "bun:test";
import { byFolderThenName, folderPath, foldersByPath, requestLabel } from "./collections";
import type { ApiCollection } from "@/bridge/apiclient";

const TREE: ApiCollection[] = [
  { id: "a", name: "GitHub", order: 0 },
  { id: "b", name: "Repos", parentId: "a", order: 0 },
  { id: "c", name: "Issues", parentId: "b", order: 0 },
  { id: "z", name: "Stripe", order: 0 },
];

describe("folderPath", () => {
  it("reads outermost first", () => {
    expect(folderPath(TREE, "c")).toBe("GitHub / Repos / Issues");
    expect(folderPath(TREE, "a")).toBe("GitHub");
    expect(folderPath(TREE, "")).toBe("");
  });

  it("stops at a parent that is not here rather than hiding the folder", () => {
    // A folder can arrive before its parent, or after the parent was deleted
    // on another device. Showing it un-nested is right; dropping it is not.
    const orphan: ApiCollection[] = [{ id: "x", name: "Orphan", parentId: "gone", order: 0 }];
    expect(folderPath(orphan, "x")).toBe("Orphan");
  });

  it("terminates on a cycle instead of hanging", () => {
    // The backend refuses to create one. This walks the tree on every render,
    // and a walk that trusts its input is a hang rather than a wrong label.
    const cycle: ApiCollection[] = [
      { id: "1", name: "One", parentId: "2", order: 0 },
      { id: "2", name: "Two", parentId: "1", order: 0 },
    ];
    expect(folderPath(cycle, "1")).toBe("Two / One");
  });
});

describe("requestLabel", () => {
  it("prefixes the folder path", () => {
    expect(requestLabel(TREE, { name: "List repos", collectionId: "b" })).toBe(
      "GitHub / Repos / List repos",
    );
  });

  it("leaves an unfiled request as its own name, with no leading separator", () => {
    expect(requestLabel(TREE, { name: "Ping", collectionId: "" })).toBe("Ping");
  });
});

describe("byFolderThenName", () => {
  it("groups by folder and then sorts by name", () => {
    const sorted = byFolderThenName(TREE, [
      { name: "Zebra", collectionId: "" },
      { name: "List repos", collectionId: "b" },
      { name: "Apple", collectionId: "" },
      { name: "Create repo", collectionId: "b" },
    ]);
    expect(sorted.map((request) => request.name)).toEqual([
      // Unfiled first — its path is "", which sorts before every folder — so
      // it is never hidden at the bottom under a blank heading.
      "Apple",
      "Zebra",
      "Create repo",
      "List repos",
    ]);
  });
});

describe("foldersByPath", () => {
  it("lists children directly after their parents", () => {
    // Sorted by full path, so "GitHub" precedes "GitHub / Repos" which
    // precedes "GitHub / Repos / Issues" — nesting order, not name order.
    expect(foldersByPath(TREE).map((folder) => folder.name)).toEqual([
      "GitHub",
      "Repos",
      "Issues",
      "Stripe",
    ]);
  });
});
