/**
 * THIRD_PARTY_LICENSES.md is generated for GitHub readers (headings, a table, fenced
 * texts). Settings shows it as the plain notices people expect from an "Open source
 * licenses" page: no code fences, no markdown heading marks, and without the file's own
 * title (the page already has one). The summary table reads as "MIT — 459" lines and
 * inline `code` / **bold** lose their marks.
 */
export function readableLicenses(markdown: string): string {
  const lines = markdown.replace(/\r\n/g, "\n").split("\n");
  const out: string[] = [];
  let skippedTitle = false;
  for (const line of lines) {
    if (/^```/.test(line)) continue;
    if (/^\|\s*-+/.test(line)) continue;
    const row = /^\|\s*(.*?)\s*\|\s*(.*?)\s*\|$/.exec(line);
    if (row) {
      if (row[1] !== "License") out.push(`${row[1]} — ${row[2]}`);
      continue;
    }
    const heading = /^(#{1,6})\s+(.*)$/.exec(line);
    if (heading) {
      if (heading[1] === "#" && !skippedTitle) {
        skippedTitle = true;
        continue;
      }
      out.push(heading[1] === "##" ? heading[2].toUpperCase() : heading[2]);
      continue;
    }
    out.push(line.replace(/\*\*(.+?)\*\*/g, "$1").replace(/`([^`]+)`/g, "$1"));
  }
  return out.join("\n").replace(/^\n+/, "").replace(/\n{4,}/g, "\n\n\n").trimEnd();
}
