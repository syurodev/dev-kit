/**
 * The markdown Agent / Message Rich (Figma `1060:3171`) draws, read by
 * `marked`'s lexer — GFM, the dialect agents write. Only the lexer is used:
 * its tokens become the small tree below and React renders that, so no HTML
 * string is ever built and nothing an agent writes reaches the DOM as markup.
 *
 * This replaced a line-based reader written here, which missed what a parser
 * gets for free — a table right after a sentence, nested lists, `~~~` fences,
 * setext headings, escapes — and was patched one agent answer at a time.
 * `marked` is what VS Code's chat and Vercel's Streamdown lex with, and the
 * lightest real GFM parser (~12 KB gzip against ~31 KB for micromark/mdast).
 */
import { Lexer, type Token, type Tokens } from "marked";

export type InlineToken =
  | { type: "text"; value: string }
  | { type: "code"; value: string }
  | { type: "secret"; value: string }
  | { type: "break" }
  | { type: "strong" | "em" | "del"; children: InlineToken[] }
  | { type: "link"; href: string; children: InlineToken[] };

/** A list item's content; `checked` is set only on a task item (`- [ ]`). */
export type ListItem = { checked?: boolean; blocks: MarkdownBlock[] };

export type MarkdownBlock =
  | { type: "heading"; level: number; inline: InlineToken[] }
  | { type: "paragraph"; inline: InlineToken[] }
  | { type: "list"; ordered: boolean; start: number; items: ListItem[] }
  | { type: "quote"; blocks: MarkdownBlock[] }
  | { type: "code"; language?: string; code: string }
  | { type: "table"; head: InlineToken[][]; rows: InlineToken[][][]; align: TableAlign[] }
  | { type: "rule" };

/** A column's alignment, as its divider cell spells it: `:---`, `:---:`, `---:`. */
export type TableAlign = "left" | "center" | "right";

export type IncrementalMarkdownState = {
  source: string;
  blocks: MarkdownBlock[];
  /** The prefix ends at a block boundary more text cannot move; only the suffix is provisional. */
  closedPrefixLength: number;
  closedBlocks: MarkdownBlock[];
  blockKeys: string[];
  closedKeys: string[];
};

let nextBlockKey = 0;

function newBlockKey(): string {
  nextBlockKey += 1;
  return `agent-block-${nextBlockKey}`;
}

/**
 * marked's block and inline rules, made sticky. Each rule is `^`-anchored
 * and tried against everything not yet lexed; V8 fails a non-matching
 * anchored rule at once, but JavaScriptCore — the engine in WKWebView and
 * WebKitGTK, so every macOS and Linux build of this app — scans the rest of
 * the input first. That made lexing quadratic there: a 20 KB answer took
 * ~90 ms, 40 KB of prose ~900 ms, against ~6 ms in V8. The sticky flag pins a
 * match to where the lexer stands, which is what `^` already meant, and
 * brings JSC back to linear. `exec` resets `lastIndex` because marked reuses
 * each rule on a new, shorter string, never on the rest of the old one.
 */
class AnchoredRule extends RegExp {
  // split and matchAll build their own copy through the species; a plain
  // RegExp keeps that copy's flags exactly what they asked for.
  static override get [Symbol.species]() {
    return RegExp;
  }

  constructor(rule: RegExp) {
    super(rule.source, `${rule.flags}y`);
  }

  override exec(input: string): RegExpExecArray | null {
    this.lastIndex = 0;
    return super.exec(input);
  }
}

type Rules = Record<string, unknown>;
const stickyRules = new Map<string, Rules>();

/** A rule set with every `^`-anchored rule made sticky, built once per set. */
function sticky(kind: string, rules: Rules): Rules {
  let out = stickyRules.get(kind);
  if (!out) {
    out = Object.fromEntries(
      Object.entries(rules).map(([name, rule]) => [
        name,
        // A global rule is walked with lastIndex on purpose; leave it be.
        rule instanceof RegExp && rule.source.startsWith("^") && !rule.global && !rule.sticky
          ? new AnchoredRule(rule)
          : rule,
      ]),
    );
    stickyRules.set(kind, out);
  }
  return out;
}

