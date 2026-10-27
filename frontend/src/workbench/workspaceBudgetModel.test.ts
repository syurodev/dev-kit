import { describe, expect, it } from "bun:test";

import type { BudgetPage } from "../bridge/harness";
import { BudgetKind } from "../../bindings/devkit/internal/harnessconfig/models";
import {
  buildBudgetEdits,
  describeEntry,
  formatKiB,
  isBudgetDirty,
  parseKiB,
  pendingCount,
  rebaseBudgets,
  samplePreview,
  savedRule,
  setIntent,
  type BudgetDraft,
} from "./workspaceBudgetModel";

const page = (patch: Partial<BudgetPage> = {}): BudgetPage =>
  ({
    workspaceId: "w",
    profileRevision: 3,
    profilePresent: true,
    defaultBytes: 24576,
    minBytes: 2048,
    maxBytes: 65536,
    groups: [
      { group: "git", tools: 3, cuttable: 3, bytes: 8192, cut: "headTail" },
      { group: "db", tools: 2, cuttable: 2 },
    ],
    tools: [
      { tool: "git.diff", group: "git", definitionBytes: 700, exempt: false, toolBytes: 16384, groupBytes: 8192 },
      { tool: "db.query", group: "db", definitionBytes: 700, exempt: false },
    ],
    ...patch,
  }) as BudgetPage;

describe("budget sizes", () => {
  it("parses KiB into exact bytes and refuses what is not a limit", () => {
    expect(parseKiB("16")).toEqual({ ok: true, bytes: 16384 });
    expect(parseKiB(" 2 ")).toEqual({ ok: true, bytes: 2048 });
    expect(parseKiB("64")).toEqual({ ok: true, bytes: 65536 });
    expect(parseKiB("2.5")).toEqual({ ok: true, bytes: 2560 });
    for (const bad of ["", "abc", "1", "65", "-4", "1e3", "2.0005", "0x10"]) {
      expect(parseKiB(bad).ok).toBe(false);
    }
  });

  it("states the exact byte value beside the KiB", () => {
    expect(formatKiB(24576)).toBe("24 KiB (24,576 bytes)");
    expect(formatKiB(2560)).toBe("2.50 KiB (2,560 bytes)");
  });
});

describe("independent intents per field", () => {
  const saved = savedRule(page(), "tool", "git.diff");

  it("reads the explicit saved rule, not the inherited value", () => {
    expect(savedRule(page(), "group", "git")).toEqual({ bytes: 8192, cut: "headTail" });
    expect(savedRule(page(), "tool", "db.query")).toEqual({ bytes: undefined, cut: undefined });
    expect(savedRule(page(), "tool", "not.loaded")).toBeNull();
    expect(savedRule(null, "group", "git")).toBeNull();
  });

  it("keeps size and cut as separate intents of one rule", () => {
    let d: BudgetDraft = {};
    d = setIntent(d, "tool", "git.diff", "bytes", { op: "reset" }, saved);
    d = setIntent(d, "tool", "git.diff", "cut", { op: "set", value: "head" }, saved);
    expect(buildBudgetEdits(d)).toEqual([{ kind: BudgetKind.BudgetTool, name: "git.diff", resetBytes: true, cut: "head" }]);
    // Clearing one field leaves the other pending.
    d = setIntent(d, "tool", "git.diff", "bytes", undefined, saved);
    expect(buildBudgetEdits(d)).toEqual([{ kind: BudgetKind.BudgetTool, name: "git.diff", cut: "head" }]);
    d = setIntent(d, "tool", "git.diff", "cut", undefined, saved);
    expect(isBudgetDirty(d)).toBe(false);
  });

  it("drops an intent the saved rule already satisfies", () => {
    let d: BudgetDraft = {};
    d = setIntent(d, "tool", "git.diff", "bytes", { op: "set", value: 16384 }, saved); // equals the saved size
    expect(isBudgetDirty(d)).toBe(false);
    d = setIntent(d, "tool", "git.diff", "cut", { op: "reset" }, saved); // saved has no cut of its own
    expect(isBudgetDirty(d)).toBe(false);
    d = setIntent(d, "tool", "git.diff", "bytes", { op: "set", value: 4096 }, saved);
    expect(pendingCount(d)).toBe(1);
  });

  it("distinguishes setting head from resetting to the inherited mode", () => {
    const group = savedRule(page(), "group", "git");
    let d = setIntent({}, "group", "git", "cut", { op: "set", value: "head" }, group);
    expect(buildBudgetEdits(d)).toEqual([{ kind: BudgetKind.BudgetGroup, name: "git", cut: "head" }]);
    d = setIntent({}, "group", "git", "cut", { op: "reset" }, group);
    expect(buildBudgetEdits(d)).toEqual([{ kind: BudgetKind.BudgetGroup, name: "git", resetCut: true }]);
  });

  it("builds edits in a stable order and never invents a field", () => {
    let d: BudgetDraft = {};
    d = setIntent(d, "tool", "db.query", "bytes", { op: "set", value: 2048 }, null);
    d = setIntent(d, "group", "git", "bytes", { op: "reset" }, null);
    expect(buildBudgetEdits(d)).toEqual([
      { kind: BudgetKind.BudgetGroup, name: "git", resetBytes: true },
      { kind: BudgetKind.BudgetTool, name: "db.query", bytes: 2048 },
    ]);
    expect(describeEntry(d["group:git"])).toBe("size: inherit");
  });
});

describe("refresh and conflict keep the person's edits", () => {
  it("keeps what is still pending, forgets what became true, keeps rows off the loaded pages", () => {
    let d: BudgetDraft = {};
    d = setIntent(d, "tool", "db.query", "bytes", { op: "set", value: 4096 }, null);
    d = setIntent(d, "tool", "unloaded.tool", "cut", { op: "set", value: "headTail" }, null);
    d = setIntent(d, "tool", "git.diff", "bytes", { op: "set", value: 4096 }, null);
    // Someone else saved 4096 for db.query meanwhile.
    const refreshed = page({
      tools: [
        { tool: "git.diff", group: "git", definitionBytes: 1, exempt: false, toolBytes: 16384 },
        { tool: "db.query", group: "db", definitionBytes: 1, exempt: false, toolBytes: 4096 },
      ],
    } as Partial<BudgetPage>);
    const next = rebaseBudgets(d, refreshed);
    expect(Object.keys(next).sort()).toEqual(["tool:git.diff", "tool:unloaded.tool"]);
  });

  it("drops an edit for a group the server no longer registers", () => {
    const d = setIntent({}, "group", "gone", "bytes", { op: "set", value: 4096 }, null);
    expect(rebaseBudgets(d, page())).toEqual({});
  });

  it("is a no-op without a page", () => {
    const d = setIntent({}, "tool", "x.y", "bytes", { op: "set", value: 4096 }, null);
    expect(rebaseBudgets(d, null)).toEqual(d);
  });
});

describe("the illustration", () => {
  it("is deterministic, fake and shows what each mode keeps", () => {
    const head = samplePreview("head", 8192);
    const both = samplePreview("headTail", 8192);
    expect(samplePreview("head", 8192)).toEqual(head);
    expect(head[0]).toContain("sample line 01");
    expect(head[head.length - 1]).toContain("left out");
    expect(both[0]).toContain("sample line 01");
    expect(both[both.length - 1]).toContain("sample line 24"); // the end survives
    expect(both.some((l) => l.includes("middle left out"))).toBe(true);
    expect(samplePreview("head", 65536).length).toBeGreaterThan(0);
  });
});
