import { describe, expect, it } from "bun:test";
import { indexedStatus } from "./CodeGraphView";

describe("indexedStatus", () => {
  it("names the module count for a repository holding several", () => {
    // The monorepo case: without this, four services read as one project with
    // a lot of endpoints.
    expect(indexedStatus(3, 12)).toBe("Indexed. 3 modules · 12 endpoint(s) found.");
  });

  it("says nothing about modules for an ordinary single project", () => {
    // "1 module" is noise on the common case.
    expect(indexedStatus(1, 5)).toBe("Indexed. 5 endpoint(s) found.");
    expect(indexedStatus(0, 0)).toBe("Indexed. 0 endpoint(s) found.");
  });
});
