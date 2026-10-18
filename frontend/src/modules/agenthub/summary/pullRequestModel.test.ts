import { describe, expect, test } from "bun:test";

import type { ChangeRequest, PullRequestItem } from "@/bridge/pullRequests";
import { folderName, hasPullRequests, pullRequestsHint, pullRequestsView, sharedMessage } from "./pullRequestModel";

// The generated types are string enums; a test writes the strings the wire carries.
type Extra = { [K in keyof ChangeRequest]?: unknown };

function cr(extra: Extra = {}): ChangeRequest {
  return {
    repo: { host: "github.com", path: "acme/widgets" },
    number: 7,
    title: "Add retries",
    url: "https://github.com/acme/widgets/pull/7",
    state: "open",
    draft: false,
    head: "feat/retries",
    base: "main",
    additions: 120,
    deletions: 14,
    changedFiles: 5,
    mergeable: "clean",
    reviewDecision: "",
    checks: { total: 4, passed: 1, failed: 1, running: 2, skipped: 0 },
    checksKnown: true,
    unresolvedThreads: 2,
    updatedAt: "2026-10-16T10:00:00Z",
    terms: { platform: "GitHub", short: "PR", long: "pull request", checks: "checks" },
    ...extra,
  } as ChangeRequest;
}

function found(change: ChangeRequest, extra: { [K in keyof PullRequestItem]?: unknown } = {}): PullRequestItem {
  return { folder: "/w/widgets", branch: change.head, status: "found", source: "branch", changeRequest: change, ...extra } as PullRequestItem;
}

