import type { Change } from "@/bridge/git";
import type { SourcedDiffLine } from "@/lib/diffHunks";
import type { FilePatch } from "./hunks";

const HUNK_START = /^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@/;

/**
 * A file's patch as the lines of a two-pane diff: each one numbered in the file
 * it came from, on either side, from the hunk headers.
 *
 * The numbers are the patch's own rather than a count from 1, so a file with
 * two hunks far apart shows where each one is — the jump in the gutter is the
 * only marker of what was left out between them. Git's "\ No newline" lines are
 * notes about the file, not lines of it, and are dropped.
 */
export function patchLines(patch: FilePatch): SourcedDiffLine[] {
  const out: SourcedDiffLine[] = [];
  for (const hunk of patch.hunks) {
    const start = hunk.header.match(HUNK_START);
    // The gutter shows number + 1, so these are zero-based.
    let before = start ? Number(start[1]) - 1 : 0;
    let after = start ? Number(start[2]) - 1 : 0;
    for (const raw of hunk.lines) {
      const text = raw.replace(/\n$/, "");
      if (raw.startsWith("\\")) continue;
      if (raw.startsWith("+")) {
        out.push({ kind: "added", text: text.slice(1), before: -1, after: after++ });
      } else if (raw.startsWith("-")) {
        out.push({ kind: "removed", text: text.slice(1), before: before++, after: -1 });
      } else {
        out.push({ kind: "same", text: text.slice(1), before: before++, after: after++ });
      }
    }
  }
  return out;
}

/** A commit's message body without its `Task:` trailer, which is drawn as a chip of its own. */
export function commitBody(body: string): { text: string; task: string } {
  let task = "";
  const kept: string[] = [];
  for (const line of body.split("\n")) {
    const trailer = line.match(/^Task: (.+)$/);
    if (trailer) {
      task = trailer[1].trim();
      continue;
    }
    kept.push(line);
  }
  return { text: kept.join("\n").trim(), task };
}

/** "5 files changed · +76 −7", counting only the files git could count: a binary file has no lines to add. */
export function changeSummary(changes: Pick<Change, "added" | "removed">[]): string {
  let added = 0;
  let removed = 0;
  for (const change of changes) {
    if (change.added < 0) continue;
    added += change.added;
    removed += change.removed;
  }
  const files = `${changes.length} file${changes.length === 1 ? "" : "s"} changed`;
  return `${files} · +${added} −${removed}`;
}
