import { describe, expect, test } from "bun:test";
import type { AccountPolicy } from "@/bridge/agentAccounts";
import {
  acknowledgeFor,
  fieldsDiffer,
  fieldsOf,
  needsRiskAcknowledgement,
  parseFields,
  refusalOf,
  riskFlow,
  strategyOf,
  thresholdMeaning,
  vendorPolicyOf,
  withVendorPolicy,
} from "./accountPolicy";

const policy: AccountPolicy = {
  revision: 4,
  riskAcknowledgedAt: "2026-10-23T08:00:00Z",
  vendors: {
    claude: {
      enabled: true,
      thresholdPercent: 80,
      weeklyThresholdPercent: 95,
      usageMaxAgeSeconds: 600,
      order: ["claude-work"],
      onLimitRefusal: "continue", strategy: "order", accountThresholds: {},
    },
  },
};

describe("vendorPolicyOf", () => {
  test("nothing stored is off with the defaults", () => {
    expect(vendorPolicyOf(undefined, "codex")).toEqual({
      enabled: false,
      thresholdPercent: 90,
      weeklyThresholdPercent: 0,
      usageMaxAgeSeconds: 300,
      order: [],
      onLimitRefusal: "none", strategy: "order", accountThresholds: {},
    });
    expect(vendorPolicyOf(policy, "codex").enabled).toBe(false);
  });

  test("a stored zero is the default, and a stored vendor is read as it is", () => {
    const zeros: AccountPolicy = {
      ...policy,
      vendors: { claude: { ...policy.vendors.claude, thresholdPercent: 0, usageMaxAgeSeconds: 0, onLimitRefusal: "" } },
    };
    expect(vendorPolicyOf(zeros, "claude")).toMatchObject({ thresholdPercent: 90, usageMaxAgeSeconds: 300, onLimitRefusal: "none", strategy: "order", accountThresholds: {} });
    expect(vendorPolicyOf(policy, "claude")).toMatchObject({ enabled: true, thresholdPercent: 80, order: ["claude-work"] });
  });

  test("an unknown refusal choice is the default one", () => {
    expect(refusalOf("resendIfNoProgress")).toBe("resendIfNoProgress");
    expect(refusalOf("surprise")).toBe("none");
  });
});

describe("parseFields", () => {
  const fields = fieldsOf(vendorPolicyOf(policy, "claude"));

  test("what is stored round-trips, an empty weekly is 0", () => {
    expect(fields).toEqual({ enabled: true, threshold: "80", weekly: "95", maxAge: "600", refusal: "continue", strategy: "order", limits: {} });
    expect(parseFields(fields)).toEqual({
      ok: true,
      value: { enabled: true, thresholdPercent: 80, weeklyThresholdPercent: 95, usageMaxAgeSeconds: 600, onLimitRefusal: "continue", strategy: "order", accountThresholds: {} },
    });
    const parsed = parseFields({ ...fields, weekly: " " });
    expect(parsed.ok && parsed.value.weeklyThresholdPercent).toBe(0);
  });

  test("the bounds are the backend's: 50 to 100 percent, 60 to 3600 seconds", () => {
    expect(parseFields({ ...fields, threshold: "50" }).ok).toBe(true);
    expect(parseFields({ ...fields, threshold: "100" }).ok).toBe(true);
    for (const bad of ["49", "101", "", "ninety", "90.5", "-90", "9e1"]) {
      const parsed = parseFields({ ...fields, threshold: bad });
      expect(parsed.ok).toBe(false);
      if (!parsed.ok) {
        expect(parsed.field).toBe("threshold");
        expect(parsed.problem).toContain("50 to 100");
      }
    }
    expect(parseFields({ ...fields, maxAge: "60" }).ok).toBe(true);
    expect(parseFields({ ...fields, maxAge: "3600" }).ok).toBe(true);
    for (const bad of ["59", "3601", "", "5 min"]) {
      const parsed = parseFields({ ...fields, maxAge: bad });
      expect(parsed.ok).toBe(false);
      if (!parsed.ok) expect(parsed.problem).toContain("60 to 3600");
    }
    const weekly = parseFields({ ...fields, weekly: "20" });
    expect(weekly.ok).toBe(false);
    if (!weekly.ok) expect(weekly.field).toBe("weekly");
  });
});

