import { describe, expect, it } from "bun:test";
import { matchesBranch } from "./GitChangesView";

describe("matchesBranch", () => {
  it("matches an exact branch name", () => {
    expect(matchesBranch("main", "main")).toBe(true);
    expect(matchesBranch("main", "maintenance")).toBe(false);
  });

  it("matches a glob the way the backend's path.Match does", () => {
    expect(matchesBranch("release/*", "release/2026-09")).toBe(true);
    expect(matchesBranch("release/*", "main")).toBe(false);
  });

  it("treats an empty pattern or branch as no match", () => {
    // A detached HEAD has no branch name, and a guard with no pattern is not
    // a guard — neither may light up the badge.
    expect(matchesBranch("", "main")).toBe(false);
    expect(matchesBranch("main", "")).toBe(false);
  });
});
