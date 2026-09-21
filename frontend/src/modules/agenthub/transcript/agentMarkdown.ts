/**
 * The markdown Agent / Message Rich (Figma `1060:3171`) actually draws:
 * heading, paragraph, bulleted list, numbered list, blockquote, table, plus
 * inline code, links and emphasis. Fenced code is routed to Agent / Code Block.
 *
 * This exists because agent messages arrive as markdown and the transcript was
 * printing the source — readers saw literal `- ` bullets and `**` pairs. No
 * markdown library is a declared dependency of this app, and the module is not
 * allowed to add one, so this reads exactly the blocks the design has.
 *
 * ponytail: line-based, not a real parser — no nested lists, no inline HTML,
 * no reference links, no setext headings. Swap in a proper markdown pipeline if
 * agents start sending any of that.
 */

export type InlineToken =
  | { type: "text"; value: string }
  | { type: "code"; value: string }
  | { type: "secret"; value: string }
  | { type: "strong"; value: string }
  | { type: "em"; value: string }
  | { type: "link"; value: string; href: string };

export type MarkdownBlock =
  | { type: "heading"; level: number; text: string }
  | { type: "paragraph"; text: string }
  | { type: "list"; ordered: boolean; items: string[] }
  | { type: "quote"; text: string }
  | { type: "code"; language?: string; code: string }
  | { type: "table"; head: string[]; rows: string[][]; align: TableAlign[] };

export type IncrementalMarkdownState = {
  source: string;
  blocks: MarkdownBlock[];
  /** The prefix ends at a safe block boundary; only the suffix is provisional. */
  closedPrefixLength: number;
  closedBlocks: MarkdownBlock[];
  blockKeys: string[];
  closedKeys: string[];
};

/** A column's alignment, as its divider cell spells it: `:---`, `:---:`, `---:`. */
export type TableAlign = "left" | "center" | "right";

function alignOf(divider: string): TableAlign {
  const cell = divider.trim();
  const left = cell.startsWith(":");
  const right = cell.endsWith(":");
  return left && right ? "center" : right ? "right" : "left";
}

const HEADING = /^(#{1,6})\s+(.*)$/;
const BULLET = /^\s*[-*+]\s+(.*)$/;
const ORDERED = /^\s*\d+[.)]\s+(.*)$/;
const FENCE = /^```(\w*)\s*$/;
const TABLE_DIVIDER = /^\s*\|?[\s:-]*-[\s|:-]*\|?\s*$/;
let nextBlockKey = 0;

function newBlockKey(): string {
  nextBlockKey += 1;
  return `agent-block-${nextBlockKey}`;
}

/**
 * Find the longest prefix that cannot be changed by more incoming text,
 * scanning on from `from`, a boundary found earlier. Only lines that already
 * have their newline count: an unfinished "```" can still grow into "````" or
 * "```j", and a boundary taken on it was later withdrawn or cut a line in two.
 * A boundary is never inside a fence, so the scan resumes outside one.
 */
function closedPrefixOf(source: string, from: number): number {
  let closed = from;
  let inFence = false;
  let start = from;
  for (let end = source.indexOf("\n", start); end !== -1; end = source.indexOf("\n", start)) {
    const line = source.slice(start, end);
    if (FENCE.test(line)) {
      if (inFence) closed = end + 1;
      inFence = !inFence;
    } else if (!inFence && !line.trim()) {
      closed = end + 1;
    }
    start = end + 1;
  }
  return closed;
}

function tailKeys(blocks: MarkdownBlock[], previousBlocks: MarkdownBlock[], previousKeys: string[]): string[] {
  return blocks.map((block, index) =>
    previousBlocks[index]?.type === block.type && previousKeys[index] ? previousKeys[index] : newBlockKey(),
  );
}

function splitRow(line: string): string[] {
  return line
    .replace(/^\s*\|/, "")
    .replace(/\|\s*$/, "")
    .split("|")
    .map((cell) => cell.trim());
}

