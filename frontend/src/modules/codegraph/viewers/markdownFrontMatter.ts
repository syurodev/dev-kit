/**
 * A leading `---` … `---` block (YAML front matter) is not Markdown: Plate would read it as a rule, a
 * heading and a rule. It is set aside before the editor sees the file and put back unchanged on save.
 */
const FRONT_MATTER = /^---[ \t]*\n[\s\S]*?\n---[ \t]*(?:\n|$)/;

export function splitFrontMatter(source: string): { front: string; body: string } {
  const m = FRONT_MATTER.exec(source);
  return m ? { front: m[0], body: source.slice(m[0].length) } : { front: "", body: source };
}
