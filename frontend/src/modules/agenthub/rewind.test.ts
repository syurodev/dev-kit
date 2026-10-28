import { describe, expect, it } from "bun:test";

import {
  REWIND_FILES_RESTORED,
  REWIND_OUTCOME_UNKNOWN,
  attemptRewind,
  canApplyRewind,
  defaultRewindMode,
  interruptedRewindNotice,
  queueBlocksRewind,
  restorableFiles,
  rewindFailure,
  rewindFailureSpendsThePlan,
  rewindOptions,
  rewindSummary,
  rewindTitle,
} from "./rewind";
import { toAppError } from "../../bridge/errors";
import {
  ChangedSource,
  CheckpointKind,
  CheckpointStatus,
  RewindMode,
  type CheckpointSummary,
  type RewindPreview,
  type RewindResult,
} from "../../../bindings/devkit/internal/agenthub/models";

function checkpoint(overrides: Partial<CheckpointSummary> = {}): CheckpointSummary {
  return {
    id: "cp1",
    sessionId: "s1",
    projectKey: "k",
    promptMessageId: "p1",
    kind: CheckpointKind.CheckpointTurn,
    beforeTree: "aaa",
    afterTree: "bbb",
    status: CheckpointStatus.CheckpointDone,
    outcome: "ok",
    createdAt: "2026-09-17T10:00:00Z",
    changed: [
      { path: "a.ts", kind: "modified", added: 1, removed: 0, binary: false, source: ChangedSource.SourceAgent },
    ],
    ...overrides,
  } as CheckpointSummary;
}

function preview(overrides: Partial<RewindPreview> = {}): RewindPreview {
  return {
    token: "t1",
    sessionId: "s1",
    mode: RewindMode.RewindBoth,
    promptText: "do the thing",
    extraTurnsDropped: 0,
    startsFresh: false,
    warnings: [],
    ...overrides,
  } as RewindPreview;
}

describe("which rewinds are on offer", () => {
  it("offers all three when the turn's files can be put back", () => {
    expect(rewindOptions(checkpoint()).map((option) => option.mode)).toEqual([
      RewindMode.RewindBoth,
      RewindMode.RewindConversation,
      RewindMode.RewindCode,
    ]);
    expect(defaultRewindMode(checkpoint())).toBe(RewindMode.RewindBoth);
  });

  // Offering "files only" with no files is offering a button that does nothing.
  it("offers the conversation alone when there is nothing to restore", () => {
    for (const record of [
      checkpoint({ changed: [] }),
      checkpoint({ beforeTree: "" }),
      null,
    ]) {
      expect(rewindOptions(record).map((option) => option.mode)).toEqual([RewindMode.RewindConversation]);
      expect(defaultRewindMode(record)).toBe(RewindMode.RewindConversation);
    }
  });

  // Changes on record with no way back to before them.
  it("does not call a turn restorable when its opening snapshot is missing", () => {
    expect(restorableFiles(checkpoint({ beforeTree: "" }))).toBe(false);
    expect(restorableFiles(checkpoint({ changed: [] }))).toBe(false);
    expect(restorableFiles(checkpoint())).toBe(true);
  });
});

describe("what the dialog says", () => {
  it("names both halves when both are happening", () => {
    const summary = rewindSummary(
      preview({
        files: { token: "f", sessionId: "s1", mode: "toBefore", actions: [{ path: "a.ts", kind: "write" }], conflicts: [], warnings: [] },
      } as Partial<RewindPreview>),
    );
    expect(summary).toContain("forgets this message");
    expect(summary).toContain("1 file change");
  });

  it("says the conversation starts over when there is nothing to branch from", () => {
    expect(rewindSummary(preview({ startsFresh: true }))).toContain("starts a new conversation");
  });

  it("leaves the conversation out of a files-only rewind", () => {
    const summary = rewindSummary(
      preview({
        mode: RewindMode.RewindCode,
        files: { token: "f", sessionId: "s1", mode: "toBefore", actions: [], conflicts: [], warnings: [] },
      } as Partial<RewindPreview>),
    );
    expect(summary).not.toContain("forgets");
    expect(summary).toContain("no files change");
  });

  it("tells the three rewinds apart by name", () => {
    expect(rewindTitle(RewindMode.RewindBoth).confirm).toBe("Rewind");
    expect(rewindTitle(RewindMode.RewindConversation).title).toContain("conversation");
    expect(rewindTitle(RewindMode.RewindCode).confirm).toBe("Restore files");
  });
});

