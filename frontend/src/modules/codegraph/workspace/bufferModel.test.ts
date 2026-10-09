import { describe, expect, it } from "bun:test";
import {
  ABSENT,
  canSave,
  conflictUnanswered,
  keptMine,
  discardedLocal,
  edited,
  emptyBuffer,
  loaded,
  orphaned,
  reconcile,
  saveConflicted,
  saveFailed,
  saveRevision,
  saveStarted,
  saveSucceeded,
  type FileBuffer,
} from "./bufferModel";

const id = { workspaceId: "w", folderId: "f", path: "src/config.ts" };
const open = (content = "one\n", revision = "r1"): FileBuffer => loaded(emptyBuffer(id), { content, revision });

describe("dirty is derived from the base", () => {
  it("typing a change back out is clean again", () => {
    let b = edited(open(), "one\ntwo\n");
    expect(b.dirty).toBe(true);
    b = edited(b, "one\n");
    expect(b.dirty).toBe(false);
  });
});

describe("a clean buffer follows the disk", () => {
  it("reloads when the file's revision changed", () => {
    const { buffer, reload } = reconcile(open(), { exists: true, revision: "r2" });
    expect(reload).toBe(true);
    expect(buffer.external).toBeNull();
    expect(buffer.diskRevision).toBe("r2");
  });

  it("does nothing for its own save echoed back by the watcher", () => {
    let b = edited(open(), "changed\n");
    b = saveSucceeded(saveStarted(b), "changed\n", "r2");
    const { buffer, reload } = reconcile(b, { exists: true, revision: "r2" });
    expect(reload).toBe(false);
    expect(buffer.external).toBeNull();
    expect(buffer.dirty).toBe(false);
  });

  it("does nothing for a touch that left the bytes alone", () => {
    const { reload } = reconcile(open(), { exists: true, revision: "r1" });
    expect(reload).toBe(false);
  });

  it("keeps a deleted file's text on screen and says so, without recreating it", () => {
    const { buffer, reload } = reconcile(open(), { exists: false });
    expect(reload).toBe(false);
    expect(buffer.content).toBe("one\n");
    expect(buffer.disk).toBe("deleted");
    expect(buffer.external).toBe("deleted");
  });

  it("recovers when the file comes back unchanged", () => {
    const gone = reconcile(open(), { exists: false }).buffer;
    const back = reconcile(gone, { exists: true, revision: "r1" }).buffer;
    expect(back.external).toBeNull();
    expect(back.disk).toBe("present");
  });
});

describe("a dirty buffer is never overwritten by the disk", () => {
  const dirty = () => edited(open(), "mine\n");

  it("flags the conflict and leaves the edits alone", () => {
    const { buffer, reload } = reconcile(dirty(), { exists: true, revision: "r2" });
    expect(reload).toBe(false);
    expect(buffer.external).toBe("changed");
    expect(buffer.content).toBe("mine\n");
    expect(buffer.baseRevision).toBe("r1");
  });

  it("notices the disk change again when it changes a second time", () => {
    const first = reconcile(dirty(), { exists: true, revision: "r2" }).buffer;
    const second = reconcile(first, { exists: true, revision: "r3" }).buffer;
    expect(second.external).toBe("changed");
    expect(second.diskRevision).toBe("r3");
  });

  it("refuses a plain save while in conflict — keeping local edits never makes it silent", () => {
    const conflicted = reconcile(dirty(), { exists: true, revision: "r2" }).buffer;
    expect(saveRevision(conflicted)).toBeNull();
    expect(canSave(conflicted)).toBe(false);
    // Overwriting is only by an explicit mode, and says what it overwrites.
    expect(saveRevision(conflicted, "overwrite")).toBe("*");
    // "Keep mine" changes nothing about the base, so the guard is still there.
    expect(conflicted.baseRevision).toBe("r1");
  });

  it("deleted under a dirty buffer: save only recreates on purpose", () => {
    const gone = reconcile(dirty(), { exists: false }).buffer;
    expect(saveRevision(gone)).toBeNull();
    expect(saveRevision(gone, "recreate")).toBe(ABSENT);
    expect(gone.content).toBe("mine\n");
  });

  it("reloading takes the disk's version and drops the conflict", () => {
    const conflicted = reconcile(dirty(), { exists: true, revision: "r2" }).buffer;
    const b = discardedLocal(conflicted, { content: "theirs\n", revision: "r2" });
    expect(b.content).toBe("theirs\n");
    expect(b.dirty).toBe(false);
    expect(b.external).toBeNull();
    expect(b.baseRevision).toBe("r2");
  });
});