describe("pullRequestsView", () => {
  test("a found request becomes a card with size, checks progress and threads", () => {
    const view = pullRequestsView([
      found(
        cr({
          checks: {
            total: 4, passed: 1, failed: 1, running: 2, skipped: 0,
            failing: [{ name: "unit", status: "failed", url: "https://ci/unit", summary: "2 tests failed" }],
          } as ChangeRequest["checks"],
        }),
      ),
    ]);
    expect(view.cards).toHaveLength(1);
    const card = view.cards[0];
    expect(card).toMatchObject({ number: 7, short: "PR", state: "open", added: 120, removed: 14, files: 5, unresolved: 2, repo: "acme/widgets" });
    expect(card.checks).toMatchObject({ label: "1/4", tone: "danger", failed: 1, running: 2 });
    expect(card.checks?.title).toBe("checks: 1 passed, 1 failing, 2 running");
    expect(card.failing).toEqual([{ name: "unit", url: "https://ci/unit", summary: "2 tests failed" }]);
    expect(card.failingMore).toBe(0);
  });

  test("checks tone: running beats nothing, failing beats running, all passed is success", () => {
    const tone = (checks: Partial<ChangeRequest["checks"]>) =>
      pullRequestsView([found(cr({ checks: { total: 3, passed: 0, failed: 0, running: 0, skipped: 0, ...checks } as ChangeRequest["checks"] }))]).cards[0].checks?.tone;
    expect(tone({ passed: 3 })).toBe("success");
    expect(tone({ passed: 2, running: 1 })).toBe("running");
    expect(tone({ passed: 1, running: 1, failed: 1 })).toBe("danger");
  });

  test("no checks, or checks that could not be read, show no progress rather than 0/0", () => {
    expect(pullRequestsView([found(cr({ checks: { total: 0, passed: 0, failed: 0, running: 0, skipped: 0 } as ChangeRequest["checks"] }))]).cards[0].checks).toBeNull();
    const unread = pullRequestsView([found(cr({ checksKnown: false, notes: ["checks unavailable: forbidden"] }))]).cards[0];
    expect(unread.checks).toBeNull();
    expect(unread.notes).toEqual(["checks unavailable: forbidden"]);
  });

  test("only three failing checks are listed and the rest are counted", () => {
    const failing = ["a", "b", "c", "d"].map((name) => ({ name, status: "failed" })) as unknown as NonNullable<ChangeRequest["checks"]["failing"]>;
    const card = pullRequestsView([found(cr({ checks: { total: 6, passed: 2, failed: 4, running: 0, skipped: 0, failing } as ChangeRequest["checks"] }))]).cards[0];
    expect(card.failing.map((f) => f.name)).toEqual(["a", "b", "c"]);
    expect(card.failingMore).toBe(1);
  });

  test("a draft, a merged and a closed request each have their own state", () => {
    const states = (change: Extra) => pullRequestsView([found(cr(change))]).cards[0].state;
    expect(states({ draft: true })).toBe("draft");
    expect(states({ state: "merged" })).toBe("merged");
    expect(states({ state: "closed" })).toBe("closed");
  });

  test("a finished request keeps its size and drops what only matters while open", () => {
    const card = pullRequestsView([found(cr({ state: "merged", mergeable: "conflicting", reviewDecision: "changes_requested" }))]).cards[0];
    expect(card).toMatchObject({ state: "merged", added: 120, removed: 14, checks: null, merge: null, review: null, unresolved: 0, failing: [], failingMore: 0 });
  });

  test("mergeability and review only speak when they need attention", () => {
    const of = (change: Extra) => pullRequestsView([found(cr(change))]).cards[0];
    expect(of({ mergeable: "clean" }).merge).toBeNull();
    expect(of({ mergeable: "unknown" }).merge).toBeNull();
    expect(of({ mergeable: "conflicting" }).merge).toEqual({ text: "Conflicts with main", tone: "danger" });
    expect(of({ mergeable: "behind" }).merge).toEqual({ text: "Behind main", tone: "warning" });
    expect(of({ mergeable: "blocked" }).merge).toEqual({ text: "Blocked by a rule", tone: "warning" });
    expect(of({ mergeable: "blocked", draft: true }).merge).toBeNull();
    expect(of({ reviewDecision: "approved" }).review).toEqual({ text: "Approved", tone: "success" });
    expect(of({ reviewDecision: "changes_requested" }).review).toEqual({ text: "Changes requested", tone: "danger" });
    expect(of({ reviewDecision: "" }).review).toBeNull();
    expect(of({ unresolvedThreads: -1 }).unresolved).toBe(0);
  });

  test("the platform's own words reach the card: a merge request and pipelines", () => {
    const card = pullRequestsView([found(cr({ terms: { platform: "GitLab", short: "MR", long: "merge request", checks: "pipelines" } }))]).cards[0];
    expect(card.short).toBe("MR");
    expect(card.checks?.title.startsWith("pipelines:")).toBe(true);
  });

  test("several repositories are several cards, open ones first and newest first", () => {
    const view = pullRequestsView([
      found(cr({ number: 3, state: "merged" }), { folder: "/w/a" }),
      found(cr({ number: 5, repo: { host: "github.com", path: "acme/api" } }), { folder: "/w/api" }),
      found(cr({ number: 9 }), { folder: "/w/widgets" }),
    ]);
    expect(view.cards.map((c) => c.number)).toEqual([9, 5, 3]);
    expect(new Set(view.cards.map((c) => c.key)).size).toBe(3);
  });

  test("attached requests say so", () => {
    expect(pullRequestsView([found(cr(), { source: "attached" })]).cards[0].source).toBe("attached");
  });
});