describe("whether the button does anything", () => {
  // Cutting messages is the point; there is no "nothing to cut" once a message
  // has been named.
  it("is always live for a rewind that touches the conversation", () => {
    expect(canApplyRewind(preview({ mode: RewindMode.RewindConversation }), new Set())).toBe(true);
    expect(canApplyRewind(preview({ mode: RewindMode.RewindBoth }), new Set())).toBe(true);
  });

  // Files only is a restore, and follows the restore's rule.
  it("is dead for a files-only rewind with nothing to write", () => {
    const empty = preview({
      mode: RewindMode.RewindCode,
      files: { token: "f", sessionId: "s1", mode: "toBefore", actions: [], conflicts: [], warnings: [] },
    } as Partial<RewindPreview>);
    expect(canApplyRewind(empty, new Set())).toBe(false);

    const conflicted = preview({
      mode: RewindMode.RewindCode,
      files: {
        token: "f",
        sessionId: "s1",
        mode: "toBefore",
        actions: [],
        conflicts: [{ path: "mine.ts", kind: "write", reason: "changed_since", overridable: true }],
        warnings: [],
      },
    } as Partial<RewindPreview>);
    expect(canApplyRewind(conflicted, new Set())).toBe(false);
    expect(canApplyRewind(conflicted, new Set(["mine.ts"]))).toBe(true);
  });

  it("is dead with no plan at all", () => {
    expect(canApplyRewind(null, new Set())).toBe(false);
  });
});

describe("a rewind that did not succeed", () => {
  // What the window receives is the bridge's payload: Wails wraps the Go error in a
  // RuntimeError whose `cause` carries { code, message, recoverable }.
  const fromBridge = (code: string, message: string) =>
    toAppError(Object.assign(new Error("runtime"), { cause: { code, message, recoverable: true } }));
  const result = { sessionId: "s1", promptText: "p" } as RewindResult;

  it("tells the files-restored and the unconfirmed outcomes from a plain refusal by code", () => {
    expect(rewindFailure(fromBridge(REWIND_FILES_RESTORED, "The files were put back.")).kind).toBe("partial");
    expect(rewindFailure(fromBridge(REWIND_OUTCOME_UNKNOWN, "Could not confirm.")).kind).toBe("unknown");
    expect(rewindFailure(fromBridge("validation_failed", "Validation failed.")).kind).toBe("refused");
    expect(rewindFailure(fromBridge("operation_cancelled", "Operation cancelled.")).kind).toBe("refused");
  });

  it("shows the host's sentence, and never one that says nothing changed", () => {
    expect(rewindFailure(fromBridge(REWIND_FILES_RESTORED, "The files were put back.")).message).toBe("The files were put back.");
    for (const odd of [undefined, null, "boom", {}, new Error("")]) {
      const failure = rewindFailure(odd);
      expect(failure.kind).toBe("refused");
      expect(failure.message).toBe("The rewind did not finish.");
      expect(failure.message.toLowerCase()).not.toContain("nothing was changed");
    }
  });

  it("is a success only when the call resolved, and a failure carries no result", async () => {
    const ok = await attemptRewind(async () => result);
    expect(ok.done).toBe(true);
    for (const code of [REWIND_FILES_RESTORED, REWIND_OUTCOME_UNKNOWN, "validation_failed"]) {
      const attempt = await attemptRewind(async () => {
        throw fromBridge(code, "x");
      });
      expect(attempt.done).toBe(false);
      expect("result" in attempt).toBe(false);
    }
  });

  it("spends the plan for the two outcomes that moved the files, not for a refusal", () => {
    expect(rewindFailureSpendsThePlan({ kind: "partial", message: "" })).toBe(true);
    expect(rewindFailureSpendsThePlan({ kind: "unknown", message: "" })).toBe(true);
    expect(rewindFailureSpendsThePlan({ kind: "refused", message: "" })).toBe(false);
    expect(rewindFailureSpendsThePlan(null)).toBe(false);
  });
});

describe("the queue and the interrupted banner", () => {
  // Queued messages were written for a conversation that is about to stop
  // existing.
  it("says what a queued message would be sent into", () => {
    expect(queueBlocksRewind(0)).toBeNull();
    expect(queueBlocksRewind(1)).toContain("A queued message");
    expect(queueBlocksRewind(3)).toContain("3 queued messages");
  });

  it("raises the banner only for a rewind that is still pending", () => {
    const pending = checkpoint({ kind: CheckpointKind.CheckpointRewindSafety, status: CheckpointStatus.CheckpointPending });
    expect(interruptedRewindNotice(pending)).toContain("interrupted");
    expect(interruptedRewindNotice(checkpoint({ kind: CheckpointKind.CheckpointRewindSafety }))).toBeNull();
    expect(interruptedRewindNotice(checkpoint())).toBeNull();
    expect(interruptedRewindNotice(null)).toBeNull();
  });
});