export function parseMarkdown(source: string): MarkdownBlock[] {
  const lines = source.replace(/\r\n?/g, "\n").split("\n");
  const blocks: MarkdownBlock[] = [];
  let i = 0;

  while (i < lines.length) {
    const line = lines[i];

    if (!line.trim()) {
      i += 1;
      continue;
    }

    const fence = FENCE.exec(line);
    if (fence) {
      const code: string[] = [];
      i += 1;
      while (i < lines.length && !FENCE.test(lines[i])) {
        code.push(lines[i]);
        i += 1;
      }
      i += 1; // closing fence, or end of input
      blocks.push({
        type: "code",
        language: fence[1] || undefined,
        code: code.join("\n"),
      });
      continue;
    }

    const heading = HEADING.exec(line);
    if (heading) {
      blocks.push({ type: "heading", level: heading[1].length, text: heading[2] });
      i += 1;
      continue;
    }

    // A table needs its divider row; without one these are just paragraphs
    // containing pipes, which is what a shell command looks like.
    if (line.includes("|") && i + 1 < lines.length && TABLE_DIVIDER.test(lines[i + 1])) {
      const head = splitRow(line);
      // The divider was only a marker before; it also says how each column
      // aligns, and a column of numbers is right-aligned so its digits line up.
      const align = splitRow(lines[i + 1]).map(alignOf);
      const rows: string[][] = [];
      i += 2;
      while (i < lines.length && lines[i].includes("|") && lines[i].trim()) {
        rows.push(splitRow(lines[i]));
        i += 1;
      }
      blocks.push({ type: "table", head, rows, align });
      continue;
    }

    if (BULLET.test(line) || ORDERED.test(line)) {
      const ordered = !BULLET.test(line);
      const items: string[] = [];
      while (i < lines.length) {
        const match = ordered ? ORDERED.exec(lines[i]) : BULLET.exec(lines[i]);
        if (!match) break;
        items.push(match[1]);
        i += 1;
      }
      blocks.push({ type: "list", ordered, items });
      continue;
    }

    if (line.trimStart().startsWith(">")) {
      const quoted: string[] = [];
      while (i < lines.length && lines[i].trimStart().startsWith(">")) {
        quoted.push(lines[i].trimStart().replace(/^>\s?/, ""));
        i += 1;
      }
      blocks.push({ type: "quote", text: quoted.join("\n") });
      continue;
    }

    // Everything else is a paragraph, joined until a blank line so a wrapped
    // sentence does not become three paragraphs.
    const paragraph: string[] = [];
    while (
      i < lines.length &&
      lines[i].trim() &&
      !HEADING.test(lines[i]) &&
      !BULLET.test(lines[i]) &&
      !ORDERED.test(lines[i]) &&
      !FENCE.test(lines[i]) &&
      !lines[i].trimStart().startsWith(">")
    ) {
      paragraph.push(lines[i].trim());
      i += 1;
    }
    blocks.push({ type: "paragraph", text: paragraph.join(" ") });
  }

  return blocks;
}

/**
 * Parses a growing answer by retaining blocks before the last safe boundary.
 * A code fence only becomes safe at its closing fence; blank lines are safe
 * for the line-based blocks this renderer supports. The remaining tail is
 * deliberately reparsed because it may still become a table, list, or fence.
 */
export function parseMarkdownIncremental(
  source: string,
  previous?: IncrementalMarkdownState,
): IncrementalMarkdownState {
  const normalized = source.replace(/\r\n?/g, "\n");
  const canExtend = previous && normalized.startsWith(previous.source);
  // Resuming from the previous boundary keeps each token's scan to the tail,
  // and means the closed prefix can only grow while the text only grows.
  const closedPrefixLength = closedPrefixOf(normalized, canExtend ? previous.closedPrefixLength : 0);

  if (!canExtend) {
    const closedBlocks = parseMarkdown(normalized.slice(0, closedPrefixLength));
    const tailBlocks = parseMarkdown(normalized.slice(closedPrefixLength));
    const closedKeys = closedBlocks.map(() => newBlockKey());
    const tailBlockKeys = tailBlocks.map(() => newBlockKey());
    return {
      source: normalized,
      blocks: [...closedBlocks, ...tailBlocks],
      closedPrefixLength,
      closedBlocks,
      blockKeys: [...closedKeys, ...tailBlockKeys],
      closedKeys,
    };
  }

  const oldClosedCount = previous.closedBlocks.length;
  const oldTailBlocks = previous.blocks.slice(oldClosedCount);
  const oldTailKeys = previous.blockKeys.slice(oldClosedCount);
  let closedBlocks = previous.closedBlocks;
  let closedKeys = previous.closedKeys;

  if (closedPrefixLength > previous.closedPrefixLength) {
    const newlyClosed = parseMarkdown(normalized.slice(previous.closedPrefixLength, closedPrefixLength));
    const newlyClosedKeys = newlyClosed.map((block, index) => {
      const old = oldTailBlocks[index];
      return old?.type === block.type && oldTailKeys[index] ? oldTailKeys[index] : newBlockKey();
    });
    closedBlocks = [...closedBlocks, ...newlyClosed];
    closedKeys = [...closedKeys, ...newlyClosedKeys];
  }

  const tailStart = closedPrefixLength;
  const tailBlocks = parseMarkdown(normalized.slice(tailStart));
  const tailBlockKeys =
    closedPrefixLength > previous.closedPrefixLength
      ? tailBlocks.map(() => newBlockKey())
      : tailKeys(tailBlocks, oldTailBlocks, oldTailKeys);
  return {
    source: normalized,
    blocks: [...closedBlocks, ...tailBlocks],
    closedPrefixLength,
    closedBlocks,
    blockKeys: [...closedKeys, ...tailBlockKeys],
    closedKeys,
  };
}

