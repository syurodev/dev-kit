import { describe, expect, it } from "bun:test";

import {
  attemptUndo,
  interruptedRewindText,
  noticeActions,
  selectInterruptedRewind,
  settleInterruptedRewind,
  undoRequest,
  undoResultLine,
  type NoticeState,
} from "./interruptedRewind";
import {
  CheckpointKind,
  CheckpointStatus,
  RestoreMode,
  type CheckpointSummary,
} from "../../../bindings/devkit/internal/agenthub/models";

function row(overrides: Partial<CheckpointSummary> = {}): CheckpointSummary {
  return {
    id: "safety-1",
    promptMessageId: "p1",
    kind: CheckpointKind.CheckpointRewindSafety,
    status: CheckpointStatus.CheckpointPending,
    outcome: "ok",
    beforeTree: "tree-before",
    createdAt: "2026-10-28T10:00:00Z",
    ...overrides,
  } as CheckpointSummary;
}

const state = (overrides: Partial<NoticeState> = {}): NoticeState => ({
  canUndo: true,
  sessionRunning: false,
  confirmingKeep: false,
  busy: false,
  restored: false,
  ...overrides,
});

describe("which row is an interrupted rewind", () => {
  it("is nothing for a session without a pending rewind safety row", () => {
    expect(selectInterruptedRewind(undefined)).toBeNull();
    expect(selectInterruptedRewind([])).toBeNull();
    expect(
      selectInterruptedRewind([
        // an ordinary turn
        row({ id: "t", kind: CheckpointKind.CheckpointTurn, status: CheckpointStatus.CheckpointDone }),
        // a rewind that finished, one that was refused, and a plain restore's own safety row
        row({ id: "d", status: CheckpointStatus.CheckpointDone }),
        row({ id: "f", status: CheckpointStatus.CheckpointFailed }),
        row({ id: "r", kind: CheckpointKind.CheckpointRestoreSafety, status: CheckpointStatus.CheckpointPending }),
      ]),
    ).toBeNull();
  });

  it("is the pending rewind safety row", () => {
    const found = selectInterruptedRewind([row({ id: "t", kind: CheckpointKind.CheckpointTurn }), row()]);
    expect(found?.record.id).toBe("safety-1");
    expect(found?.others).toBe(0);
    expect(found?.canUndo).toBe(true);
  });

  it("offers the newest of several and says how many more there are", () => {
    const found = selectInterruptedRewind([
      row({ id: "old", createdAt: "2026-10-28T09:00:00Z" }),
      row({ id: "new", createdAt: "2026-10-28T11:00:00Z" }),
      row({ id: "mid", createdAt: "2026-10-28T10:00:00Z" }),
    ]);
    expect(found?.record.id).toBe("new");
    expect(found?.others).toBe(2);
  });

  it("cannot put files back when no snapshot is held", () => {
    expect(selectInterruptedRewind([row({ beforeTree: "" })])?.canUndo).toBe(false);
  });
});

describe("the request goes through the existing undo-restore flow", () => {
  it("names the session, the undo-restore mode and the safety row, nothing else", () => {
    expect(undoRequest("s1", row())).toEqual({
      sessionId: "s1",
      mode: RestoreMode.RestoreUndoRestore,
      checkpointId: "safety-1",
      promptMessageId: "p1",
    });
  });
});

describe("what the notice says", () => {
  it("states what is true: files may be put back or partly, the conversation was not rewound, a copy is kept", () => {
    const text = interruptedRewindText({ canUndo: true, others: 0 });
    expect(text.title).toBe("A rewind did not finish");
    const body = text.body.join(" ");
    expect(body).toContain("may have been put back, or only partly");
    expect(body).toContain("The conversation was not rewound");
    expect(body).toContain("kept a copy of the files as they were before the rewind");
  });

  it("does not promise the rewind will finish by itself", () => {
    const all = [
      ...interruptedRewindText({ canUndo: true, others: 2 }).body,
      ...interruptedRewindText({ canUndo: false, others: 0 }).body,
    ].join(" ");
    expect(all).not.toMatch(/retry|will finish|continue|resume|again/i);
  });

  it("says the files cannot be put back from here when no copy is held", () => {
    const body = interruptedRewindText({ canUndo: false, others: 0 }).body.join(" ");
    expect(body).toContain("cannot be put back from here");
    expect(body).not.toContain("kept a copy");
  });

  it("counts the other interrupted rewinds", () => {
    expect(interruptedRewindText({ canUndo: true, others: 1 }).body.join(" ")).toContain("1 other rewind");
    expect(interruptedRewindText({ canUndo: true, others: 3 }).body.join(" ")).toContain("3 other rewinds");
  });
});