describe("thresholdMeaning", () => {
  const fields = fieldsOf(vendorPolicyOf(policy, "claude"));
  test("says what the thresholds do, with the weekly one only when it differs", () => {
    expect(thresholdMeaning(parseFields({ ...fields, weekly: "" }))).toBe(
      "A session moves to the next signed-in account at its next turn once its account passes 80% of a window.",
    );
    expect(thresholdMeaning(parseFields(fields))).toContain("or 95% of a weekly one");
    expect(thresholdMeaning(parseFields({ ...fields, threshold: "x" }))).toBe("");
  });
});

describe("saving", () => {
  test("replaces one vendor's rules and nothing else", () => {
    const next = withVendorPolicy(policy, "codex", {
      enabled: false,
      thresholdPercent: 90,
      weeklyThresholdPercent: 0,
      usageMaxAgeSeconds: 300,
      order: ["codex-default"],
      onLimitRefusal: "none", strategy: "order", accountThresholds: {},
    });
    expect(next.revision).toBe(4);
    expect(next.riskAcknowledgedAt).toBe("2026-10-23T08:00:00Z");
    expect(next.vendors.claude).toEqual(policy.vendors.claude);
    expect(next.vendors.codex.order).toEqual(["codex-default"]);
  });

  test("the order is capped where the backend caps it", () => {
    const many = Array.from({ length: 40 }, (_, i) => `a${i}`);
    expect(withVendorPolicy(policy, "claude", { ...vendorPolicyOf(policy, "claude"), order: many }).vendors.claude.order).toHaveLength(32);
  });

  test("the terms warning is owed until one was accepted", () => {
    expect(needsRiskAcknowledgement(undefined)).toBe(true);
    expect(needsRiskAcknowledgement({ riskAcknowledgedAt: "" })).toBe(true);
    expect(needsRiskAcknowledgement({ riskAcknowledgedAt: " " })).toBe(true);
    expect(needsRiskAcknowledgement(policy)).toBe(false);
  });

  test("fields differ when a value was typed, not when whitespace was", () => {
    const base = fieldsOf(vendorPolicyOf(policy, "claude"));
    expect(fieldsDiffer(base, { ...base, threshold: " 80 " })).toBe(false);
    expect(fieldsDiffer(base, { ...base, threshold: "81" })).toBe(true);
    expect(fieldsDiffer(base, { ...base, enabled: false })).toBe(true);
    expect(fieldsDiffer(base, { ...base, refusal: "none" })).toBe(true);
  });
});

describe("acknowledgeFor", () => {
  test("is true only when the warning was just accepted and switching is being turned on", () => {
    expect(acknowledgeFor(true, true, false)).toBe(true);
  });

  test("is false when the terms were acknowledged before, so a threshold edit never re-sends it", () => {
    expect(acknowledgeFor(true, true, true)).toBe(false);
  });

  test("is false when switching is not being turned on, or nothing was accepted", () => {
    expect(acknowledgeFor(true, false, false)).toBe(false);
    expect(acknowledgeFor(false, true, false)).toBe(false);
    expect(acknowledgeFor(false, false, false)).toBe(false);
  });
});

