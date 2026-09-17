import {
  ChangedSource,
  CheckpointKind,
  CheckpointStatus,
  type ChangedFile,
  type CheckpointRecord,
} from "../../../bindings/devkit/internal/agenthub/models";

/**
 * What a turn changed on disk.
 *
 * A checkpoint is keyed on the prompt that started its turn, because that is
 * the one name the live stream, a reloaded page, the turn row and the
 * checkpoint all agree on — a transcript item's own id differs between a live
 * event and a row read back.
 *
 * Nothing here decides whether the agent changed a file. The backend does that
 * and says how sure it is; this only renders the answer, including the case
 * where the honest answer is "something did, and here is who else was running".
 */

export type CheckpointIndex = ReadonlyMap<string, CheckpointRecord>;

export const EMPTY_CHECKPOINTS: CheckpointIndex = new Map();

/**
 * Index a session's checkpoints by prompt.
 *
 * Only turn checkpoints: the safety rows a restore and a rewind leave behind
 * are not turns and have no header to sit in. A later row for the same prompt
 * wins — a report that arrived after the turn rewrites its checkpoint.
 */
export function indexCheckpoints(records: readonly CheckpointRecord[] | null | undefined): CheckpointIndex {
  const out = new Map<string, CheckpointRecord>();
  for (const record of records ?? []) {
    if (record.kind !== CheckpointKind.CheckpointTurn) continue;
    if (!record.promptMessageId) continue;
    out.set(record.promptMessageId, record);
  }
  return out;
}

/** One record landing on an index, as the live event delivers it. */
export function applyCheckpoint(index: CheckpointIndex, record: CheckpointRecord | null | undefined): CheckpointIndex {
  if (!record || record.kind !== CheckpointKind.CheckpointTurn || !record.promptMessageId) return index;
  const next = new Map(index);
  next.set(record.promptMessageId, record);
  return next;
}

export type ChangeSummary = {
  files: number;
  added: number;
  removed: number;
  /** "3 files changed" — the turn header's line. */
  label: string;
  /** The checkpoint holds only one side, so there is nothing to restore to. */
  partial: boolean;
};

/**
 * The turn header's count, or null when there is nothing to say.
 *
 * Null rather than "0 files changed": a turn that changed nothing and a turn
 * that was never checkpointed look identical in a zero, and only one of them
 * should be silent. A failed checkpoint is also null — the row exists to
 * explain itself elsewhere, not to put "unknown" in a header.
 */
export function changeSummary(record: CheckpointRecord | undefined | null): ChangeSummary | null {
  if (!record || record.status !== CheckpointStatus.CheckpointDone) return null;
  const changed = record.changed ?? [];
  if (changed.length === 0) return null;
  let added = 0;
  let removed = 0;
  for (const file of changed) {
    added += file.added ?? 0;
    removed += file.removed ?? 0;
  }
  return {
    files: changed.length,
    added,
    removed,
    label: `${changed.length} ${changed.length === 1 ? "file" : "files"} changed`,
    partial: record.outcome !== "ok",
  };
}

/** Why a checkpoint cannot be restored from, in a sentence, or null. */
export function partialReason(record: CheckpointRecord | undefined | null): string | null {
  if (!record) return null;
  switch (record.outcome) {
    case "before_missing":
      return "The snapshot before this turn did not finish, so these changes cannot be undone.";
    case "after_missing":
      return "The snapshot after this turn did not finish, so what it changed is incomplete.";
    case "unavailable":
      return "This project could not be snapshotted, so nothing was recorded for this turn.";
    default:
      return null;
  }
}

export type SourceLabel = { text: string; tone: "agent" | "child" | "unknown" };

/**
 * Who changed a file, as the list shows it.
 *
 * "Unattributed" is deliberately not "the agent": a file written by a shell
 * script, an MCP server or a second session on the same folder is invisible to
 * every tool call and to the vendor's own report, and calling it the agent's
 * would be a guess dressed as a fact.
 */
export function sourceLabel(file: ChangedFile): SourceLabel {
  switch (file.source) {
    case ChangedSource.SourceAgent:
      return { text: "Agent", tone: "agent" };
    case ChangedSource.SourceChild:
      return { text: "Sub-agent", tone: "child" };
    default:
      return { text: "Unattributed", tone: "unknown" };
  }
}

/**
 * The sentence under an unattributed file, when another session was running in
 * the same project while this turn ran. Null when nothing else was.
 */
export function overlapNote(record: CheckpointRecord | undefined | null): string | null {
  const overlap = parseOverlap(record);
  if (overlap.length === 0) return null;
  const count = overlap.length;
  return count === 1
    ? "Another session was running in this project at the same time."
    : `${count} other sessions were running in this project at the same time.`;
}

type OverlapEntry = { sessionId: string };

function parseOverlap(record: CheckpointRecord | undefined | null): OverlapEntry[] {
  const raw = record?.overlap;
  if (!raw) return [];
  try {
    const parsed = typeof raw === "string" ? JSON.parse(raw) : raw;
    if (!Array.isArray(parsed)) return [];
    return parsed.filter((entry): entry is OverlapEntry => Boolean(entry && typeof entry === "object"));
  } catch {
    // The field is the backend's own JSON; a shape this build cannot read means
    // one fewer sentence, never a broken turn header.
    return [];
  }
}

/** Sorted for the list: changes the agent owns first, then by path. */
export function orderChangedFiles(files: readonly ChangedFile[]): ChangedFile[] {
  const rank = (file: ChangedFile) =>
    file.source === ChangedSource.SourceAgent ? 0 : file.source === ChangedSource.SourceChild ? 1 : 2;
  return [...files].sort((a, b) => rank(a) - rank(b) || a.path.localeCompare(b.path));
}

/** "+12 −3", or null for a file git called binary. */
export function changeCounts(file: ChangedFile): string | null {
  if (file.binary) return null;
  return `+${file.added ?? 0} −${file.removed ?? 0}`;
}
