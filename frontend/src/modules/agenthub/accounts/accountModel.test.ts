import { describe, expect, test } from "bun:test";
import type { AgentAccount } from "@/bridge/agentAccounts";
import {
  builtinAccountHelp,
  accountLabelFor,
  accountLabelOf,
  accountNotes,
  agoText,
  inWords,
  isBuiltinAccountId,
  movedOrder,
  moveId,
  noticeText,
  orderedAccounts,
  readingText,
  refreshNote,
  switchedToast,
  usageRows,
} from "./accountModel";

const NOW = Date.UTC(2026, 9, 23, 12, 0, 0);
const nowSeconds = NOW / 1000;

const account = (over: Partial<AgentAccount> = {}): AgentAccount => ({
  id: "claude-work",
  vendor: "claude",
  label: "Work",
  builtin: false,
  signedIn: true,
  shared: false,
  exhaustedUntil: 0,
  cooldownUntil: 0,
  usage: [],
  usageSource: "",
  observedAt: 0,
  ...over,
});
const builtin = account({ id: "claude-default", label: "System default", builtin: true });

describe("account names", () => {
  test("the built-in account is known by its id and by an empty one", () => {
    expect(isBuiltinAccountId("claude", "")).toBe(true);
    expect(isBuiltinAccountId("claude", "claude-default")).toBe(true);
    expect(isBuiltinAccountId("claude", "codex-default")).toBe(false);
  });

  test("a label comes from the list; the built-in one has fallbacks, a removed one has words", () => {
    expect(accountLabelOf([builtin, account()], "claude", "claude-work")).toBe("Work");
    expect(accountLabelOf(undefined, "claude", "claude-default")).toBe("System default");
    expect(accountLabelOf(undefined, "codex", "codex-default")).toBe("DevKit default");
    expect(accountLabelOf([builtin], "claude", "claude-default")).toBe("System default");
    expect(accountLabelOf(undefined, "claude", "claude-work")).toBe("another account");
    expect(accountLabelOf([builtin], "claude", "claude-gone")).toBe("a removed account");
  });

  test("the summary row is absent for an empty id and while the list that would name it is missing", () => {
    expect(accountLabelFor(undefined, "claude", "")).toBeUndefined();
    expect(accountLabelFor(undefined, "claude", "claude-work")).toBeUndefined();
    expect(accountLabelFor([builtin, account()], "claude", "claude-work")).toBe("Work");
    expect(accountLabelFor(undefined, "claude", "claude-default")).toBe("System default");
    expect(accountLabelFor([builtin], "claude", "claude-gone")).toBe("a removed account");
  });
});

describe("the built-in account", () => {
  test("is explained per vendor", () => {
    expect(builtinAccountHelp("claude")).toContain("already uses on this computer");
    expect(builtinAccountHelp("codex")).toContain("DevKit keeps for its own sessions");
  });
});

describe("order", () => {
  test("moveId swaps neighbours and stops at the ends", () => {
    expect(moveId(["a", "b", "c"], "b", -1)).toEqual(["b", "a", "c"]);
    expect(moveId(["a", "b", "c"], "b", 1)).toEqual(["a", "c", "b"]);
    expect(moveId(["a", "b", "c"], "a", -1)).toEqual(["a", "b", "c"]);
    expect(moveId(["a", "b", "c"], "c", 1)).toEqual(["a", "b", "c"]);
    expect(moveId(["a", "b"], "zzz", 1)).toEqual(["a", "b"]);
  });

  test("the ordered ids come first, a removed id is skipped, the rest keep their place", () => {
    const list = [builtin, account(), account({ id: "claude-spare", label: "Spare" })];
    expect(orderedAccounts(list, ["claude-spare", "claude-gone", "claude-spare"]).map((a) => a.id)).toEqual([
      "claude-spare",
      "claude-default",
      "claude-work",
    ]);
    expect(orderedAccounts(list, []).map((a) => a.id)).toEqual(["claude-default", "claude-work", "claude-spare"]);
  });

  test("a move is written out in full from what is on screen", () => {
    const list = [builtin, account(), account({ id: "claude-spare", label: "Spare" })];
    expect(movedOrder(list, [], "claude-spare", -1)).toEqual(["claude-default", "claude-spare", "claude-work"]);
    // On screen: Work, System default, Spare. System default moves above Work.
    expect(movedOrder(list, ["claude-work", "claude-gone"], "claude-default", -1)).toEqual([
      "claude-default",
      "claude-work",
      "claude-spare",
    ]);
  });
});

