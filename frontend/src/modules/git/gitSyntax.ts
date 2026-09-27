import { useEffect, useState } from "react";
import { HIGHLIGHT_LIMIT, loadParser, tokenizeLines, type SyntaxSegment } from "@/components/code/highlight";
import { languageForPath } from "@/components/code/languages";
import { HUNK_START } from "./commitView";
import type { FilePatch, Hunk } from "./hunks";

export type PatchHighlight = { before: Map<number, SyntaxSegment[]>; after: Map<number, SyntaxSegment[]> };

/**
 * One hunk's own before/after text, each line's diff marker stripped — what a
 * parser needs to see more than one isolated line to colour correctly (a
 * multi-line string, a block comment opened above the change). A hunk is the
 * widest span there is to give it: a git patch carries no full file, only
 * what git chose to show around each change, so a comment opened outside a
 * hunk's own few lines of context still reads as code, not a string.
 */
function hunkSides(hunk: Hunk): { beforeLines: string[]; afterLines: string[]; beforeStart: number; afterStart: number } {
  const start = hunk.header.match(HUNK_START);
  const beforeStart = start ? Number(start[1]) - 1 : 0;
  const afterStart = start ? Number(start[2]) - 1 : 0;
  const beforeLines: string[] = [];
  const afterLines: string[] = [];
  for (const raw of hunk.lines) {
    if (raw.startsWith("\\")) continue;
    const text = raw.replace(/\n$/, "").slice(1);
    if (raw.startsWith("+")) afterLines.push(text);
    else if (raw.startsWith("-")) beforeLines.push(text);
    else {
      beforeLines.push(text);
      afterLines.push(text);
    }
  }
  return { beforeLines, afterLines, beforeStart, afterStart };
}

function patchCharCount(patch: FilePatch): number {
  let total = 0;
  for (const hunk of patch.hunks) for (const line of hunk.lines) total += line.length;
  return total;
}

/**
 * Colours a patch's lines by the file's own grammar, keyed by the absolute
 * line number patchLines already numbers each row with — so a caller looks
 * a row's segments up the same way FileDiffPane looks up a whole file's.
 *
 * Each hunk is tokenized on its own rather than the whole patch concatenated:
 * two hunks far apart in the same file are not adjacent code, and colouring
 * them as one block could open a string or a comment in one that only closes
 * in the other.
 */
export async function highlightPatch(patch: FilePatch, path: string): Promise<PatchHighlight | null> {
  const language = languageForPath(path);
  if (!language || patch.binary || patchCharCount(patch) > HIGHLIGHT_LIMIT) return null;
  const parser = await loadParser(language);
  const before = new Map<number, SyntaxSegment[]>();
  const after = new Map<number, SyntaxSegment[]>();
  for (const hunk of patch.hunks) {
    const { beforeLines, afterLines, beforeStart, afterStart } = hunkSides(hunk);
    tokenizeLines(beforeLines.join("\n"), parser).forEach((segments, i) => before.set(beforeStart + i, segments));
    tokenizeLines(afterLines.join("\n"), parser).forEach((segments, i) => after.set(afterStart + i, segments));
  }
  return { before, after };
}

/**
 * highlightPatch, as a hook: null until the parser has loaded (the plain diff
 * is already on screen by then) or when there is nothing to colour.
 */
export function usePatchHighlight(patch: FilePatch | null, path: string): PatchHighlight | null {
  const [highlight, setHighlight] = useState<PatchHighlight | null>(null);

  useEffect(() => {
    setHighlight(null);
    if (!patch) return;
    let cancelled = false;
    highlightPatch(patch, path)
      .then((result) => {
        if (!cancelled) setHighlight(result);
      })
      .catch(() => {
        // No grammar for this file, or it failed to load: the plain diff stands.
      });
    return () => {
      cancelled = true;
    };
  }, [patch, path]);

  return highlight;
}