/** The lexer every parse here uses: GFM, with the anchored rules made sticky. */
export function newLexer(): Lexer {
  const lexer = new Lexer({ gfm: true });
  // The rule set belongs to this lexer (marked builds it per instance), so
  // swapping it touches no other user of marked.
  const rules = (lexer as unknown as { tokenizer: { rules: { block: Rules; inline: Rules } } }).tokenizer.rules;
  rules.block = sticky("block", rules.block);
  rules.inline = sticky("inline", rules.inline);
  return lexer;
}

function lex(source: string): Token[] {
  return newLexer().lex(source);
}

// ─── Inline ────────────────────────────────────────────────────────────────

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

/**
 * Where a link may go. The href is the agent's to write, so a `javascript:`
 * or `data:` one is drawn as its text instead of as something to click.
 */
function safeHref(href: string): boolean {
  const scheme = href.replace(/[\u0000-\u0020]/g, "").toLowerCase();
  return !/^(javascript|vbscript|data):/.test(scheme);
}

/**
 * marked's inline tokens as the tree the renderer draws. Adjacent text is
 * joined before the JWT mask looks at it: marked ends a text run at every
 * `_`, and a token's signature is full of them.
 */
function inlineFrom(tokens: Token[] | undefined): InlineToken[] {
  const out: InlineToken[] = [];
  let text = "";
  const flush = () => {
    if (text) out.push(...splitSecrets(text));
    text = "";
  };

  for (const token of tokens ?? []) {
    switch (token.type) {
      case "text":
        // A tight list item's text carries its own inline tokens.
        if ((token as Tokens.Text).tokens?.length) {
          flush();
          out.push(...inlineFrom((token as Tokens.Text).tokens));
        } else {
          text += (token as Tokens.Text).text;
        }
        break;
      case "escape":
        text += (token as Tokens.Escape).text;
        break;
      // Inline HTML is shown as written, not interpreted.
      case "html":
        text += token.raw;
        break;
      case "codespan": {
        flush();
        const value = (token as Tokens.Codespan).text;
        // A JWT reads as a secret whether it landed in prose or alone inside
        // a backtick span.
        out.push(JWT_WHOLE.test(value) ? { type: "secret", value } : { type: "code", value });
        break;
      }
      case "br":
        flush();
        out.push({ type: "break" });
        break;
      case "strong":
      case "em":
      case "del":
        flush();
        out.push({ type: token.type, children: inlineFrom((token as Tokens.Strong).tokens) });
        break;
      case "link": {
        const link = token as Tokens.Link;
        const children = inlineFrom(link.tokens);
        flush();
        if (safeHref(link.href)) out.push({ type: "link", href: link.href, children });
        else out.push(...children);
        break;
      }
      // An image is a link to it. Loading it would fetch a URL the agent
      // chose, from inside the app, the moment the answer streams in.
      case "image": {
        const image = token as Tokens.Image;
        flush();
        const children: InlineToken[] = [{ type: "text", value: image.text || image.href }];
        if (safeHref(image.href)) out.push({ type: "link", href: image.href, children });
        else out.push(...children);
        break;
      }
      case "checkbox":
        break;
      default:
        text += token.raw;
    }
  }
  flush();
  return out;
}

/** Inline markdown alone — backticks, emphasis, links — with no block structure. */
export function parseInline(text: string): InlineToken[] {
  return inlineFrom(newLexer().inlineTokens(text));
}

// ─── Blocks ────────────────────────────────────────────────────────────────

function blocksFrom(tokens: Token[]): MarkdownBlock[] {
  const out: MarkdownBlock[] = [];
  for (const token of tokens) {
    const block = blockFrom(token);
    if (block) out.push(block);
  }
  return out;
}

