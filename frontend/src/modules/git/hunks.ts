/**
 * Reading a unified diff into hunks, and writing a subset of them back.
 *
 * This is the only risky part of staging by hunk: the backend hands the patch
 * to `git apply --cached`, which either applies it or refuses it, so a patch
 * assembled wrongly here is a confusing error rather than a wrong commit.
 *
 * The rule that keeps it honest is that nothing is rewritten. A hunk's `@@`
 * header and its lines go back out byte for byte, under the same file header
 * they arrived with. Line numbers in the remaining headers still describe the
 * index the patch was measured against, which is what `git add -p` relies on
 * too — it feeds whole hunks to the same command.
 */

export type Hunk = {
  /** The `@@ -a,b +c,d @@ …` line, verbatim. */
  header: string;
  /** Every line of the hunk after its header, verbatim and including "\". */
  lines: string[];
  added: number;
  removed: number;
};

export type FilePatch = {
  /** `diff --git`, `index`, `---`, `+++` — everything before the first hunk. */
  head: string[];
  hunks: Hunk[];
  /**
   * True when git said the file is binary. There are no hunks to pick and the
   * caller stages the whole file or none of it.
   */
  binary: boolean;
};

const HUNK = /^@@ -\d+(,\d+)? \+\d+(,\d+)? @@/;

/**
 * Splits on newlines and KEEPS them, so joining the pieces returns the input
 * byte for byte. Written out rather than `split(/(?<=\n)/)` because lookbehind
 * needs WebKit 16.4, and the webview here is whatever the OS ships.
 */
function lines(text: string): string[] {
  const out: string[] = [];
  let start = 0;
  for (let i = 0; i < text.length; i++) {
    if (text[i] !== "\n") continue;
    out.push(text.slice(start, i + 1));
    start = i + 1;
  }
  // A patch's last line may have no newline of its own.
  if (start < text.length) out.push(text.slice(start));
  return out;
}

/**
 * Splits a patch into its file header and its hunks.
 *
 * An empty patch — an untracked file, or one git declined to diff — comes back
 * with no hunks rather than as an error: there is simply nothing to pick.
 */
export function parsePatch(text: string): FilePatch {
  const out: FilePatch = { head: [], hunks: [], binary: false };
  if (!text) return out;

  let current: Hunk | null = null;

  for (const line of lines(text)) {
    if (HUNK.test(line)) {
      current = { header: line, lines: [], added: 0, removed: 0 };
      out.hunks.push(current);
      continue;
    }
    if (!current) {
      if (line.startsWith("Binary files ") || line.startsWith("GIT binary patch")) {
        out.binary = true;
      }
      out.head.push(line);
      continue;
    }
    current.lines.push(line);
    // "+++"/"---" never reach here (they are head), so a single + or - is a
    // real change. "\ No newline at end of file" counts as neither.
    if (line.startsWith("+")) current.added++;
    else if (line.startsWith("-")) current.removed++;
  }
  return out;
}

/**
 * Rebuilds a patch containing only the chosen hunks.
 *
 * Choosing none returns an empty string rather than a header with no hunks:
 * git apply rejects that, and "nothing selected" is not an error to show.
 */
export function buildPatch(patch: FilePatch, selected: readonly number[]): string {
  const keep = [...new Set(selected)].filter((i) => i >= 0 && i < patch.hunks.length).sort((a, b) => a - b);
  if (keep.length === 0) return "";

  const parts = [...patch.head];
  for (const index of keep) {
    const hunk = patch.hunks[index];
    parts.push(hunk.header, ...hunk.lines);
  }
  const text = parts.join("");
  // git apply refuses a patch whose last line has no newline with a message
  // about corrupt input, which reads as though the hunk is wrong.
  return text.endsWith("\n") ? text : text + "\n";
}
