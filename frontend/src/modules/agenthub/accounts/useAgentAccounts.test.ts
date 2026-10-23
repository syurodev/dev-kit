import { describe, expect, test } from "bun:test";
import { AGENT_ACCOUNTS_KEY, agentAccountsKey, watchAgentAccounts } from "./useAgentAccounts";

function fakes() {
  const invalidated: unknown[] = [];
  const client = { invalidateQueries: (filter: unknown) => (invalidated.push(filter), Promise.resolve()) };
  let changed: () => void = () => {};
  let unsubscribed = 0;
  const subscribe = (cb: () => void) => {
    changed = cb;
    return () => void (unsubscribed += 1);
  };
  const listeners = new Map<string, () => void>();
  const focus = {
    addEventListener: (type: string, listener: () => void) => void listeners.set(type, listener),
    removeEventListener: (type: string) => void listeners.delete(type),
  } as unknown as Pick<Window, "addEventListener" | "removeEventListener">;
  return { client, invalidated, subscribe, focus, listeners, fire: () => changed(), unsubscribed: () => unsubscribed };
}

describe("account query keys", () => {
  test("a vendor's list sits under the key every list is invalidated by", () => {
    expect(agentAccountsKey("claude")).toEqual([...AGENT_ACCOUNTS_KEY, "claude"]);
  });
});

describe("watchAgentAccounts", () => {
  test("a change event and a window focus both refetch every list", () => {
    const f = fakes();
    watchAgentAccounts(f.client, f.subscribe, f.focus);
    f.fire();
    f.listeners.get("focus")?.();
    expect(f.invalidated).toEqual([{ queryKey: AGENT_ACCOUNTS_KEY }, { queryKey: AGENT_ACCOUNTS_KEY }]);
  });

  test("stopping removes both listeners", () => {
    const f = fakes();
    const stop = watchAgentAccounts(f.client, f.subscribe, f.focus);
    expect(f.listeners.has("focus")).toBe(true);
    stop();
    expect(f.listeners.has("focus")).toBe(false);
    expect(f.unsubscribed()).toBe(1);
  });

  test("with no window to listen on it still follows the event", () => {
    const f = fakes();
    watchAgentAccounts(f.client, f.subscribe, null);
    f.fire();
    expect(f.invalidated).toHaveLength(1);
  });
});
