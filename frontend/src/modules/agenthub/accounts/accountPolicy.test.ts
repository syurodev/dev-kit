import { describe, expect, test } from "bun:test";
import type { AccountPolicy } from "@/bridge/agentAccounts";
import {
  fieldsDiffer,
  fieldsOf,
  needsRiskAcknowledgement,
  parseFields,
  refusalOf,
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
      onLimitRefusal: "continue",
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
      onLimitRefusal: "none",
    });
    expect(vendorPolicyOf(policy, "codex").enabled).toBe(false);
  });

  test("a stored zero is the default, and a stored vendor is read as it is", () => {
    const zeros: AccountPolicy = {
      ...policy,
      vendors: { claude: { ...policy.vendors.claude, thresholdPercent: 0, usageMaxAgeSeconds: 0, onLimitRefusal: "" } },
    };
    expect(vendorPolicyOf(zeros, "claude")).toMatchObject({ thresholdPercent: 90, usageMaxAgeSeconds: 300, onLimitRefusal: "none" });
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
    expect(fields).toEqual({ enabled: true, threshold: "80", weekly: "95", maxAge: "600", refusal: "continue" });
    expect(parseFields(fields)).toEqual({
      ok: true,
      value: { enabled: true, thresholdPercent: 80, weeklyThresholdPercent: 95, usageMaxAgeSeconds: 600, onLimitRefusal: "continue" },
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
      onLimitRefusal: "none",
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