// Underscore emphasis excludes intraword underscores (CommonMark's own rule,
// for exactly this reason): without the lookaround, `program_config_auth` —
// ordinary in a comment about a database column — reads as `program`,
// <em>config</em>, `auth`, its two underscores silently eaten. `*asterisks*`
// carry no such requirement; identifiers do not use them for word joints.
const INLINE =
  /(`[^`]+`)|(\[[^\]]+\]\([^)\s]+\))|(\*\*[^*]+\*\*)|(\*[^*]+\*|(?<!\w)_[^_]+_(?!\w))/g;

/**
 * A JSON Web Token, wherever it shows up in an agent's own words — pasted as
 * a sample, quoted from a header, read back out of a config. "eyJ" is the
 * base64 of `{"`, which every JWT header starts with (`{"alg":...,"typ":...}`
 * always begins with `{"`), so it is a near-zero-false-positive anchor; three
 * base64url segments joined by dots is the rest of the shape. Segments keep
 * their own minimum length so "eyJ" alone, mid-sentence, is not one.
 */
const JWT_SOURCE = String.raw`\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]{5,}\b`;
const JWT = new RegExp(JWT_SOURCE, "g");
const JWT_WHOLE = new RegExp(`^${JWT_SOURCE}$`);

/** A run of plain text, split around any JWT it carries. */
function splitSecrets(value: string): InlineToken[] {
  const parts: InlineToken[] = [];
  let last = 0;
  for (const match of value.matchAll(JWT)) {
    const start = match.index ?? 0;
    if (start > last) parts.push({ type: "text", value: value.slice(last, start) });
    parts.push({ type: "secret", value: match[0] });
    last = start + match[0].length;
  }
  if (parts.length === 0) return [{ type: "text", value }];
  if (last < value.length) parts.push({ type: "text", value: value.slice(last) });
  return parts;
}

export function parseInline(text: string): InlineToken[] {
  const tokens: InlineToken[] = [];
  let last = 0;

  for (const match of text.matchAll(INLINE)) {
    const start = match.index ?? 0;
    if (start > last) tokens.push({ type: "text", value: text.slice(last, start) });
    const raw = match[0];

    if (raw.startsWith("`")) {
      tokens.push({ type: "code", value: raw.slice(1, -1) });
    } else if (raw.startsWith("[")) {
      const split = raw.indexOf("](");
      tokens.push({
        type: "link",
        value: raw.slice(1, split),
        href: raw.slice(split + 2, -1),
      });
    } else if (raw.startsWith("**")) {
      tokens.push({ type: "strong", value: raw.slice(2, -2) });
    } else {
      tokens.push({ type: "em", value: raw.slice(1, -1) });
    }
    last = start + raw.length;
  }

  if (last < text.length) tokens.push({ type: "text", value: text.slice(last) });

  // A JWT reads as a secret whether it landed in plain prose or alone inside
  // a backtick span — `parseInline`'s own caller draws either the same way,
  // so this is the one place both need to pass through the mask.
  const expanded: InlineToken[] = [];
  for (const token of tokens) {
    if (token.type === "text") expanded.push(...splitSecrets(token.value));
    else if (token.type === "code" && JWT_WHOLE.test(token.value)) expanded.push({ type: "secret", value: token.value });
    else expanded.push(token);
  }
  return expanded;
}
