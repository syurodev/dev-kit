import { describe, expect, test } from "bun:test";
import type { AccountPolicy, AgentAccount, VendorPolicy } from "@/bridge/agentAccounts";
import { applyUsageRefresh, attempt, dropAccount, moveVendorAccount, replaceAccount, saveVendorPolicy } from "./accountActions";
import { AGENT_ACCOUNT_POLICY_KEY } from "./useAgentAccounts";

const account = (id: string, label = id): AgentAccount => ({
  id,
  vendor: "claude",
  label,
  builtin: false,
  signedIn: true,
  shared: false,
  exhaustedUntil: 0,
  cooldownUntil: 0,
  usage: [],
  usageSource: "",
  observedAt: 0,
});

function fakeClient(initial: AgentAccount[] | undefined) {
  let cache = initial;
  const invalidated: unknown[] = [];
  return {
    client: {
      setQueryData: (_key: unknown, update: (current: AgentAccount[] | undefined) => AgentAccount[] | undefined) => {
        cache = update(cache);
        return cache;
      },
      invalidateQueries: (filter: unknown) => (invalidated.push(filter), Promise.resolve()),
    } as never,
    cache: () => cache,
    invalidated,
  };
}

describe("attempt", () => {
  test("hands back what the call answered and leaves the list alone", async () => {
    const f = fakeClient([]);
    expect(await attempt(f.client, ["k"], async () => 7)).toEqual({ ok: true, value: 7 });
    expect(f.invalidated).toEqual([]);
  });

  test("a refusal comes back as its sentence and the list is read again", async () => {
    const f = fakeClient([]);
    const out = await attempt(f.client, ["agenthub", "accounts", "claude"], async () => {
      throw new Error("A session is using that account. Close it first.");
    });
    expect(out).toEqual({ ok: false, message: "A session is using that account. Close it first." });
    expect(f.invalidated).toEqual([{ queryKey: ["agenthub", "accounts", "claude"] }]);
  });
});

describe("the cached list", () => {
  test("a renamed account takes its own place, and nothing else moves", () => {
    const f = fakeClient([account("a"), account("b")]);
    replaceAccount(f.client, ["k"], account("b", "Renamed"));
    expect(f.cache()?.map((a) => a.label)).toEqual(["a", "Renamed"]);
  });

  test("a list that was never read stays unread", () => {
    const f = fakeClient(undefined);
    replaceAccount(f.client, ["k"], account("a"));
    dropAccount(f.client, ["k"], "a");
    expect(f.cache()).toBeUndefined();
  });

  test("a removed account leaves the list", () => {
    const f = fakeClient([account("a"), account("b")]);
    dropAccount(f.client, ["k"], "a");
    expect(f.cache()?.map((a) => a.id)).toEqual(["b"]);
  });
});

const vendorPolicy = (over: Partial<VendorPolicy> = {}): VendorPolicy => ({
  enabled: true,
  thresholdPercent: 85,
  weeklyThresholdPercent: 0,
  usageMaxAgeSeconds: 300,
  order: ["a", "b"],
  onLimitRefusal: "none",
  ...over,
});

const stored: AccountPolicy = {
  revision: 7,
  riskAcknowledgedAt: "2026-10-23T08:00:00Z",
  vendors: { claude: vendorPolicy(), codex: vendorPolicy({ thresholdPercent: 70, order: ["x"] }) },
};

function policyClient() {
  const sets: { key: unknown; value: unknown }[] = [];
  const invalidated: unknown[] = [];
  return {
    client: {
      setQueryData: (key: unknown, value: unknown) => (sets.push({ key, value }), value),
      invalidateQueries: (filter: unknown) => (invalidated.push(filter), Promise.resolve()),
    } as never,
    sets,
    invalidated,
  };
}

