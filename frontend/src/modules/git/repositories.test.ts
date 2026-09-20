import { describe, expect, test } from "bun:test";
import type { Repository } from "@/bridge/git";
import { describeRepository, folderStatus, pathTail, repositoryRoot } from "./repositories";

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

describe("pathTail", () => {
  test("keeps the last segments and none of the way there", () => {
    expect(pathTail("/Users/me/CHIRO/log-service", 2)).toBe("CHIRO/log-service");
    expect(pathTail("/log-service", 2)).toBe("log-service");
    expect(pathTail("/Users/me/CHIRO/", 1)).toBe("CHIRO");
  });
});

describe("folderStatus", () => {
  const base = { folder: "/w/CHIRO/log", current: "/w/CHIRO/log", pinned: false, project: "/w/CHIRO/log", repos: null };

  test("following says whose folder it is following, and where that is", () => {
    expect(folderStatus(base)).toEqual({ tone: "success", text: "Following Code Graph · CHIRO/log" });
  });

  test("a folder that is no repository says so before anything else", () => {
    expect(folderStatus({ ...base, folder: "/w/CHIRO", current: "/w/CHIRO", project: "" })).toEqual({
      tone: "warning",
      text: "Not a repository · following Code Graph",
    });
  });

  test("pinned names what Code Graph is doing, and whether that is a repository", () => {
    const pinned = { ...base, pinned: true, folder: "/w/CHIRO/his", current: "/w/CHIRO" };
    expect(folderStatus({ ...pinned, repos: [] })).toEqual({ tone: "info", text: "Pinned · CHIRO is not a repository" });
    expect(folderStatus({ ...pinned, repos: [repo({ path: "/w/CHIRO" })] })).toEqual({
      tone: "info",
      text: "Pinned · Code Graph is in CHIRO",
    });
    expect(folderStatus({ ...pinned, current: "" })).toEqual({
      tone: "info",
      text: "Pinned · Code Graph has no project open",
    });
  });
});
