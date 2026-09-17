import { describe, expect, it } from "bun:test";

import {
  applyCheckpoint,
  changeCounts,
  changeSummary,
  indexCheckpoints,
  orderChangedFiles,
  overlapNote,
  partialReason,
  sourceLabel,
} from "./checkpoints";
import {
  ChangedSource,
  CheckpointKind,
  CheckpointStatus,
  type ChangedFile,
  type CheckpointRecord,
} from "../../../bindings/devkit/internal/agenthub/models";

function record(overrides: Partial<CheckpointRecord> = {}): CheckpointRecord {
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
    ...overrides,
  } as CheckpointRecord;
}

function file(overrides: Partial<ChangedFile> = {}): ChangedFile {
  return {
    path: "src/a.ts",
    kind: "modified",
    added: 1,
    removed: 0,
    binary: false,
    source: ChangedSource.SourceUnattributed,
    ...overrides,
  } as ChangedFile;
}

describe("indexing", () => {
  it("keys a turn's checkpoint on its prompt, which is the only id both ends share", () => {
    const index = indexCheckpoints([record({ promptMessageId: "p1" }), record({ id: "cp2", promptMessageId: "p2" })]);
    expect(index.get("p1")?.id).toBe("cp1");
    expect(index.get("p2")?.id).toBe("cp2");
  });

  it("leaves out the safety rows a restore and a rewind write: they are not turns", () => {
    const index = indexCheckpoints([
      record({ id: "safety", kind: CheckpointKind.CheckpointRestoreSafety, promptMessageId: "p1" }),
    ]);
    expect(index.size).toBe(0);
  });

  it("lets a later record replace an earlier one for the same prompt", () => {
    const first = indexCheckpoints([record({ changed: [file()] })]);
    // What a report arriving after the turn produces: the same checkpoint, re-attributed.
    const next = applyCheckpoint(first, record({ changed: [file({ source: ChangedSource.SourceAgent })] }));
    expect(next.get("p1")?.changed?.[0]?.source).toBe(ChangedSource.SourceAgent);
    expect(first.get("p1")?.changed?.[0]?.source).toBe(ChangedSource.SourceUnattributed);
  });
});

describe("the turn header's count", () => {
  it("counts the files and totals the lines", () => {
    const summary = changeSummary(
      record({ changed: [file({ added: 3, removed: 1 }), file({ path: "b.ts", added: 9, removed: 2 })] }),
    );
    expect(summary).toEqual({ files: 2, added: 12, removed: 3, label: "2 files changed", partial: false });
  });

  it("says file, not files, for one", () => {
    expect(changeSummary(record({ changed: [file()] }))?.label).toBe("1 file changed");
  });

  // A turn that changed nothing and a turn nobody checkpointed both have
  // nothing to show, and a "0 files changed" on the second would be a claim
  // about a project DevKit never looked at.
  it("says nothing for a turn with no changes and nothing for a turn with no checkpoint", () => {
    expect(changeSummary(record({ changed: [] }))).toBeNull();
    expect(changeSummary(undefined)).toBeNull();
    expect(changeSummary(record({ status: CheckpointStatus.CheckpointFailed, outcome: "unavailable" }))).toBeNull();
  });

  it("marks a half-recorded turn as partial and explains which half is missing", () => {
    const summary = changeSummary(record({ outcome: "before_missing", changed: [file()] }));
    expect(summary?.partial).toBe(true);
    expect(partialReason(record({ outcome: "before_missing" }))).toContain("cannot be undone");
    expect(partialReason(record({ outcome: "ok" }))).toBeNull();
  });
});

describe("attribution", () => {
  // The distinction the whole feature rests on: a file nothing accounts for is
  // labelled as that, never as the agent's.
  it("never calls an unattributed file the agent's", () => {
    expect(sourceLabel(file({ source: ChangedSource.SourceUnattributed })).text).toBe("Unattributed");
    expect(sourceLabel(file({ source: ChangedSource.SourceAgent })).text).toBe("Agent");
    expect(sourceLabel(file({ source: ChangedSource.SourceChild })).text).toBe("Sub-agent");
  });

  it("names the other sessions that were running, which is the honest half of unattributed", () => {
    expect(overlapNote(record({ overlap: JSON.stringify([{ sessionId: "s2" }]) as never }))).toContain("Another session");
    expect(overlapNote(record({ overlap: JSON.stringify([{ sessionId: "s2" }, { sessionId: "s3" }]) as never }))).toContain("2 other sessions");
    expect(overlapNote(record())).toBeNull();
  });

  it("drops an overlap field it cannot read rather than breaking the header", () => {
    expect(overlapNote(record({ overlap: "{{not json" as never }))).toBeNull();
  });

  it("puts what the agent owns first, then sorts by path", () => {
    const ordered = orderChangedFiles([
      file({ path: "z.ts", source: ChangedSource.SourceUnattributed }),
      file({ path: "b.ts", source: ChangedSource.SourceAgent }),
      file({ path: "a.ts", source: ChangedSource.SourceChild }),
      file({ path: "a2.ts", source: ChangedSource.SourceAgent }),
    ]);
    expect(ordered.map((entry) => entry.path)).toEqual(["a2.ts", "b.ts", "a.ts", "z.ts"]);
  });

  it("shows no line counts for a binary file, because git counted none", () => {
    expect(changeCounts(file({ binary: true, added: 0, removed: 0 }))).toBeNull();
    expect(changeCounts(file({ added: 2, removed: 5 }))).toBe("+2 −5");
  });
});
