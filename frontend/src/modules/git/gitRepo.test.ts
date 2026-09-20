import { describe, expect, test } from "bun:test";
import type { GitStatus, Repository } from "@/bridge/git";
import { createGitRepo, type GitReads } from "./gitRepo";

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

/** Reads that count their calls and answer when told, so a test decides the order things land in. */
function fake(branch = "main") {
  const calls = { status: 0, repositories: 0 };
  const waiting: Array<(value: GitStatus) => void> = [];
  const reads: GitReads = {
    status: () => {
      calls.status++;
      return new Promise((resolve) => waiting.push(resolve));
    },
    log: async () => [],
    guards: async () => [],
    remotes: async () => [],
    repositories: async () => {
      calls.repositories++;
      return [{ path: "/w/a", name: "a" } as Repository];
    },
  };
  const answer = (value: GitStatus = status(branch)) => waiting.shift()?.(value);
  return { reads, calls, answer };
}

const settle = () => new Promise((resolve) => setTimeout(resolve, 0));

describe("refresh", () => {
  test("two callers with the same stamp share one read", async () => {
    const { reads, calls, answer } = fake();
    const git = createGitRepo(reads);
    const a = git.refresh("/w/a", "1");
    const b = git.refresh("/w/a", "1");
    await settle();
    answer();
    await Promise.all([a, b]);
    expect(calls.status).toBe(1);
    expect(git.useStore.getState().byFolder["/w/a"]?.snapshot?.status.branch).toBe("main");
  });

  test("a different stamp is a different read", async () => {
    const { reads, calls, answer } = fake();
    const git = createGitRepo(reads);
    const a = git.refresh("/w/a", "1");
    const b = git.refresh("/w/a", "2");
    await settle();
    answer();
    answer();
    await Promise.all([a, b]);
    expect(calls.status).toBe(2);
  });

  test("an answer that lands after a later read was asked is dropped", async () => {
    const { reads, answer } = fake();
    const git = createGitRepo(reads);
    const first = git.refresh("/w/a", "1");
    const second = git.refresh("/w/b", "1");
    await settle();
    // The later read is answered first, then the older one arrives.
    const older = status("old");
    const newer = status("new");
    answer(older); // the queue is in asking order: this is the first read's answer
    answer(newer);
    await Promise.all([first, second]);
    expect(git.useStore.getState().byFolder["/w/a"]).toBeUndefined();
    expect(git.useStore.getState().byFolder["/w/b"]?.snapshot?.status.branch).toBe("new");
  });

  test("a read that finds nothing changed leaves the state object alone", async () => {
    const { reads, answer } = fake();
    const git = createGitRepo(reads);
    const first = git.refresh("/w/a", "1");
    await settle();
    answer();
    await first;
    const before = git.useStore.getState();
    const again = git.refresh("/w/a", "2");
    await settle();
    answer();
    await again;
    expect(git.useStore.getState()).toBe(before);
  });

  test("a failed read keeps what was read before and says why", async () => {
    let fail = false;
    const { reads } = fake();
    const git = createGitRepo({
      ...reads,
      status: async () => {
        if (fail) throw new Error("index locked");
        return status("main");
      },
    });
    await git.refresh("/w/a", "1");
    fail = true;
    await git.refresh("/w/a", "2");
    const entry = git.useStore.getState().byFolder["/w/a"];
    expect(entry?.error).toBe("index locked");
    expect(entry?.snapshot?.status.branch).toBe("main");
  });

  test("forgets the oldest folder past the limit, not the one just used", async () => {
    const git = createGitRepo({ ...fake().reads, status: async () => status("main") });
    for (let i = 0; i < 52; i++) await git.refresh(`/w/${i}`, "1");
    const keys = Object.keys(git.useStore.getState().byFolder);
    expect(keys).toHaveLength(50);
    expect(keys).not.toContain("/w/0");
    expect(keys).toContain("/w/51");
  });
});

describe("refreshRepositories", () => {
  test("a scan already running is shared, and marks the root as scanning until it lands", async () => {
    const { reads, calls } = fake();
    const git = createGitRepo(reads);
    const a = git.refreshRepositories("/w");
    const b = git.refreshRepositories("/w");
    expect(git.useStore.getState().scanning["/w"]).toBe(true);
    await Promise.all([a, b]);
    expect(calls.repositories).toBe(1);
    expect(git.useStore.getState().scanning["/w"]).toBeUndefined();
    expect(git.useStore.getState().reposByRoot["/w"]?.repos).toHaveLength(1);
  });

  test("force reads again", async () => {
    const { reads, calls } = fake();
    const git = createGitRepo(reads);
    const a = git.refreshRepositories("/w");
    const b = git.refreshRepositories("/w", true);
    await Promise.all([a, b]);
    expect(calls.repositories).toBe(2);
  });
});