describe("Keep mine", () => {
  it("quiets the banner for that disk revision only, and never unguards the save", () => {
    const conflicted = reconcile(edited(open(), "mine\n"), { exists: true, revision: "r2" }).buffer;
    expect(conflictUnanswered(conflicted)).toBe(true);
    const kept = keptMine(conflicted);
    expect(conflictUnanswered(kept)).toBe(false);
    expect(saveRevision(kept)).toBeNull();
    // The disk changes again: a new conflict, loud again.
    const again = reconcile(kept, { exists: true, revision: "r3" }).buffer;
    expect(again.external).toBe("changed");
    expect(conflictUnanswered(again)).toBe(true);
  });
});

describe("saving", () => {
  it("asserts the revision it was based on", () => {
    expect(saveRevision(edited(open(), "x"))).toBe("r1");
    expect(saveRevision(edited(emptyBuffer(id), "new"))).toBe(ABSENT);
  });

  it("typing during the save leaves the buffer dirty, never marked clean", () => {
    let b = edited(open(), "v1\n");
    b = saveStarted(b);
    b = edited(b, "v1\nand more\n"); // typed while the write is in flight
    b = saveSucceeded(b, "v1\n", "r2");
    expect(b.dirty).toBe(true);
    expect(b.base).toBe("v1\n");
    expect(b.baseRevision).toBe("r2");
    expect(b.content).toBe("v1\nand more\n");
  });

  it("a failed save keeps the edits and shows why", () => {
    const b = saveFailed(saveStarted(edited(open(), "x")), "disk full");
    expect(b.dirty).toBe(true);
    expect(b.content).toBe("x");
    expect(b.error).toBe("disk full");
    expect(b.saving).toBe(false);
  });

  it("a refused save (conflict) keeps the edits and records what the disk has", () => {
    const b = saveConflicted(saveStarted(edited(open(), "x")), { exists: true, revision: "r9" });
    expect(b.external).toBe("changed");
    expect(b.diskRevision).toBe("r9");
    expect(b.content).toBe("x");
    expect(b.saving).toBe(false);
  });

  it("ignores disk observations while a write is in flight", () => {
    const b = saveStarted(edited(open(), "x"));
    const r = reconcile(b, { exists: true, revision: "r-own-write" });
    expect(r.buffer).toBe(b);
    expect(r.reload).toBe(false);
  });
});

describe("a buffer whose folder left the workspace", () => {
  it("keeps its edits and cannot be saved", () => {
    const b = orphaned(edited(open(), "draft"), "folder_removed");
    expect(b.content).toBe("draft");
    expect(b.dirty).toBe(true);
    expect(saveRevision(b)).toBeNull();
    expect(saveRevision(b, "overwrite")).toBeNull();
    expect(canSave(b)).toBe(false);
  });
});

describe("line endings", () => {
  it("a CRLF file is clean when opened and stays CRLF", () => {
    const b = loaded(emptyBuffer(id), { content: "a\r\nb\r\n", revision: "r" });
    expect(b.eol).toBe("\r\n");
    expect(b.dirty).toBe(false);
    expect(b.content).toBe("a\r\nb\r\n");
    // The editor, given lineSeparator CRLF, hands the same text back: not an edit.
    expect(edited(b, "a\r\nb\r\n").dirty).toBe(false);
  });

  it("a mixed file is normalized and flagged, and the normalized text is the base", () => {
    const b = loaded(emptyBuffer(id), { content: "a\r\nb\n", revision: "r" });
    expect(b.mixedEol).toBe(true);
    expect(b.dirty).toBe(false);
    expect(b.base).toBe(b.content);
  });
});

describe("unreadable files", () => {
  it("is flagged without being reloaded as text", () => {
    const { buffer, reload } = reconcile(open(), { exists: true, revision: "bin", unreadable: "binary" });
    expect(reload).toBe(false);
    expect(buffer.disk).toBe("unreadable");
    expect(buffer.external).toBe("changed");
  });
});