describe("times", () => {
  test("inWords and agoText round to the unit a person reads", () => {
    expect(inWords(nowSeconds + 12 * 60, NOW)).toBe("in 12m");
    expect(inWords(nowSeconds + 3 * 3600, NOW)).toBe("in 3h");
    expect(inWords(nowSeconds + 4 * 86_400, NOW)).toBe("in 4d");
    expect(inWords(nowSeconds - 100, NOW)).toBe("in 0m");
    expect(agoText(nowSeconds - 20, NOW)).toBe("just now");
    expect(agoText(nowSeconds - 3 * 60, NOW)).toBe("3m ago");
    expect(agoText(nowSeconds - 5 * 3600, NOW)).toBe("5h ago");
    expect(agoText(nowSeconds - 4 * 86_400, NOW)).toBe("4d ago");
    expect(agoText(nowSeconds + 50, NOW)).toBe("just now");
  });
});

describe("usage", () => {
  test("a window is a percentage, its reset and a tone from the vendor's status", () => {
    const rows = usageRows(
      account({
        usage: [
          { label: "5-hour", usedPercent: 41.6, resetsAt: nowSeconds + 2 * 3600, status: "ok" },
          { label: "Weekly", usedPercent: 83, resetsAt: nowSeconds + 3 * 86_400, status: "warning" },
          { label: "Opus", usedPercent: 100, resetsAt: nowSeconds + 600, status: "reached" },
          { label: "Other", usedPercent: 12, resetsAt: 0, status: "" },
        ],
      }),
      NOW,
    );
    expect(rows.map((r) => [r.label, r.percent, r.text, r.tone])).toEqual([
      ["5-hour", 42, "42% · resets in 2h", undefined],
      ["Weekly", 83, "83% · resets in 3d", "warning"],
      ["Opus", 100, "100% · resets in 10m", "danger"],
      ["Other", 12, "12%", undefined],
    ]);
  });

  test("a window that reset since the reading is drawn empty and says so; junk is clamped", () => {
    const rows = usageRows(
      account({
        usage: [
          { label: "5-hour", usedPercent: 97, resetsAt: nowSeconds - 60, status: "reached" },
          { label: "Weekly", usedPercent: 140, resetsAt: nowSeconds + 3600, status: "" },
        ],
      }),
      NOW,
    );
    expect(rows[0]).toMatchObject({ percent: 0, text: "reset since the last reading", tone: undefined });
    expect(rows[1].percent).toBe(100);
  });

  test("the reading says who saw it and how long ago", () => {
    expect(readingText(account(), NOW)).toBe("No usage reading yet");
    expect(readingText(account({ observedAt: nowSeconds - 180, usageSource: "checked" }), NOW)).toBe("Checked 3m ago");
    expect(readingText(account({ observedAt: nowSeconds - 7200, usageSource: "observed" }), NOW)).toBe("Observed 2h ago");
  });

  test("an unchanged reading time is the sign that nothing was asked", () => {
    expect(refreshNote({ observedAt: 5 }, { observedAt: 9 })).toBe("");
    expect(refreshNote({ observedAt: 5 }, { observedAt: 5 })).toContain("Nothing new");
  });
});

describe("what keeps an account out", () => {
  test("limited and cooling down each say so; past times say nothing", () => {
    expect(accountNotes(account({ exhaustedUntil: nowSeconds + 3 * 3600 }), NOW)).toEqual([
      { key: "limit", text: "Limit reached · available in 3h", tone: "danger" },
    ]);
    expect(accountNotes(account({ cooldownUntil: nowSeconds + 600 }), NOW)[0].text).toContain("retry in 10m");
    expect(accountNotes(account({ exhaustedUntil: nowSeconds - 5, cooldownUntil: nowSeconds - 5 }), NOW)).toEqual([]);
  });
});

describe("words for the notice and the toast", () => {
  test("a switch reads as a move, a failure leads with what did not happen", () => {
    expect(noticeText({ kind: "switch", from: "a", to: "b", reason: "5-hour 95% ≥ 90%" }, "System default", "Work")).toBe(
      "Switched from System default to Work · 5-hour 95% ≥ 90%",
    );
    expect(noticeText({ kind: "switch", from: "a", to: "b" }, "A", "B")).toBe("Switched from A to B");
    expect(noticeText({ kind: "failed", from: "a", to: "b", reason: "It could not be opened." }, "A", "B")).toBe(
      "Could not switch from A to B: It could not be opened.",
    );
  });

  test("the toast names where the session went and why", () => {
    expect(switchedToast("Work", "Weekly 95% ≥ 90%")).toEqual({ title: "Moved to Work", description: "Weekly 95% ≥ 90%" });
  });
});