describe("saveVendorPolicy", () => {
  test("sends the whole preference at the revision it was read at, the other vendors kept, and the flag as given", async () => {
    const c = policyClient();
    const calls: { policy: AccountPolicy; ack: boolean }[] = [];
    const answer: AccountPolicy = { ...stored, revision: 8 };
    const next = vendorPolicy({ thresholdPercent: 95 });
    const out = await saveVendorPolicy(c.client, stored, "claude", next, true, async (policy, ack) => {
      calls.push({ policy, ack });
      return answer;
    });
    expect(out).toEqual({ ok: true, value: answer });
    expect(calls).toHaveLength(1);
    expect(calls[0].ack).toBe(true);
    expect(calls[0].policy.revision).toBe(7);
    expect(calls[0].policy.vendors.claude.thresholdPercent).toBe(95);
    expect(calls[0].policy.vendors.codex).toEqual(stored.vendors.codex);
    // The cache holds the answer and nothing is read again.
    expect(c.sets).toEqual([{ key: AGENT_ACCOUNT_POLICY_KEY, value: answer }]);
    expect(c.invalidated).toEqual([]);
  });

  test("a refusal comes back as its sentence, the rules are read again and the cache is left alone", async () => {
    const c = policyClient();
    const out = await saveVendorPolicy(c.client, stored, "claude", vendorPolicy(), false, async () => {
      throw new Error("The rules changed elsewhere. Try again.");
    });
    expect(out).toEqual({ ok: false, message: "The rules changed elsewhere. Try again." });
    expect(c.sets).toEqual([]);
    expect(c.invalidated).toEqual([{ queryKey: AGENT_ACCOUNT_POLICY_KEY }]);
  });
});

describe("moveVendorAccount", () => {
  test("writes the full order as shown, without the terms flag, the rest of the rules kept", async () => {
    const c = policyClient();
    const calls: { policy: AccountPolicy; ack: boolean }[] = [];
    const list = [account("a"), account("b"), account("c")];
    await moveVendorAccount(c.client, stored, "claude", list, "c", -1, async (policy, ack) => (calls.push({ policy, ack }), policy));
    expect(calls[0].ack).toBe(false);
    expect(calls[0].policy.revision).toBe(7);
    expect(calls[0].policy.vendors.claude).toEqual(vendorPolicy({ order: ["a", "c", "b"] }));
    expect(calls[0].policy.vendors.codex).toEqual(stored.vendors.codex);
  });

  test("a vendor with nothing stored gets its defaults and the order", async () => {
    const c = policyClient();
    const calls: AccountPolicy[] = [];
    const list = [account("a"), account("b")];
    await moveVendorAccount(c.client, { revision: 1, riskAcknowledgedAt: "", vendors: {} }, "claude", list, "b", -1, async (policy) => (calls.push(policy), policy));
    expect(calls[0].vendors.claude).toEqual({
      enabled: false,
      thresholdPercent: 90,
      weeklyThresholdPercent: 0,
      usageMaxAgeSeconds: 300,
      order: ["b", "a"],
      onLimitRefusal: "none",
    });
  });
});

describe("applyUsageRefresh", () => {
  test("puts the answer in the list; a reading that did not move says so quietly", () => {
    const f = fakeClient([account("a")]);
    const note = applyUsageRefresh(f.client, ["k"], { ...account("a"), observedAt: 5 }, { ...account("a"), observedAt: 5, label: "New" });
    expect(note).toContain("Nothing new");
    expect(f.cache()?.[0].label).toBe("New");
  });

  test("a newer reading has no note", () => {
    const f = fakeClient([account("a")]);
    expect(applyUsageRefresh(f.client, ["k"], { ...account("a"), observedAt: 5 }, { ...account("a"), observedAt: 9 })).toBe("");
  });

  test("an account that was not on screen has no note", () => {
    const f = fakeClient([account("a")]);
    expect(applyUsageRefresh(f.client, ["k"], undefined, account("a"))).toBe("");
  });
});
