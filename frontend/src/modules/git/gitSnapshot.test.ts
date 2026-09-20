import { describe, expect, test } from "bun:test";
import type { GitStatus } from "@/bridge/git";
import { recallSnapshot, rememberSnapshot } from "./gitSnapshot";

const status = (branch: string): GitStatus => ({
  branch,
  upstream: "",
  ahead: 0,
  behind: 0,
  detached: false,
  files: {},
  staged: null,
  unstaged: null,
  untracked: null,
  conflicted: null,
});

describe("gitSnapshot", () => {
  test("remembers what a folder showed, and only for that folder", () => {
    rememberSnapshot("/w/a", { status: status("dev"), guards: [], remotes: null });
    expect(recallSnapshot("/w/a")?.status.branch).toBe("dev");
    expect(recallSnapshot("/w/b")).toBeNull();
  });

  test("a later read replaces the earlier one", () => {
    rememberSnapshot("/w/c", { status: status("main"), guards: [], remotes: null });
    rememberSnapshot("/w/c", { status: status("dev"), guards: [], remotes: [] });
    expect(recallSnapshot("/w/c")?.status.branch).toBe("dev");
  });

  test("an empty folder is not a key", () => {
    rememberSnapshot("", { status: status("dev"), guards: [], remotes: null });
    expect(recallSnapshot("")).toBeNull();
  });
});
