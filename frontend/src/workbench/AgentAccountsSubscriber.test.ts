import { describe, expect, it } from "bun:test";
import { handleAccountSwitched, type SwitchDeps } from "./AgentAccountsSubscriber";

const event = { sessionId: "s1", from: "claude-default", to: "claude-work", reason: "5-hour 95% ≥ 90%" };

function deps(over: Partial<SwitchDeps> = {}) {
  const calls = { set: [] as [string, string][], toasts: [] as [string, string][] };
  const full: SwitchDeps = {
    session: () => ({ agentType: "claude", accountId: "" }),
    setAccount: (id, account) => void calls.set.push([id, account]),
    onScreen: () => true,
    labelOf: async () => "Work",
    notify: (title, description) => void calls.toasts.push([title, description]),
    ...over,
  };
  return { full, calls };
}

describe("a session moved to another account", () => {
  it("is recorded in the window's row and told in a toast when the session is on screen", async () => {
    const { full, calls } = deps();
    handleAccountSwitched(event, full);
    await Promise.resolve();
    await Promise.resolve();
    expect(calls.set).toEqual([["s1", "claude-work"]]);
    expect(calls.toasts).toEqual([["Moved to Work", "5-hour 95% ≥ 90%"]]);
  });

  it("is recorded but not announced when nobody is looking at the session", async () => {
    const { full, calls } = deps({ onScreen: () => false });
    handleAccountSwitched(event, full);
    await Promise.resolve();
    expect(calls.set).toEqual([["s1", "claude-work"]]);
    expect(calls.toasts).toEqual([]);
  });

  it("a row that already says so is left alone", async () => {
    const { full, calls } = deps({ session: () => ({ agentType: "claude", accountId: "claude-work" }) });
    handleAccountSwitched(event, full);
    expect(calls.set).toEqual([]);
  });

  it("a session this window does not know changes nothing and says nothing", async () => {
    const { full, calls } = deps({ session: () => undefined });
    handleAccountSwitched(event, full);
    await Promise.resolve();
    expect(calls.set).toEqual([]);
    expect(calls.toasts).toEqual([]);
  });
});
