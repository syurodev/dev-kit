import { fileRef } from "./fileRef";
import type { InlineToken, MarkdownBlock } from "./agentMarkdown";

/** A symbol found in the workspace's indexes, ready to open and preview. */
export type SymbolHit = {
  workspaceId: string;
  folderId: string;
  folderName: string;
  /** Folder-relative. */
  filePath: string;
  line: number;
  kind: string;
  label: string;
};

/** Names looked up per message; past this, a span stays code. */
export const MAX_SYMBOLS_PER_MESSAGE = 12;

const SYMBOL = /^[A-Za-z_$][\w$]*(\.[A-Za-z_$][\w$]*)*(\(\))?$/;
// Words of the languages an agent writes about that read like identifiers but name nothing in an index.
const RESERVED = new Set([
  "null", "NULL", "nil", "None", "undefined", "true", "True", "TRUE", "false", "False", "FALSE",
  "this", "self", "Self", "super", "return", "new", "var", "let", "const", "function", "func", "def",
  "class", "struct", "interface", "type", "enum", "import", "export", "package", "async", "await",
  "void", "static", "public", "private", "protected", "if", "else", "elif", "for", "while", "switch",
  "case", "break", "continue", "default", "try", "catch", "finally", "throw", "throws", "typeof",
  "instanceof", "yield", "defer", "range", "chan", "select", "string", "int", "bool", "boolean",
  "number", "any", "unknown", "never", "object", "float", "double", "char", "byte", "long", "short",
  "NaN", "Infinity", "local", "end", "then", "and", "not",
]);

const withoutCall = (text: string) => (text.endsWith("()") ? text.slice(0, -2) : text);

/** The name after the last dot, without a trailing `()`. */
export function lastSegment(text: string): string {
  const name = withoutCall(text);
  return name.slice(name.lastIndexOf(".") + 1);
}

/** True when an inline code span reads as an identifier worth looking up (spec §6). */
export function symbolCandidate(text: string): boolean {
  if (text.length < 3 || text.length > 80) return false;
  if (!SYMBOL.test(text)) return false;
  return !RESERVED.has(withoutCall(text));
}

/** True when an inline code span reads as an abbreviated or full commit hash. */
export function commitCandidate(text: string): boolean {
  return /^[0-9a-f]{7,40}$/.test(text) && /[0-9]/.test(text) && /[a-f]/.test(text);
}

/**
 * The one match that names exactly what the span says, or null. Exact means
 * the label equals the span, or its last segment equals the span's and the
 * span's qualifier ends the label's qualifier (`Service.Invoke` is
 * `agenthub.Service.Invoke`, never `MyService.Invoke`). Two such matches are
 * ambiguous: a wrong link is worse than none.
 */
export function exactSymbolMatch<M extends { label: string }>(span: string, matches: readonly M[]): M | null {
  const name = withoutCall(span);
  const cut = name.lastIndexOf(".");
  const last = name.slice(cut + 1);
  const qualifier = cut < 0 ? "" : name.slice(0, cut);
  const hits = matches.filter((match) => {
    // A label may carry its parameters: `invoke(String)`.
    const label = match.label.replace(/\(.*\)$/, "");
    if (label === name) return true;
    const labelCut = label.lastIndexOf(".");
    if (label.slice(labelCut + 1) !== last) return false;
    const labelQualifier = labelCut < 0 ? "" : label.slice(0, labelCut);
    return qualifier === "" || labelQualifier === qualifier || labelQualifier.endsWith("." + qualifier);
  });
  return hits.length === 1 ? hits[0] : null;
}

function inlineSpans(tokens: readonly InlineToken[], out: string[]) {
  for (const token of tokens) {
    if (token.type === "code") out.push(token.value);
    // Code inside a link is the link's text: it opens what the link points at.
    else if (token.type !== "link" && "children" in token) inlineSpans(token.children, out);
  }
}

function blockSpans(blocks: readonly MarkdownBlock[], out: string[]) {
  for (const block of blocks) {
    switch (block.type) {
      case "heading":
      case "paragraph":
        inlineSpans(block.inline, out);
        break;
      case "list":
        for (const item of block.items) blockSpans(item.blocks, out);
        break;
      case "quote":
        blockSpans(block.blocks, out);
        break;
      case "table":
        for (const cell of block.head) inlineSpans(cell, out);
        for (const row of block.rows) for (const cell of row) inlineSpans(cell, out);
        break;
    }
  }
}

/** Every inline code span of a message, in reading order. */
export function codeSpans(blocks: readonly MarkdownBlock[]): string[] {
  const out: string[] = [];
  blockSpans(blocks, out);
  return out;
}

/**
 * Whether a candidate says enough to be worth asking the index about. A bare
 * short word (`get`, `run`, `Add`) matches half a codebase and names none of
 * it; a qualified name, a call, a camel-cased name or four letters do.
 */
export function worthLookingUp(name: string): boolean {
  return lastSegment(name).length >= 4 || name.includes(".") || name.endsWith("()") || /[a-z0-9][A-Z]/.test(name);
}

/**
 * The distinct names worth a symbol lookup, at most 12: not a file (that is a
 * file reference), not a commit hash, not a short bare word.
 */
export function symbolNames(spans: readonly string[]): string[] {
  const names: string[] = [];
  const seen = new Set<string>();
  for (const span of spans) {
    if (seen.has(span)) continue;
    seen.add(span);
    if (!symbolCandidate(span) || !worthLookingUp(span) || commitCandidate(span) || fileRef(span)) continue;
    names.push(span);
    if (names.length === MAX_SYMBOLS_PER_MESSAGE) break;
  }
  return names;
}

/** The workspace folder holding the project (the deepest when folders nest), or the project itself. */
export function projectFolderPath(projectPath: string, folders: readonly { path: string }[]): string {
  let best = "";
  for (const folder of folders) {
    const root = folder.path.replace(/\/+$/, "");
    if (projectPath !== root && !projectPath.startsWith(root + "/")) continue;
    if (root.length > best.length) best = root;
  }
  return best || projectPath;
}
