import { detectEol, type Eol } from "./eol";
import type { DocIdentity } from "./identity";

/**
 * One open file's buffer, as a state machine.
 *
 * A buffer is the editor's text plus what it is *based on*: the text and the
 * disk revision it was loaded or last saved as. Everything about external
 * changes follows from keeping those apart:
 *
 *   current   what the editor shows            (content)
 *   base      what the buffer equalled on disk (base, baseRevision)
 *   observed  what disk last looked like       (diskRevision, disk)
 *
 * `dirty` is derived — content differs from base — so typing a change back
 * out is clean again, and a save that finishes while the person is typing leaves
 * the buffer dirty by itself, with no flag to forget to set.
 *
 * Nothing here talks to the bridge: each function takes what was observed and
 * returns the next buffer (and, for reconcile, whether the file should be read
 * again). That keeps every awkward case — a self-save echoed back by the
 * watcher, a file replaced by an atomic rename, one deleted under a dirty
 * buffer — a plain test.
 */

export type DiskState = "present" | "deleted" | "unreadable";

/** Why a dirty buffer is not trusted to overwrite what is on disk. */
export type ExternalState = null | "changed" | "deleted";

/** The buffer's folder left the workspace, or is gone from the disk. */
export type Orphan = null | "folder_removed" | "folder_unavailable";

/** Where to put the caret and scroll once the editor has the text. */
export type RestoreTarget = {
  line: number;
  column?: number;
  anchor?: number;
  head?: number;
  scrollTop?: number;
  /** Bumped per request so two requests for the same spot are two requests. */
  token: number;
};

export type FileBuffer = DocIdentity & {
  content: string;
  base: string;
  baseRevision: string;
  diskRevision: string;
  disk: DiskState;
  external: ExternalState;
  /**
   * The disk revision whose conflict the person has seen and answered "Keep
   * mine" to. It only quiets the banner: the buffer is still in conflict and a
   * plain Save is still refused. A different disk revision is a new conflict.
   */
  ack: string;
  dirty: boolean;
  saving: boolean;
  loading: boolean;
  error: string;
  orphan: Orphan;
  /** The line a navigation asked to reveal, and the request counter for it. */
  line: number | null;
  revealToken: number;
  restore: RestoreTarget | null;
  /** Set when a clean buffer was replaced by a newer disk version (Unix ms). */
  reloadedAt: number;
  /** The file's line ending, restored by the editor on every write (eol.ts). */
  eol: Eol;
  /** The file mixed endings; it was normalized to `eol` when opened. */
  mixedEol: boolean;
};

export const ABSENT = "absent";

export function emptyBuffer(id: DocIdentity): FileBuffer {
  return {
    ...id,
    content: "",
    base: "",
    baseRevision: "",
    diskRevision: "",
    disk: "present",
    external: null,
    ack: "",
    dirty: false,
    saving: false,
    loading: true,
    error: "",
    orphan: null,
    line: null,
    revealToken: 0,
    restore: null,
    reloadedAt: 0,
    eol: "\n",
    mixedEol: false,
  };
}

/** The file was read: its text is both what is shown and what the buffer is based on. */
export function loaded(b: FileBuffer, read: { content: string; revision: string }): FileBuffer {
  const { text, eol, mixed } = detectEol(read.content);
  return {
    ...b,
    content: text,
    base: text,
    eol,
    mixedEol: mixed,
    baseRevision: read.revision,
    diskRevision: read.revision,
    disk: "present",
    external: null,
    dirty: false,
    loading: false,
    error: "",
  };
}

/** The editor's text changed. */
export function edited(b: FileBuffer, content: string): FileBuffer {
  if (content === b.content) return b;
  return { ...b, content, dirty: content !== b.base };
}

export type DiskObservation =
  | { exists: false }
  | { exists: true; revision: string; unreadable?: string };

export type Reconciled = {
  buffer: FileBuffer;
  /** True: the buffer is clean and disk has a newer version — read it again. */
  reload: boolean;
};

/**
 * Brings a buffer up to date with what was just observed on disk.
 *
 * A clean buffer follows the disk. A dirty one never does: its edits are the
 * only copy, so a different disk revision only raises `external`, and the
 * person decides (compare, reload, or overwrite on purpose).
 *
 * While a save is in flight the observation is ignored — it may be the save's
 * own write — and the caller reconciles again when the save settles.
 */
