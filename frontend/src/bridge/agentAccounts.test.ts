import { describe, expect, test } from "bun:test";
import type { AgentAccountInfo, AgentAccountPolicyState } from "../../bindings/devkit/internal/bridge/models";
import { accountOf, parseAccountSwitched, policyOf, rawPolicyOf } from "./agentAccounts";

const raw: AgentAccountInfo = {
  id: "claude-work",
  vendor: "claude",
  label: "Work",
  builtin: false,
  signedIn: true,
  shared: true,
  exhaustedUntil: 0,
  cooldownUntil: 0,
  usage: [{ label: "5-hour", usedPercent: 41.5, resetsAt: 1_800_000_000, status: "ok" }],
  usageSource: "checked",
  observedAt: 1_799_999_000,
};

describe("accountOf", () => {
  test("keeps what Go sent", () => {
    expect(accountOf(raw)).toEqual({
      id: "claude-work",
      vendor: "claude",
      label: "Work",
      builtin: false,
      signedIn: true,
      shared: true,
      exhaustedUntil: 0,
      cooldownUntil: 0,
      usage: [{ label: "5-hour", usedPercent: 41.5, resetsAt: 1_800_000_000, status: "ok" }],
      usageSource: "checked",
      observedAt: 1_799_999_000,
    });
  });

  test("a null usage list is an empty one, and an absent reset or status is 0 and empty", () => {
    expect(accountOf({ ...raw, usage: null }).usage).toEqual([]);
    const bare = accountOf({ ...raw, usage: [{ label: "Weekly", usedPercent: 10 }] });
    expect(bare.usage).toEqual([{ label: "Weekly", usedPercent: 10, resetsAt: 0, status: "" }]);
  });
});

describe("policy", () => {
  const state: AgentAccountPolicyState = {
    revision: 3,
    riskAcknowledgedAt: "2026-10-23T08:00:00Z",
    vendors: {
      claude: {
        enabled: true,
        thresholdPercent: 90,
        weeklyThresholdPercent: 0,
        usageMaxAgeSeconds: 300,
        order: ["claude-work", "claude-default"],
        onLimitRefusal: "none",
      },
      codex: undefined,
    },
  };

  test("reads vendors, drops an absent one, and never hands out Go's array", () => {
    const policy = policyOf(state);
    expect(Object.keys(policy.vendors)).toEqual(["claude"]);
    expect(policy.vendors.claude.order).toEqual(["claude-work", "claude-default"]);
    expect(policy.vendors.claude.order).not.toBe(state.vendors?.claude?.order);
    expect(policy.revision).toBe(3);
    expect(policy.riskAcknowledgedAt).toBe("2026-10-23T08:00:00Z");
  });

  test("null vendors and a null order are empty", () => {
    expect(policyOf({ revision: 0, riskAcknowledgedAt: "", vendors: null })).toEqual({
      revision: 0,
      riskAcknowledgedAt: "",
      vendors: {},
    });
    const noOrder = policyOf({
      revision: 1,
      riskAcknowledgedAt: "",
      vendors: { claude: { ...state.vendors!.claude!, order: null } },
    });
    expect(noOrder.vendors.claude.order).toEqual([]);
  });

  test("goes back to Go as it was read", () => {
    const policy = policyOf(state);
    const again = rawPolicyOf(policy);
    expect(again.revision).toBe(3);
    expect(again.vendors?.claude?.order).toEqual(["claude-work", "claude-default"]);
    expect(policyOf(again)).toEqual(policy);
  });
});

describe("parseAccountSwitched", () => {
  test("reads the event main.go emits, array-wrapped or not", () => {
    const event = { sessionId: "s1", from: "claude-default", to: "claude-work", reason: "5-hour 95% ≥ 90%" };
    expect(parseAccountSwitched(event)).toEqual(event);
    expect(parseAccountSwitched([event])).toEqual(event);
  });

  test("a missing reason is empty, anything malformed is not a switch", () => {
    expect(parseAccountSwitched({ sessionId: "s1", from: "a", to: "b" })?.reason).toBe("");
    expect(parseAccountSwitched(null)).toBeNull();
    expect(parseAccountSwitched({ sessionId: "s1", from: "a" })).toBeNull();
    expect(parseAccountSwitched({ sessionId: "", from: "a", to: "b" })).toBeNull();
    expect(parseAccountSwitched({ sessionId: "s1", from: "a", to: "" })).toBeNull();
  });
});
