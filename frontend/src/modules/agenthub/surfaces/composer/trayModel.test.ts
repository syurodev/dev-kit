import { describe, expect, it } from "bun:test";
import { chooseTrayPanel, repoSummary, type SummarisedRepo } from "./trayModel";

const none = {
  permission: false,
  permissionFolded: false,
  question: false,
  questionFolded: false,
  opened: null,
  runningSubagents: 0,
  hasBoards: false,
  repoCount: 0,
  slowCommands: false,
} as const;

describe("which panel the composer shows", () => {
  it("is none until something asks for one", () => {
    expect(chooseTrayPanel(none)).toBeNull();
  });

  it("puts a permission before a question before anything the person opened", () => {
    const opened = { ...none, opened: "repos" as const, repoCount: 2 };
    expect(chooseTrayPanel(opened)).toBe("repos");
    expect(chooseTrayPanel({ ...opened, question: true })).toBe("question");
    expect(chooseTrayPanel({ ...opened, question: true, permission: true })).toBe("permission");
  });

  it("lets a folded request step aside — the next one in line comes forward, the list does not", () => {
    const both = { ...none, permission: true, question: true };
    expect(chooseTrayPanel({ ...both, permissionFolded: true })).toBe("question");
    // Both folded to read the transcript: the person's own open list is what shows.
    expect(chooseTrayPanel({ ...both, permissionFolded: true, questionFolded: true, opened: "tasks", hasBoards: true })).toBe("tasks");
    expect(chooseTrayPanel({ ...both, permissionFolded: true, questionFolded: true })).toBeNull();
  });

  it("closes an opened list whose subject has gone, instead of showing it empty", () => {
    expect(chooseTrayPanel({ ...none, opened: "subagents", runningSubagents: 2 })).toBe("subagents");
    expect(chooseTrayPanel({ ...none, opened: "subagents", runningSubagents: 0 })).toBeNull();
    expect(chooseTrayPanel({ ...none, opened: "tasks", hasBoards: false })).toBeNull();
    expect(chooseTrayPanel({ ...none, opened: "repos", repoCount: 0 })).toBeNull();
    expect(chooseTrayPanel({ ...none, opened: "commands", slowCommands: false })).toBeNull();
    expect(chooseTrayPanel({ ...none, opened: "commands", slowCommands: true })).toBe("commands");
  });
});

const repo = (name: string, over: Partial<SummarisedRepo> = {}): SummarisedRepo => ({ name, changed: 0, conflicted: 0, ...over });

describe("the repository chip", () => {
  it("never reads as one string of two numbers: the total is the label, the attention count its own badge", () => {
    const repos = [
      ...Array.from({ length: 2 }, (_, i) => repo(`dirty-${i}`, { changed: 3 })),
      ...Array.from({ length: 4 }, (_, i) => repo(`changed-${i}`, { changed: 1 })),
      repo("clean-a"),
      repo("clean-b"),
    ];
    const summary = repoSummary(repos, null);
    expect(summary.label).toBe("8 repos");
    expect(summary.label).not.toMatch(/6/);
    expect(summary.badge).toEqual({
      value: 6,
      tone: "warning",
      label: "6 of 8 repositories need attention (6 with changes)",
    });
    expect(summary.title).toBe("8 repositories · 6 of 8 repositories need attention (6 with changes)");
  });

  it("is quiet when everything is clean", () => {
    const summary = repoSummary([repo("a"), repo("b")], null);
    expect(summary.badge).toBeNull();
    expect(summary.title).toBe("2 repositories · all clean");
  });

  it("turns the badge danger for a conflict or an unreadable repository, and says which", () => {
    const summary = repoSummary([repo("a", { conflicted: 2 }), repo("b", { changed: 1 }), repo("c", { error: "not a repository" }), repo("d")], null);
    expect(summary.badge?.tone).toBe("danger");
    expect(summary.badge?.value).toBe(3);
    expect(summary.badge?.label).toBe("3 of 4 repositories need attention (1 with conflicts, 1 with changes, 1 unreadable)");
  });

  it("one repository: its branch, with the changed files as the badge, and ahead/behind in the name", () => {
    const summary = repoSummary([repo("api", { changed: 3, ahead: 2, behind: 1 })], "feature/x");
    expect(summary.label).toBe("feature/x");
    expect(summary.badge).toEqual({ value: 3, tone: "warning", label: "3 changed files" });
    expect(summary.title).toBe("Branch feature/x · ↑2 ↓1 · 3 changed files");
    expect(repoSummary([repo("api", { conflicted: 1, changed: 2 })], "main").badge).toEqual({
      value: 1,
      tone: "danger",
      label: "1 conflicted file, 2 changed",
    });
    expect(repoSummary([repo("api")], "main").badge).toBeNull();
    expect(repoSummary([repo("api", { error: "x" })], null).badge?.tone).toBe("danger");
  });

  it("falls back to the repository's name when the branch is unknown", () => {
    expect(repoSummary([repo("api")], null).label).toBe("api");
  });
});
