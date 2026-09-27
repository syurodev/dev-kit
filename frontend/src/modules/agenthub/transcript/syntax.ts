/**
 * What the transcript needs on top of the shared syntax registry
 * (components/code): reading a tool's listing back into code.
 *
 * The grammars, the tokenizer and the highlight hook used to live here and
 * were imported by the diff views and Git as well; they are
 * components/code/{languages,highlight,CodeBlock} now, shared with every
 * surface that shows code.
 */

const FENCE_OPEN = /^```([\w+#.-]*)\s*$/;
const FENCE_CLOSE = /^```\s*$/;
const NUMBERED = /^\s*(\d+)(?:\t|→)(.*)$/;

/**
 * A tool's text as the listing it is. Claude's Read answers with the file
 * inside a markdown fence, each line prefixed with its number and a tab:
 * "```\n1\tpackage …\n2\t\n…\n```". Shown as is, that put the fence marks
 * and the numbers into the code. Returns the code alone, the first line's
 * number when every line is numbered in sequence, and the fence's info string;
 * null when the text is neither fenced nor numbered.
 */
export function parseListing(text: string): { code: string; startLine?: number; info?: string } | null {
  let lines = text.replace(/\r\n/g, "\n").split("\n");
  let info: string | undefined;
  const open = FENCE_OPEN.exec(lines[0] ?? "");
  if (open) {
    info = open[1] || undefined;
    lines = lines.slice(1);
    while (lines.length > 0 && lines[lines.length - 1].trim() === "") lines.pop();
    if (lines.length > 0 && FENCE_CLOSE.test(lines[lines.length - 1])) lines.pop();
  }

  const matches = lines.map((line) => NUMBERED.exec(line));
  const numbered =
    lines.length > 0 &&
    matches.every((match, index) => match && Number(match[1]) === Number(matches[0]![1]) + index);
  if (numbered) {
    return { code: matches.map((match) => match![2]).join("\n"), startLine: Number(matches[0]![1]), info };
  }
  return open ? { code: lines.join("\n"), info } : null;
}
