/**
 * Removes the indentation every line of `source` shares.
 *
 * For code cut out of the middle of a file — a method shown on its own still
 * carries the four or eight columns its class put in front of it, which say
 * nothing once the class is not there, and in a narrow pane are columns the
 * code itself needed.
 *
 * The shared indentation is a PREFIX, compared character for character, not a
 * width: a file that mixes tabs and spaces then shares less and loses less,
 * and no line can ever move relative to another. Blank lines say nothing
 * about indentation — an editor strips them, or leaves whatever was there —
 * so they are not asked, and come out empty.
 */
export function dedent(source: string): string {
  const lines = source.split("\n");
  let shared: string | null = null;
  for (const line of lines) {
    if (line.trim() === "") continue;
    const indent = /^[ \t]*/.exec(line)![0];
    shared = shared === null ? indent : commonPrefix(shared, indent);
    if (shared === "") return source;
  }
  if (!shared) return source;
  const width = shared.length;
  return lines.map((line) => (line.trim() === "" ? "" : line.slice(width))).join("\n");
}

function commonPrefix(a: string, b: string): string {
  let i = 0;
  while (i < a.length && i < b.length && a[i] === b[i]) i++;
  return a.slice(0, i);
}