function blockFrom(token: Token): MarkdownBlock | null {
  switch (token.type) {
    case "heading": {
      const heading = token as Tokens.Heading;
      return { type: "heading", level: heading.depth, inline: inlineFrom(heading.tokens) };
    }
    case "paragraph":
    case "text":
      return { type: "paragraph", inline: inlineFrom((token as Tokens.Paragraph).tokens) };
    case "list": {
      const list = token as Tokens.List;
      return {
        type: "list",
        ordered: list.ordered,
        start: typeof list.start === "number" ? list.start : 1,
        items: list.items.map((item) => ({
          ...(item.task ? { checked: Boolean(item.checked) } : {}),
          blocks: blocksFrom(item.tokens),
        })),
      };
    }
    case "blockquote":
      return { type: "quote", blocks: blocksFrom((token as Tokens.Blockquote).tokens) };
    case "code": {
      const code = token as Tokens.Code;
      // The info string's first word; `ts {1-3}` is still TypeScript.
      const language = code.lang?.trim().split(/\s+/)[0];
      return { type: "code", language: language || undefined, code: code.text };
    }
    case "table": {
      const table = token as Tokens.Table;
      return {
        type: "table",
        head: table.header.map((cell) => inlineFrom(cell.tokens)),
        rows: table.rows.map((row) => row.map((cell) => inlineFrom(cell.tokens))),
        align: table.align.map((align) => align ?? "left"),
      };
    }
    case "hr":
      return { type: "rule" };
    // HTML is shown as written, not interpreted.
    case "html": {
      const value = token.raw.trim();
      return value ? { type: "paragraph", inline: [{ type: "text", value }] } : null;
    }
    // Blank lines and reference definitions draw nothing.
    default:
      return null;
  }
}

export function parseMarkdown(source: string): MarkdownBlock[] {
  return blocksFrom(lex(source.replace(/\r\n?/g, "\n")));
}

// ─── Streaming ─────────────────────────────────────────────────────────────

/**
 * Parses a growing answer by keeping every block but the last. More text can
 * only change the block it lands in — a paragraph that turns into a table, a
 * list that gains an item, a fence that closes — so every block before the
 * last one is final, and only the tail from the last block's start is lexed
 * again. It is the same split Streamdown makes.
 *
 * One thing it gives up: a reference link (`[x]` defined by a later `[x]: url`)
 * in a block closed before the definition arrived stays text. Agents write
 * inline links.
 */
export function parseMarkdownIncremental(
  source: string,
  previous?: IncrementalMarkdownState,
): IncrementalMarkdownState {
  const normalized = source.replace(/\r\n?/g, "\n");
  const canExtend = previous !== undefined && normalized.startsWith(previous.source);
  const start = canExtend ? previous.closedPrefixLength : 0;

  const tokens = lex(normalized.slice(start));
  // Blank lines after the last block are part of it: they may still turn out
  // to be a loose list's gap.
  let last = tokens.length - 1;
  while (last > 0 && tokens[last].type === "space") last -= 1;
  let closedLength = 0;
  for (let index = 0; index < last; index += 1) closedLength += tokens[index].raw.length;

  const newlyClosed = blocksFrom(tokens.slice(0, Math.max(last, 0)));
  const tail = blocksFrom(tokens.slice(Math.max(last, 0)));

  // Blocks keep their key where they are still the same kind of block at the
  // same place: the one being written stays mounted while it grows and when
  // the next one starts.
  const oldClosedCount = canExtend ? previous.closedBlocks.length : 0;
  const oldBlocks = canExtend ? previous.blocks.slice(oldClosedCount) : [];
  const oldKeys = canExtend ? previous.blockKeys.slice(oldClosedCount) : [];
  const keys = [...newlyClosed, ...tail].map((block, index) =>
    oldBlocks[index]?.type === block.type && oldKeys[index] ? oldKeys[index] : newBlockKey(),
  );

  const closedBlocks = canExtend ? [...previous.closedBlocks, ...newlyClosed] : newlyClosed;
  const closedKeys = [...(canExtend ? previous.closedKeys : []), ...keys.slice(0, newlyClosed.length)];
  return {
    source: normalized,
    blocks: [...closedBlocks, ...tail],
    closedPrefixLength: start + closedLength,
    closedBlocks,
    blockKeys: [...closedKeys, ...keys.slice(newlyClosed.length)],
    closedKeys,
  };
}
