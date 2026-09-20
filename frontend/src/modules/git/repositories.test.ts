import { describe, expect, test } from "bun:test";
import type { Repository } from "@/bridge/git";
import { describeRepository, repositoryRoot } from "./repositories";

const repo = (over: Partial<Repository> = {}): Repository => ({
  path: "/work/billing",
  name: "billing",
  branch: "main",
  detached: false,
  ahead: 0,
  behind: 0,
  changed: 0,
  conflicted: 0,
  error: "",
  ...over,
});

describe("describeRepository", () => {
  test("a level, untouched repository is clean", () => {
    expect(describeRepository(repo())).toEqual({ branch: "main", state: "clean", sync: "", attention: false });
  });

  test("changes are counted, and one is singular", () => {
    expect(describeRepository(repo({ changed: 1 })).state).toBe("1 change");
    expect(describeRepository(repo({ changed: 3 })).state).toBe("3 changes");
  });

  test("ahead and behind read as arrows, and only the ones there are", () => {
    expect(describeRepository(repo({ ahead: 2, behind: 1 })).sync).toBe("↑2 ↓1");
    expect(describeRepository(repo({ behind: 4 })).sync).toBe("↓4");
  });

  test("unmerged files outrank the plain change count and need attention", () => {
    const line = describeRepository(repo({ changed: 5, conflicted: 2 }));
    expect(line.state).toBe("2 conflicted");
    expect(line.attention).toBe(true);
  });

  test("a detached HEAD and a repository with no commits say so", () => {
    expect(describeRepository(repo({ detached: true, branch: "" })).branch).toBe("detached HEAD");
    expect(describeRepository(repo({ branch: "" })).branch).toBe("no commits");
  });

  test("a repository that could not be read says that and needs attention", () => {
    expect(describeRepository(repo({ error: "boom" }))).toEqual({
      branch: "",
      state: "cannot read",
      sync: "",
      attention: true,
    });
  });
});

describe("repositoryRoot", () => {
  test("is the folder the app has open, even while Git is pinned inside it", () => {
    expect(repositoryRoot("/work/CHIRO", "/work/CHIRO/billing")).toBe("/work/CHIRO");
  });

  test("falls back to Git's own folder when nothing is open", () => {
    expect(repositoryRoot("", "/elsewhere/repo")).toBe("/elsewhere/repo");
  });
});
