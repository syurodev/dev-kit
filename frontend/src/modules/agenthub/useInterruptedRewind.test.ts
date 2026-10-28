import { describe, expect, it } from "bun:test";

import { followInterruptedRewind, type InterruptedRewindSources } from "./useInterruptedRewind";
import type { InterruptedRewind } from "./interruptedRewind";
import {
  CheckpointKind,
  CheckpointStatus,
  type CheckpointSummary,
} from "../../../bindings/devkit/internal/agenthub/models";

const pending = (id: string): CheckpointSummary =>
  ({
    id,
    promptMessageId: "p1",
    kind: CheckpointKind.CheckpointRewindSafety,
    status: CheckpointStatus.CheckpointPending,
    outcome: "ok",
    beforeTree: "t",
    createdAt: "2026-10-28T10:00:00Z",
  }) as CheckpointSummary;

const flush = () => new Promise((resolve) => setTimeout(resolve, 0));

function harness(initial: CheckpointSummary[] = []) {
  const answers = new Map<string, CheckpointSummary[]>([["s1", initial]]);
  const held: { sessionId: string; resolve: (records: CheckpointSummary[]) => void }[] = [];
  const handlers: ((sessionId: string) => void)[] = [];
  let lists = 0;
  let hold = false;
  let fail = false;
  const sources: InterruptedRewindSources = {
    list: (sessionId) => {
      lists += 1;
      if (fail) return Promise.reject(new Error("unreadable"));
      if (hold) return new Promise((resolve) => held.push({ sessionId, resolve }));
      return Promise.resolve(answers.get(sessionId) ?? []);
    },
    onRewound: (handler) => {
      handlers.push(handler);
      return () => handlers.splice(handlers.indexOf(handler), 1);
    },
  };
  const seen: (InterruptedRewind | null)[] = [];
  return {
    sources,
    seen,
    set: (next: InterruptedRewind | null) => seen.push(next),
    last: () => seen[seen.length - 1],
    answer: (sessionId: string, records: CheckpointSummary[]) => answers.set(sessionId, records),
    holdLists: (on: boolean) => (hold = on),
    failLists: (on: boolean) => (fail = on),
    held,
    rewound: (sessionId: string) => handlers.forEach((h) => h(sessionId)),
    lists: () => lists,
    subscribers: () => handlers.length,
  };
}

describe("following a session's interrupted rewind", () => {
  it("shows nothing when the session has no pending rewind row", async () => {
    const h = harness([]);
    const follow = followInterruptedRewind("s1", h.sources, h.set);
    await flush();
    expect(h.last()).toBeNull();
    follow.stop();
  });

  it("shows the pending row of this session", async () => {
    const h = harness([pending("safety-1")]);
    const follow = followInterruptedRewind("s1", h.sources, h.set);
    await flush();
    expect(h.last()?.record.id).toBe("safety-1");
    follow.stop();
  });

  it("reads again when the session was rewound, and only for this session", async () => {
    const h = harness([pending("safety-1")]);
    const follow = followInterruptedRewind("s1", h.sources, h.set);
    await flush();
    h.answer("s1", []);
    h.rewound("other");
    await flush();
    expect(h.lists()).toBe(1);
    expect(h.last()?.record.id).toBe("safety-1");
    h.rewound("s1");
    await flush();
    expect(h.lists()).toBe(2);
    expect(h.last()).toBeNull();
    follow.stop();
    expect(h.subscribers()).toBe(0);
  });

  it("a stale answer never lands after the session moved on", async () => {
    const h = harness();
    h.answer("s1", [pending("safety-1")]);
    h.holdLists(true);
    const first = followInterruptedRewind("s1", h.sources, h.set);
    // the person switches sessions: the first follower stops, the second starts
    first.stop();
    const second = followInterruptedRewind("s2", h.sources, h.set);
    h.held[1].resolve([]);
    await flush();
    // the first session's slow answer arrives last
    h.held[0].resolve([pending("safety-1")]);
    await flush();
    expect(h.seen.some((item) => item?.record.id === "safety-1")).toBe(false);
    expect(h.last()).toBeNull();
    second.stop();
  });

  it("an older read that finishes after a newer one does not bring back a settled row", async () => {
    const h = harness([pending("safety-1")]);
    h.holdLists(true);
    const follow = followInterruptedRewind("s1", h.sources, h.set);
    const slow = follow.reload();
    const fast = follow.reload();
    h.held[2].resolve([]);
    await fast;
    h.held[0].resolve([pending("safety-1")]);
    h.held[1].resolve([pending("safety-1")]);
    await slow;
    expect(h.last()).toBeNull();
    follow.stop();
  });

  it("keeps what it showed when a reload cannot be read, rather than claiming the row is gone", async () => {
    const h = harness([pending("safety-1")]);
    const follow = followInterruptedRewind("s1", h.sources, h.set);
    await flush();
    h.failLists(true);
    await follow.reload();
    expect(h.last()?.record.id).toBe("safety-1");
    follow.stop();
  });

  it("shows nothing for a session whose list cannot be read at first", async () => {
    const h = harness([pending("safety-1")]);
    h.failLists(true);
    const follow = followInterruptedRewind("s1", h.sources, h.set);
    await flush();
    expect(h.last()).toBeNull();
    follow.stop();
  });
});
