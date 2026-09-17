import { describe, expect, it } from "bun:test";

import {
  canApplyRestore,
  conflictReason,
  forcedPaths,
  restoreCounts,
  restoreSummary,
  restoreTitle,
  toggleForced,
} from "./restore";
import { RestoreMode, type RestorePreview } from "../../../bindings/devkit/internal/agenthub/models";

function preview(overrides: Partial<RestorePreview> = {}): RestorePreview {
  return {
    token: "t1",
    sessionId: "s1",
    mode: RestoreMode.RestoreUndoTurn,
    actions: [],
    conflicts: [],
    warnings: [],
    ...overrides,
  } as RestorePreview;
}

describe("the dialog's summary", () => {
  // "5 files restored" reads as five files coming back. Some of them are files
  // going away, and that is the half worth being explicit about.
  it("names deletions separately from writes", () => {
    const summary = restoreSummary(
      preview({
        actions: [
          { path: "a.ts", kind: "write" },
          { path: "b.ts", kind: "write" },
          { path: "c.ts", kind: "delete" },
        ],
      }),
    );
    expect(summary).toBe("2 files restored, 1 file deleted");
  });

  it("says plainly when there is nothing to do", () => {
    expect(restoreSummary(preview())).toContain("Nothing to change");
    expect(restoreSummary(null)).toContain("Nothing to change");
  });

  it("counts the two kinds of conflict apart, because only one can be forced", () => {
    const counts = restoreCounts(
      preview({
        conflicts: [
          { path: "mine.ts", kind: "write", reason: "changed_since", overridable: true },
          { path: "big.bin", kind: "write", reason: "protected", overridable: false },
          { path: "via-link.ts", kind: "write", reason: "unsafe_path", overridable: false },
        ],
      }),
    );
    expect(counts).toEqual({ written: 0, deleted: 0, overridable: 1, blocked: 2 });
  });
});

describe("what the confirm button may do", () => {
  it("is off for a plan that would write nothing", () => {
    expect(canApplyRestore(preview(), new Set())).toBe(false);
    expect(canApplyRestore(null, new Set())).toBe(false);
  });

  it("is on once a forceable conflict has been chosen, and not before", () => {
    const plan = preview({
      conflicts: [{ path: "mine.ts", kind: "write", reason: "changed_since", overridable: true }],
    });
    expect(canApplyRestore(plan, new Set())).toBe(false);
    expect(canApplyRestore(plan, new Set(["mine.ts"]))).toBe(true);
  });

  // The backend refuses these anyway; the point is that the UI never offers a
  // button whose only effect would be to be refused.
  it("stays off when the only conflict cannot be forced", () => {
    const plan = preview({
      conflicts: [{ path: "big.bin", kind: "write", reason: "protected", overridable: false }],
    });
    expect(canApplyRestore(plan, new Set(["big.bin"]))).toBe(false);
  });
});

describe("what gets sent", () => {
  it("sends only the conflicts that can be forced and were chosen", () => {
    const plan = preview({
      conflicts: [
        { path: "mine.ts", kind: "write", reason: "changed_since", overridable: true },
        { path: "theirs.ts", kind: "write", reason: "changed_since", overridable: true },
        { path: "big.bin", kind: "write", reason: "protected", overridable: false },
      ],
    });
    expect(forcedPaths(plan, new Set(["mine.ts", "big.bin"]))).toEqual(["mine.ts"]);
  });

  it("toggles one path without disturbing the rest", () => {
    const first = toggleForced(new Set(["a"]), "b");
    expect([...first].sort()).toEqual(["a", "b"]);
    expect([...toggleForced(first, "a")]).toEqual(["b"]);
  });
});

describe("wording", () => {
  it("tells the three restores apart by name", () => {
    expect(restoreTitle(RestoreMode.RestoreUndoTurn).title).toContain("Undo this turn");
    expect(restoreTitle(RestoreMode.RestoreToBeforeTurn).title).toContain("before this message");
    expect(restoreTitle(RestoreMode.RestoreUndoRestore).confirm).toBe("Undo restore");
  });

  it("explains each conflict in terms of what the user would lose", () => {
    expect(conflictReason({ path: "a", kind: "write", reason: "changed_since", overridable: true })).toContain("overwrite");
    expect(conflictReason({ path: "a", kind: "write", reason: "protected", overridable: false })).toContain("does not know");
    expect(conflictReason({ path: "a", kind: "write", reason: "unsafe_path", overridable: false })).toContain("link");
  });
});