describe("riskFlow", () => {
  const off = riskFlow.initial(false);

  test("switching on before the terms were ever accepted asks, and the switch stays off until confirmed", () => {
    const asked = riskFlow.toggle(off, true, false);
    expect(asked).toEqual({ enabled: false, askRisk: true, accepted: false });
    const confirmed = riskFlow.confirm();
    expect(confirmed).toEqual({ enabled: true, askRisk: false, accepted: true });
  });

  test("cancelling the dialog closes it and leaves the switch off and nothing accepted", () => {
    const cancelled = riskFlow.cancel(riskFlow.toggle(off, true, false));
    expect(cancelled).toEqual({ enabled: false, askRisk: false, accepted: false });
  });

  test("the dialog closing right after a confirm, from the state the confirm produced, keeps the switch on", () => {
    // Radix reports the close of the dialog (onOpenChange(false)) in the same batch as the
    // "Turn on" click, so the editor applies cancel to the confirmed state, not to the render's.
    const asked = riskFlow.toggle(off, true, false);
    expect(riskFlow.cancel(riskFlow.confirm())).toEqual({ enabled: true, askRisk: false, accepted: true });
    // Applied to the stale state it would undo the confirm: that is the bug the updater form avoids.
    expect(riskFlow.cancel(asked)).toEqual({ enabled: false, askRisk: false, accepted: false });
  });

  test("switching on when the terms were acknowledged before asks nothing", () => {
    expect(riskFlow.toggle(off, true, true)).toEqual({ enabled: true, askRisk: false, accepted: false });
  });

  test("switching on again in the same sitting, after accepting, asks nothing", () => {
    const accepted = riskFlow.confirm();
    const switchedOff = riskFlow.toggle(accepted, false, false);
    expect(switchedOff).toEqual({ enabled: false, askRisk: false, accepted: true });
    expect(riskFlow.toggle(switchedOff, true, false)).toEqual({ enabled: true, askRisk: false, accepted: true });
  });

  test("switching off never asks", () => {
    expect(riskFlow.toggle(riskFlow.initial(true), false, false).askRisk).toBe(false);
  });

  test("accepting survives a failed save, which changes nothing, and is cleared when what is stored moves", () => {
    const accepted = riskFlow.confirm();
    // A save that failed makes no transition: the state is the one the person left.
    expect(accepted.accepted).toBe(true);
    expect(riskFlow.saved(accepted)).toEqual({ enabled: true, askRisk: false, accepted: false });
    expect(riskFlow.storedChanged(accepted, true)).toEqual({ enabled: true, askRisk: false, accepted: false });
    expect(riskFlow.storedChanged(accepted, false)).toEqual({ enabled: false, askRisk: false, accepted: false });
  });

  test("stored values moving under an open dialog do not close it", () => {
    expect(riskFlow.storedChanged(riskFlow.toggle(off, true, false), false).askRisk).toBe(true);
  });

  test("the save carries acknowledgeRisk only after accepting, and only while enabling", () => {
    const asked = riskFlow.toggle(off, true, false);
    expect(acknowledgeFor(asked.accepted, asked.enabled, false)).toBe(false);
    const accepted = riskFlow.confirm();
    expect(acknowledgeFor(accepted.accepted, accepted.enabled, false)).toBe(true);
    const switchedOff = riskFlow.toggle(accepted, false, false);
    expect(acknowledgeFor(switchedOff.accepted, switchedOff.enabled, false)).toBe(false);
    // Acknowledged before: nothing is sent again.
    const before = riskFlow.toggle(off, true, true);
    expect(acknowledgeFor(before.accepted, before.enabled, true)).toBe(false);
  });
});

describe("strategy and each account's own limit", () => {
  const base = fieldsOf(vendorPolicyOf(policy, "claude"));

  test("an unknown strategy is the default one", () => {
    expect(strategyOf("best")).toBe("best");
    expect(strategyOf("consumeFirst")).toBe("consumeFirst");
    expect(strategyOf("")).toBe("order");
    expect(strategyOf("surprise")).toBe("order");
  });

  test("a stored limit and strategy round-trip through the form", () => {
    const stored = { ...vendorPolicyOf(policy, "claude"), strategy: "best", accountThresholds: { "claude-work": 70 } };
    const fields = fieldsOf(stored);
    expect(fields.strategy).toBe("best");
    expect(fields.limits).toEqual({ "claude-work": "70" });
    const parsed = parseFields(fields);
    expect(parsed.ok && parsed.value.strategy).toBe("best");
    expect(parsed.ok && parsed.value.accountThresholds).toEqual({ "claude-work": 70 });
  });

  test("an empty limit is no limit, and a wrong one names its account", () => {
    const parsed = parseFields({ ...base, limits: { "claude-work": " ", "claude-spare": "75" } });
    expect(parsed.ok && parsed.value.accountThresholds).toEqual({ "claude-spare": 75 });
    const bad = parseFields({ ...base, limits: { "claude-work": "49" } });
    expect(bad.ok).toBe(false);
    expect(!bad.ok && bad.field).toBe("limit:claude-work");
  });

  test("fields differ by strategy or by a typed limit, not by an empty one or whitespace", () => {
    expect(fieldsDiffer(base, { ...base, strategy: "best" })).toBe(true);
    expect(fieldsDiffer(base, { ...base, limits: { "claude-work": "70" } })).toBe(true);
    expect(fieldsDiffer(base, { ...base, limits: { "claude-work": " " } })).toBe(false);
    const withLimit = { ...base, limits: { "claude-work": "70" } };
    expect(fieldsDiffer(withLimit, { ...withLimit, limits: { "claude-work": " 70 " } })).toBe(false);
  });
});
