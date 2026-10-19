import { describe, expect, it } from "bun:test";
import { QueryClient } from "@tanstack/react-query";

import { PULL_REQUESTS_KEY_PREFIX, pullRequestsKey, watchAccountChanges } from "./usePullRequests";

/** A subscription seam: `fire` plays the event, `listeners` says who is still subscribed. */
function fakeEvents() {
  const listeners = new Set<() => void>();
  return {
    listeners,
    subscribe: (cb: () => void) => {
      listeners.add(cb);
      return () => void listeners.delete(cb);
    },
    fire: () => listeners.forEach((cb) => cb()),
  };
}

describe("watchAccountChanges", () => {
  it("invalidates every session's pull-request query, and only those", () => {
    const client = new QueryClient();
    client.setQueryData(pullRequestsKey("s1", ["/w/a"]), { items: [], nextPollMs: -1 });
    client.setQueryData(pullRequestsKey("s2", ["/w/b", "/w/c"]), { items: [], nextPollMs: -1 });
    client.setQueryData(["agenthub", "sessions"], []);
    client.setQueryData(["accounts"], []);

    const events = fakeEvents();
    watchAccountChanges(client, events.subscribe);
    events.fire();

    const stale = (key: readonly unknown[]) => client.getQueryState(key)?.isInvalidated;
    expect(stale(pullRequestsKey("s1", ["/w/a"]))).toBe(true);
    expect(stale(pullRequestsKey("s2", ["/w/b", "/w/c"]))).toBe(true);
    expect(stale(["agenthub", "sessions"])).toBe(false);
    expect(stale(["accounts"])).toBe(false);
  });

  it("does nothing until the event, and nothing after it is unsubscribed", () => {
    const calls: unknown[] = [];
    const client = { invalidateQueries: (filters: unknown) => (calls.push(filters), Promise.resolve()) };
    const events = fakeEvents();

    const off = watchAccountChanges(client, events.subscribe);
    expect(calls).toHaveLength(0);
    events.fire();
    expect(calls).toEqual([{ queryKey: PULL_REQUESTS_KEY_PREFIX }]);

    off();
    expect(events.listeners.size).toBe(0);
    events.fire();
    expect(calls).toHaveLength(1);
  });

  it("uses the prefix every card's key starts with", () => {
    expect(pullRequestsKey("s1", ["/w/a"]).slice(0, PULL_REQUESTS_KEY_PREFIX.length)).toEqual([...PULL_REQUESTS_KEY_PREFIX]);
  });
});