export function reconcile(b: FileBuffer, seen: DiskObservation): Reconciled {
  if (b.saving) return { buffer: b, reload: false };

  if (!seen.exists) {
    if (b.disk === "deleted" && b.external === "deleted" && b.diskRevision === ABSENT) return { buffer: b, reload: false };
    return { buffer: { ...b, disk: "deleted", external: "deleted", diskRevision: ABSENT }, reload: false };
  }
  if (seen.unreadable) {
    return {
      buffer: { ...b, disk: "unreadable", diskRevision: seen.revision, external: seen.revision === b.baseRevision ? null : "changed" },
      reload: false,
    };
  }
  if (seen.revision === b.baseRevision) {
    // Our own save echoed back, a `touch`, or the file put back as it was.
    if (b.disk === "present" && b.external === null && b.diskRevision === seen.revision) return { buffer: b, reload: false };
    return { buffer: { ...b, disk: "present", external: null, diskRevision: seen.revision }, reload: false };
  }
  if (b.dirty) {
    if (b.external === "changed" && b.diskRevision === seen.revision && b.disk === "present") return { buffer: b, reload: false };
    return { buffer: { ...b, disk: "present", external: "changed", diskRevision: seen.revision }, reload: false };
  }
  // Clean: follow the disk. The caller reads the text; until then note what is there.
  return { buffer: { ...b, disk: "present", external: null, diskRevision: seen.revision }, reload: true };
}

/** What the next Save may send. */
export type SaveMode = "normal" | "overwrite" | "recreate";

/**
 * The revision a save asserts about the disk, or null when it must not go
 * ahead. A buffer in conflict (`external`) saves only by an explicit choice —
 * "Keep my edits" dismisses nothing about that: the next plain Save is refused
 * again, so a conflict is never overwritten silently.
 */
export function saveRevision(b: FileBuffer, mode: SaveMode = "normal"): string | null {
  if (b.orphan || b.saving) return null;
  if (mode === "overwrite") return b.disk === "deleted" ? ABSENT : "*";
  if (mode === "recreate") return b.disk === "deleted" ? ABSENT : null;
  if (b.external) return null;
  return b.baseRevision || ABSENT;
}

export function canSave(b: FileBuffer): boolean {
  return b.dirty && saveRevision(b) !== null;
}

/** A write began. */
export function saveStarted(b: FileBuffer): FileBuffer {
  return { ...b, saving: true, error: "" };
}

/** The write landed: `saved` is exactly what was written. */
export function saveSucceeded(b: FileBuffer, saved: string, revision: string): FileBuffer {
  return {
    ...b,
    base: saved,
    baseRevision: revision,
    diskRevision: revision,
    disk: "present",
    external: null,
    // Typing during the save keeps it dirty: content moved on, base is `saved`.
    dirty: b.content !== saved,
    saving: false,
    error: "",
  };
}

/** The write was refused because the disk is no longer what the buffer is based on. */
export function saveConflicted(b: FileBuffer, seen: DiskObservation): FileBuffer {
  const base = { ...b, saving: false, error: "" };
  if (!seen.exists) return { ...base, disk: "deleted", external: "deleted", diskRevision: ABSENT };
  return { ...base, disk: "present", external: "changed", diskRevision: seen.revision };
}

export function saveFailed(b: FileBuffer, message: string): FileBuffer {
  return { ...b, saving: false, error: message };
}

/** The folder this buffer belongs to is no longer reachable; its edits stay. */
export function orphaned(b: FileBuffer, reason: Exclude<Orphan, null>): FileBuffer {
  return b.orphan === reason ? b : { ...b, orphan: reason, saving: false };
}

/** The person chose to take the disk's version, discarding local edits. */
export function discardedLocal(b: FileBuffer, read: { content: string; revision: string }): FileBuffer {
  return { ...loaded(b, read), reloadedAt: Date.now() };
}

/** "Keep mine": the conflict has been seen. Saving stays guarded. */
export function keptMine(b: FileBuffer): FileBuffer {
  return b.external ? { ...b, ack: b.diskRevision, error: "" } : b;
}

/** Whether the conflict banner should be loud: a conflict nobody has answered yet. */
export function conflictUnanswered(b: FileBuffer): boolean {
  return b.external !== null && b.ack !== b.diskRevision;
}