describe("notices", () => {
  const item = (status: string, message: string, folder = "/w/widgets") => ({ folder, branch: "x", status, message, source: "branch" }) as PullRequestItem;

  test("a missing account is stated, with its reason", () => {
    const view = pullRequestsView([item("no_account", "No GitHub account for this remote (acme/widgets on github.com). Add a token in Accounts.")]);
    expect(view.cards).toHaveLength(0);
    expect(view.notices).toEqual([
      expect.objectContaining({
        status: "no_account",
        tone: "warning",
        message: expect.stringContaining("No GitHub account"),
        folders: [expect.objectContaining({ name: "widgets" })],
      }),
    ]);
    expect(hasPullRequests(view)).toBe(true);
  });

  test("every reason a person can act on is shown", () => {
    for (const status of ["no_account", "ambiguous_account", "unauthorized", "forbidden", "not_found", "rate_limited", "unavailable"]) {
      expect(pullRequestsView([item(status, `because ${status}`)]).notices).toHaveLength(1);
    }
  });

  test("states that are not news stay quiet", () => {
    for (const status of ["none", "detached", "default_branch", "not_repository", "unsupported_host"]) {
      const view = pullRequestsView([item(status, "whatever")]);
      expect(view).toEqual({ cards: [], notices: [] });
      expect(hasPullRequests(view)).toBe(false);
    }
  });

  test("two folders failing for one reason are one notice, and what needs a person comes first", () => {
    const view = pullRequestsView([item("no_account", "same", "/w/a"), item("no_account", "same", "/w/b"), item("unauthorized", "other", "/w/c")]);
    expect(view.notices.map((n) => n.status)).toEqual(["unauthorized", "no_account"]);
    expect(view.notices[1].folders.map((f) => f.name)).toEqual(["a", "b"]);
  });

  test("the repository is taken out of a sentence so refusals for different repositories group", () => {
    expect(sharedMessage("No GitLab account for this remote (a/b on gitlab.com). Add a token in Accounts.")).toBe(
      "No GitLab account for this remote. Add a token in Accounts.",
    );
    const view = pullRequestsView(
      ["his-connector", "db-migrations", "workload"].map((name) =>
        item("no_account", `No GitLab account for this remote (chiro/${name} on gitlab.com). Add a token in Accounts.`, `/w/${name}`),
      ),
    );
    expect(view.notices).toHaveLength(1);
    expect(view.notices[0]).toMatchObject({ status: "no_account", message: "No GitLab account for this remote. Add a token in Accounts.", action: { label: "Add token", target: "accounts" } });
    expect(view.notices[0].folders.map((f) => f.name)).toEqual(["his-connector", "db-migrations", "workload"]);
    // Each folder keeps its own sentence for the tooltip.
    expect(view.notices[0].folders[0].message).toContain("chiro/his-connector");
  });

  test("one folder listed twice (a branch and an attachment) is counted once", () => {
    const view = pullRequestsView([item("no_account", "same", "/w/a"), item("no_account", "same", "/w/a")]);
    expect(view.notices[0].folders).toHaveLength(1);
  });

  test("only what a person can fix offers an action", () => {
    const action = (status: string) => pullRequestsView([item(status, "x")]).notices[0].action?.label;
    expect(action("no_account")).toBe("Add token");
    expect(action("unauthorized")).toBe("Fix token");
    expect(action("rate_limited")).toBeUndefined();
    expect(action("unavailable")).toBeUndefined();
  });

  test("the folded section's line counts cards and refusals", () => {
    const refused = (n: number) => pullRequestsView(Array.from({ length: n }, (_, i) => item("no_account", "m", `/w/${i}`)));
    expect(pullRequestsHint(refused(6))).toBe("6 need an account");
    expect(pullRequestsHint(refused(1))).toBe("1 needs an account");
    expect(pullRequestsHint(pullRequestsView([found(cr()), found(cr({ number: 2, state: "merged" }))]))).toBe("1 open");
    expect(pullRequestsHint(pullRequestsView([found(cr({ state: "merged" }))]))).toBe("1 finished");
    expect(pullRequestsHint(pullRequestsView([]))).toBeUndefined();
  });

  test("a notice with no message is not drawn", () => {
    expect(pullRequestsView([item("no_account", "")]).notices).toHaveLength(0);
  });

  test("a null or empty list is an empty view", () => {
    expect(pullRequestsView(null)).toEqual({ cards: [], notices: [] });
    expect(pullRequestsView(undefined)).toEqual({ cards: [], notices: [] });
  });
});

test("folderName", () => {
  expect(folderName("/w/widgets/")).toBe("widgets");
  expect(folderName("C:\\work\\api")).toBe("api");
  expect(folderName("solo")).toBe("solo");
});
