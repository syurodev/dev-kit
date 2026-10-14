/**
 * What the `pdf.*` tools answer (internal/mcp/tools_pdf.go), read into what
 * their cards draw. Pure, no React: each parser returns null for anything it
 * cannot read, so the transcript falls back to the generic block
 * (docs/dev-kit/tool-result-view.mdx, rule 1).
 *
 * readText and viewPages answer in text only (no structuredContent — Claude
 * would hand the model that instead), so their payload is the text itself.
 */

import { parseToolOutput } from "@/workbench/toolResultViews";
import type { ToolContentBlock } from "@/workbench/toolPayload";

function isRecord(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

function isNumber(value: unknown): value is number {
  return typeof value === "number" && Number.isFinite(value);
}

function strings(value: unknown): string[] {
  return Array.isArray(value) ? value.filter((item): item is string => typeof item === "string") : [];
}

function numbers(value: unknown): number[] {
  return Array.isArray(value) ? value.filter(isNumber) : [];
}

// --- pdf.list / pdf.info ---------------------------------------------------------

/** pdfDocSummary: one attached document. */
export type PdfDocument = {
  id: string;
  name: string;
  pages: number;
  sizeBytes: number;
  textLayer: string;
  attachedAt: string;
};

function parseDocument(value: unknown): PdfDocument | null {
  if (!isRecord(value) || typeof value.id !== "string" || typeof value.name !== "string" || !isNumber(value.pages)) {
    return null;
  }
  return {
    id: value.id,
    name: value.name,
    pages: value.pages,
    sizeBytes: isNumber(value.sizeBytes) ? value.sizeBytes : 0,
    textLayer: typeof value.textLayer === "string" ? value.textLayer : "",
    attachedAt: typeof value.attachedAt === "string" ? value.attachedAt : "",
  };
}

/** `pdf.list` → `{documents: [...]}`. */
export function parsePdfList(rawOutput: unknown): PdfDocument[] | null {
  const value = parseToolOutput(rawOutput);
  if (!isRecord(value) || !Array.isArray(value.documents)) return null;
  const documents = value.documents.map(parseDocument);
  return documents.every((doc): doc is PdfDocument => doc !== null) ? documents : null;
}

export type PdfInfo = {
  document: PdfDocument;
  /** Label → value, in reading order; empty fields left out. */
  rows: [string, string][];
  notes: string[];
};

export function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

function textLayerLabel(layer: unknown): string {
  if (!isRecord(layer) || typeof layer.state !== "string") return "";
  const missing = numbers(layer.pagesWithoutText);
  if (missing.length === 0) return layer.state;
  const count = isNumber(layer.pagesWithoutTextCount) ? layer.pagesWithoutTextCount : missing.length;
  const more = count > missing.length ? ` and ${count - missing.length} more` : "";
  return `${layer.state} · no text on ${missing.join(", ")}${more}`;
}

/** `pdf.info` → `{document, pdf, notes}`. */
export function parsePdfInfo(rawOutput: unknown): PdfInfo | null {
  const value = parseToolOutput(rawOutput);
  if (!isRecord(value) || !isRecord(value.pdf)) return null;
  const document = parseDocument(value.document);
  if (!document) return null;
  const pdf = value.pdf;
  const text = (key: string) => (typeof pdf[key] === "string" ? (pdf[key] as string) : "");
  const size = numbers(pdf.pageSizePoints);
  const rows: [string, string][] = [
    ["Document", `${document.name} (${document.id})`],
    ["Pages", String(isNumber(pdf.pages) ? pdf.pages : document.pages)],
    ["Page size", size.length === 2 ? `${Math.round(size[0])} × ${Math.round(size[1])} pt` : ""],
    ["File size", document.sizeBytes > 0 ? formatBytes(document.sizeBytes) : ""],
    ["Text layer", textLayerLabel(pdf.textLayer) || document.textLayer],
    ["Title", text("title")],
    ["Author", text("author")],
    ["Subject", text("subject")],
    ["Keywords", text("keywords")],
    ["Creator", text("creator")],
    ["Producer", text("producer")],
    ["Created", text("created")],
    ["Modified", text("modified")],
    ["Page chrome", strings(pdf.pageChrome).join(" · ")],
  ];
  return { document, rows: rows.filter(([, v]) => v !== ""), notes: strings(value.notes) };
}

// --- pdf.search -------------------------------------------------------------------

export type PdfHit = { page: number; snippet: string; match: string };

export type PdfSearch = {
  documentId: string;
  query: string;
  hits: PdfHit[];
  pagesSearched: number;
  complete: boolean;
  resumeFromPage?: number;
  truncated: boolean;
  notes: string[];
};

/** `pdf.search` → `{documentId, query, result: {hits, pagesSearched, complete, resumeFromPage?}, notes}`. */
export function parsePdfSearch(rawOutput: unknown): PdfSearch | null {
  const value = parseToolOutput(rawOutput);
  if (!isRecord(value) || !isRecord(value.result) || typeof value.query !== "string") return null;
  const result = value.result;
  if (!Array.isArray(result.hits) || !isNumber(result.pagesSearched) || typeof result.complete !== "boolean") {
    return null;
  }
  const hits: PdfHit[] = [];
  for (const hit of result.hits) {
    if (!isRecord(hit) || !isNumber(hit.page) || typeof hit.snippet !== "string") return null;
    hits.push({ page: hit.page, snippet: hit.snippet, match: typeof hit.match === "string" ? hit.match : "phrase" });
  }
  return {
    documentId: typeof value.documentId === "string" ? value.documentId : "",
    query: value.query,
    hits,
    pagesSearched: result.pagesSearched,
    complete: result.complete,
    resumeFromPage: isNumber(result.resumeFromPage) ? result.resumeFromPage : undefined,
    truncated: result.truncated === true,
    notes: strings(value.notes),
  };
}

export type MarkedSpan = { text: string; mark: boolean };

/**
 * One character folded the way the search folds (internal/pdfdoc): case and
 * diacritics ignored. Whitespace is handled by the caller.
 */
function foldChar(ch: string): string {
  return ch.normalize("NFD").toLowerCase().replace(/\p{M}/gu, "");
}

/**
 * The snippet split into marked and unmarked spans: the phrase where it
 * stands for a phrase match, each word for a words match. The snippet carries
 * no offsets, so this finds the match again with the search's own folding —
 * case and accents aside, any run of spaces as one.
 */
export function markMatches(snippet: string, query: string, match: string): MarkedSpan[] {
  // Folded haystack with, for each folded character, the original span it came from.
  let hay = "";
  const from: number[] = [];
  const to: number[] = [];
  const chars = Array.from(snippet);
  let offset = 0;
  let lastWasSpace = false;
  for (const ch of chars) {
    const start = offset;
    offset += ch.length;
    if (/\s/u.test(ch)) {
      if (lastWasSpace) {
        to[to.length - 1] = offset;
        continue;
      }
      lastWasSpace = true;
      hay += " ";
      from.push(start);
      to.push(offset);
      continue;
    }
    lastWasSpace = false;
    const foldedChars = Array.from(foldChar(ch));
    // A lone combining mark folds to nothing: it belongs to the letter before it.
    if (foldedChars.length === 0 && to.length > 0) to[to.length - 1] = offset;
    for (const folded of foldedChars) {
      hay += folded;
      from.push(start);
      to.push(offset);
    }
  }
  const foldText = (text: string) =>
    Array.from(text.trim().replace(/\s+/gu, " "))
      .map((ch) => (ch === " " ? " " : foldChar(ch)))
      .join("");
  const needles = (match === "words" ? query.trim().split(/\s+/u) : [query]).map(foldText).filter((n) => n !== "");

  // Ranges in the original snippet, found left to right, longest needle first at a tie.
  const ranges: [number, number][] = [];
  for (const needle of needles) {
    let at = hay.indexOf(needle);
    while (at !== -1) {
      ranges.push([from[at], to[at + needle.length - 1]]);
      at = hay.indexOf(needle, at + needle.length);
    }
  }
  ranges.sort((a, b) => a[0] - b[0] || b[1] - a[1]);
  const spans: MarkedSpan[] = [];
  let cursor = 0;
  for (const [start, end] of ranges) {
    if (start < cursor) continue;
    if (start > cursor) spans.push({ text: snippet.slice(cursor, start), mark: false });
    spans.push({ text: snippet.slice(start, end), mark: true });
    cursor = end;
  }
  if (cursor < snippet.length) spans.push({ text: snippet.slice(cursor), mark: false });
  return spans;
}

// --- pdf.readText -----------------------------------------------------------------

export type PdfText = {
  /** The first line: which document and which pages. */
  header: string;
  pages: { page: number; text: string }[];
  /** Notes after the last page: pages that did not fit. */
  footer: string[];
};

const PAGE_MARK = /\n--- page (\d+) of \d+ ---\n/g;
const FOOTER_LINE = /^\[Output limit reached\b.*\]$/;

/** `pdf.readText` → its text: a header, then `--- page N of M ---` per page, then footer notes. */
export function parsePdfText(rawOutput: unknown): PdfText | null {
  if (typeof rawOutput !== "string" || !rawOutput.startsWith("Document ")) return null;
  const marks = [...rawOutput.matchAll(PAGE_MARK)];
  if (marks.length === 0) return null;
  const header = rawOutput.slice(0, marks[0].index).split("\n")[0].trim();
  const pages = marks.map((mark, i) => {
    const start = mark.index! + mark[0].length;
    const end = i + 1 < marks.length ? marks[i + 1].index! : rawOutput.length;
    return { page: Number(mark[1]), text: rawOutput.slice(start, end) };
  });
  // The footer rides after the last page, each line on its own.
  const last = pages[pages.length - 1];
  const lines = last.text.split("\n");
  const footer: string[] = [];
  while (lines.length > 0) {
    const line = lines[lines.length - 1].trim();
    if (line === "") {
      lines.pop();
      continue;
    }
    if (!FOOTER_LINE.test(line)) break;
    footer.unshift(line);
    lines.pop();
  }
  last.text = lines.join("\n");
  for (const page of pages) page.text = page.text.replace(/\n+$/, "");
  return { header, pages, footer };
}

// --- pdf.viewPages ----------------------------------------------------------------

export type PdfPage = {
  page: number;
  widthPx: number;
  heightPx: number;
  dpi: number;
  mimeType: string;
  /** A zoomed part of the page rather than all of it. */
  region: boolean;
  /** A data URL built from the result's own image block; unset when the row no longer carries it. */
  src?: string;
};

export type PdfPages = {
  /** "Document doc1 · "name.pdf" · 12 pages." */
  caption: string;
  pages: PdfPage[];
};

const IMAGE_MIME = /^image\/(png|jpeg|webp|gif)$/;
const BASE64 = /^[A-Za-z0-9+/]+={0,2}$/;
const CAPTION_HEAD = /^Document .*? · \d+ pages?\./;

/** The image block's picture as a data URL — a raster type and base64 only, nothing fetched. */
function dataUrl(block: ToolContentBlock | undefined): string | undefined {
  if (!block || typeof block.data !== "string" || block.data === "") return undefined;
  const mime = block.mimeType ?? "";
  if (!IMAGE_MIME.test(mime) || !BASE64.test(block.data)) return undefined;
  return `data:${mime};base64,${block.data}`;
}

/**
 * `pdf.viewPages` → a caption line, then on the next a JSON array of
 * `{page, widthPx, heightPx, dpi, mimeType, region?}`; image blocks follow in
 * the same order. A stored row has the caption and an emptied image (or, from
 * Codex, a sentence in place of all of it).
 */
export function parsePdfPages(rawOutput: unknown, content: readonly ToolContentBlock[] = []): PdfPages | null {
  if (typeof rawOutput !== "string") return null;
  const split = rawOutput.lastIndexOf("\n[");
  if (split === -1) return null;
  let shown: unknown;
  try {
    shown = JSON.parse(rawOutput.slice(split + 1));
  } catch {
    return null;
  }
  if (!Array.isArray(shown) || shown.length === 0) return null;
  const images = content.filter((block) => block.type === "image");
  const pages: PdfPage[] = [];
  for (const [i, item] of shown.entries()) {
    if (!isRecord(item) || !isNumber(item.page) || !isNumber(item.widthPx) || !isNumber(item.heightPx)) return null;
    if (item.widthPx <= 0 || item.heightPx <= 0) return null;
    pages.push({
      page: item.page,
      widthPx: item.widthPx,
      heightPx: item.heightPx,
      dpi: isNumber(item.dpi) ? item.dpi : 0,
      mimeType: typeof item.mimeType === "string" ? item.mimeType : "",
      region: isRecord(item.region),
      src: dataUrl(images[i]),
    });
  }
  const text = rawOutput.slice(0, split);
  const caption = CAPTION_HEAD.exec(text)?.[0] ?? text.split("\n")[0];
  return { caption, pages };
}
