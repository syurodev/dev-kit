import { describe, expect, test } from "bun:test";
import type { AgentAccount } from "@/bridge/agentAccounts";
import { attempt, dropAccount, replaceAccount } from "./accountActions";

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
