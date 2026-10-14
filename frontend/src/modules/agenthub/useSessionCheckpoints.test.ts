import { describe, expect, test } from "bun:test";

import { followSessionCheckpoints } from "./useSessionCheckpoints";
import type { CheckpointIndex } from "./checkpoints";

const turn = (id: string, promptMessageId: string) => ({ id, promptMessageId, kind: "turn", status: "done", createdAt: "" }) as never;

function harness() {
  let lists = 0;
  let answer = [turn("c1", "p1")];
  const rewound: ((sessionId: string) => void)[] = [];
  const seen: CheckpointIndex[] = [];
  const deps = {
    list: async () => {
      lists += 1;
      return answer;
    },
    onCheckpoint: () => () => {},
    onRewound: (handler: (sessionId: string) => void) => {
      rewound.push(handler);
      return () => rewound.splice(rewound.indexOf(handler), 1);
    },
  };
  return {
    deps,
    seen,
    set: (next: CheckpointIndex | ((prev: CheckpointIndex) => CheckpointIndex)) => {
      seen.push(typeof next === "function" ? next(seen[seen.length - 1] ?? new Map()) : next);
    },
    rewind: (sessionId: string) => rewound.forEach((h) => h(sessionId)),
    lists: () => lists,
    answer: (next: never[]) => (answer = next),
    subscribers: () => rewound.length,
  };
}

const flush = () => new Promise((resolve) => setTimeout(resolve, 0));

describe("following a session's checkpoints", () => {
  test("a rewind of this session lists them again; another session's does not", async () => {
    const h = harness();
    const off = followSessionCheckpoints("s1", h.deps, h.set);
    await flush();
    expect(h.lists()).toBe(1);
    expect([...h.seen[h.seen.length - 1].keys()]).toEqual(["p1"]);

    h.rewind("other");
    await flush();
    expect(h.lists()).toBe(1);

    h.answer([]);
    h.rewind("s1");
    await flush();
    expect(h.lists()).toBe(2);
    expect(h.seen[h.seen.length - 1].size).toBe(0);

    off();
    expect(h.subscribers()).toBe(0);
  });
});