describe("the actions on offer are only what the backend can do", () => {
  const ids = (s: NoticeState) => noticeActions(s).map((a) => a.id);

  it("offers undo and keep, nothing that would repeat or complete the rewind", () => {
    expect(ids(state())).toEqual(["undo", "keep"]);
    for (const action of noticeActions(state())) {
      expect(action.label).not.toMatch(/retry|finish|complete|again|continue|resume/i);
    }
  });

  it("offers no undo when there is no snapshot", () => {
    expect(ids(state({ canUndo: false }))).toEqual(["keep"]);
  });

  it("disables undo while the session is running, and while anything is in flight", () => {
    expect(noticeActions(state({ sessionRunning: true })).find((a) => a.id === "undo")?.disabled).toBe(true);
    expect(noticeActions(state({ busy: true })).every((a) => a.disabled)).toBe(true);
    expect(noticeActions(state()).every((a) => !a.disabled)).toBe(true);
  });

  it("asks before it closes the way back, and a cancel returns to the two choices", () => {
    expect(ids(state({ confirmingKeep: true }))).toEqual(["confirmKeep", "cancelKeep"]);
  });

  it("after a restore the notice can be closed, and undo stays for whatever was left alone", () => {
    expect(ids(state({ restored: true }))).toEqual(["undo", "done"]);
  });
});

describe("one undo attempt", () => {
  it("is done with the result when the apply resolves", async () => {
    const result = { written: ["a.ts"], deleted: [], safetyCheckpointId: "c9" };
    expect(await attemptUndo(async () => result)).toEqual({ done: true, result });
  });

  it("reports a failure with the host's sentence and does not claim nothing changed", async () => {
    const attempt = await attemptUndo(async () => {
      throw new Error("Agent session is busy.");
    });
    expect(attempt.done).toBe(false);
    if (attempt.done) throw new Error("unreachable");
    expect(attempt.message).toContain("Agent session is busy.");
    expect(attempt.message).toContain("cannot tell whether any file changed");
    expect(attempt.message).not.toMatch(/nothing (was )?changed|no files? (were )?changed/i);
  });

  it("still says it did not finish when the failure carries no message", async () => {
    const attempt = await attemptUndo(async () => {
      throw "boom";
    });
    if (attempt.done) throw new Error("unreachable");
    expect(attempt.message).toContain("The restore did not finish.");
  });
});

describe("the outcome line", () => {
  it("counts what was put back and deleted", () => {
    expect(undoResultLine({ written: ["a", "b"], deleted: ["c"], safetyCheckpointId: "x" })).toBe(
      "2 files put back, 1 file deleted.",
    );
    expect(undoResultLine({ written: null, deleted: null, safetyCheckpointId: "x" })).toBe(
      "Nothing needed to change.",
    );
  });
});

describe("closing the notice", () => {
  it("settles the row through the backend and reloads", async () => {
    const calls: string[] = [];
    const outcome = await settleInterruptedRewind(
      "safety-1",
      async (id) => void calls.push(`dismiss ${id}`),
      async () => void calls.push("reload"),
    );
    expect(outcome).toEqual({ ok: true });
    expect(calls).toEqual(["dismiss safety-1", "reload"]);
  });

  it("reports a refused settle truthfully and still reloads, because the row may be gone or still there", async () => {
    const calls: string[] = [];
    const outcome = await settleInterruptedRewind(
      "safety-1",
      async () => {
        throw new Error("Something went wrong.");
      },
      async () => void calls.push("reload"),
    );
    expect(outcome).toEqual({ ok: false, message: "Something went wrong. The notice is still open." });
    expect(calls).toEqual(["reload"]);
  });
});
