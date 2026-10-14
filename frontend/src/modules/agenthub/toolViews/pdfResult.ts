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
  /** Pages with no text layer, so not searched (capped by Go). */
  pagesWithoutText: number[];
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
    // Go's `notes` are written for the agent — how to continue, what to try,
    // that snippets are data. The fields above already say what they say;
    // the one fact the card adds is which pages could not be searched.
    pagesWithoutText: numbers(result.pagesWithoutText),
  };
}

export type MarkedSpan = { text: string; mark: boolean };

/**
 * One character folded the way the search folds (internal/pdfdoc/search.go
 * foldRune): case and diacritics ignored, đ/Đ mapped by hand since it has no
 * decomposition. Whitespace is handled by the caller.
 */
function foldChar(ch: string): string {
  if (ch === "đ" || ch === "Đ") return "d";
  return ch.normalize("NFD").toLowerCase().replace(/\p{M}/gu, "");
}

/**
 * The snippet split into marked and unmarked spans: the phrase where it
 * stands for a phrase match, each word for a words match. The snippet carries
 * no offsets, so this finds the match again with the search's own folding —
 * case and accents aside, any run of spaces as one.
 */
export function markMatches(snippet: string, query: string, match: string): MarkedSpan[] {
  // The folded haystack, and for each of its UTF-16 code units (what indexOf
  // counts) the span of the original snippet it came from.
  let hay = "";
  const from: number[] = [];
  const to: number[] = [];
  const push = (text: string, start: number, end: number) => {
    hay += text;
    for (let i = 0; i < text.length; i++) {
      from.push(start);
      to.push(end);
    }
  };
  let offset = 0;
  let lastWasSpace = false;
  for (const ch of snippet) {
    const start = offset;
    offset += ch.length;
    if (/\s/u.test(ch)) {
      if (lastWasSpace) {
        to[to.length - 1] = offset;
        continue;
      }
      lastWasSpace = true;
      push(" ", start, offset);
      continue;
    }
    lastWasSpace = false;
    const folded = foldChar(ch);
    // A lone combining mark folds to nothing: it belongs to the letter before it.
    if (folded === "" && to.length > 0) to[to.length - 1] = offset;
    push(folded, start, offset);
  }
  const foldText = (text: string) =>
    Array.from(text.trim().replace(/\s+/gu, " "))
      .map((ch) => (ch === " " ? " " : foldChar(ch)))
      .join("");
  const needles = (match === "words" ? query.trim().split(/\s+/u) : [query]).map(foldText).filter((n) => n !== "");

  // Ranges in the original snippet, left to right, the longer first at a tie.
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

/** Go writes every mark after a blank line: the header and each page end in "\n", the mark starts with one. */
const PAGE_MARK = /\n\n--- page (\d+) of (\d+) ---\n/g;
const FOOTER_LINE = /^\[Output limit reached\b.*\]$/;
/** Go's `Document %s · %q · %d pages.` — the name is quoted, so it may hold anything but an unescaped quote. */
const DOCUMENT_HEAD = /^Document \S+ · "(?:[^"\\]|\\.)*" · (\d+) pages\./;
const PAGE_LIST = /Text of pages? ([\d,-]+)(?: \(column layout\))?\.$/;

/** "3-4,7" → [3, 4, 7]: the pages a read was asked for, as Go's spanOf wrote them. */
function pageSpan(spec: string): number[] {
  const out: number[] = [];
  for (const part of spec.split(",")) {
    const [lo, hi] = part.split("-").map(Number);
    if (!Number.isInteger(lo)) return [];
    const last = Number.isInteger(hi) ? hi : lo;
    for (let n = lo; n <= last && out.length < 3000; n++) out.push(n);
  }
  return out;
}

/**
 * `pdf.readText` → its text: a header, then `--- page N of M ---` per page,
 * then footer notes. Document text can hold a line that looks like a mark, so
 * a mark counts only when it follows a blank line, M is the header's page
 * count and N is one of the pages the header lists, in order. Several that
 * name the same page: the last before the next page's first is the real one
 * (Go writes the mark, then the page's own text — any copy inside that text
 * comes after a real mark, never before it).
 */
export function parsePdfText(rawOutput: unknown): PdfText | null {
  if (typeof rawOutput !== "string") return null;
  const header = rawOutput.split("\n")[0].trim();
  const head = DOCUMENT_HEAD.exec(header);
  if (!head) return null;
  const total = head[1];
  const expected = pageSpan(PAGE_LIST.exec(header)?.[1] ?? "");
  const marks: RegExpMatchArray[] = [];
  let current = -1; // the index in `expected` of the page whose mark is marks' last
  for (const mark of rawOutput.matchAll(PAGE_MARK)) {
    if (mark[2] !== total) continue;
    const page = Number(mark[1]);
    if (current >= 0 && expected[current] === page) {
      marks[marks.length - 1] = mark;
      continue;
    }
    const at = expected.indexOf(page, current + 1);
    if (at === -1) continue;
    marks.push(mark);
    current = at;
  }
  if (marks.length === 0) return null;
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
  /** A data URL built from the result's own image block; unset when there is no picture to draw. */
  src?: string;
};

export type PdfPages = {
  /** "Document doc1 · "name.pdf" · 12 pages." */
  caption: string;
  pages: PdfPage[];
  /** The result had page images and this row no longer carries them (a stored row). */
  imagesDropped: boolean;
};

const IMAGE_MIME = /^image\/(png|jpeg|webp|gif)$/;
const BASE64_SAMPLE = /^[A-Za-z0-9+/]*={0,2}$/;
const SAMPLE = 64;
const CAPTION_HEAD = /^Document \S+ · "(?:[^"\\]|\\.)*" · \d+ pages\./;

// Per image block: the transcript keeps handing back the same block object,
// and a page picture is megabytes, so its data URL is built once.
const dataUrls = new WeakMap<ToolContentBlock, string | null>();

/**
 * The image block's picture as a data URL — a raster type and base64 only,
 * nothing fetched. Checked cheaply (length and a sample at each end) rather
 * than with a pattern over megabytes; a bad picture fails to decode, nothing more.
 */
function dataUrl(block: ToolContentBlock): string | undefined {
  const known = dataUrls.get(block);
  if (known !== undefined) return known ?? undefined;
  const data = typeof block.data === "string" ? block.data : "";
  const mime = block.mimeType ?? "";
  const ok =
    data.length > 0 &&
    data.length % 4 === 0 &&
    IMAGE_MIME.test(mime) &&
    BASE64_SAMPLE.test(data.slice(0, SAMPLE).replace(/=+$/, "")) &&
    BASE64_SAMPLE.test(data.slice(-SAMPLE));
  const url = ok ? `data:${mime};base64,${data}` : null;
  dataUrls.set(block, url);
  return url ?? undefined;
}

/**
 * `pdf.viewPages` → a caption line, then on the next a JSON array of
 * `{page, widthPx, heightPx, dpi, mimeType, region?}`; image blocks follow in
 * the same order. A stored row has the caption and emptied images (or, from
 * Codex, a sentence in place of all of it). When the images and the pages do
 * not pair one to one, no page is given a picture: a wrong page is worse.
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
  const paired = images.length === shown.length;
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
      src: paired ? dataUrl(images[i]) : undefined,
    });
  }
  const text = rawOutput.slice(0, split);
  const caption = CAPTION_HEAD.exec(text)?.[0] ?? text.split("\n")[0];
  const imagesDropped = images.some((block) => !block.data);
  return { caption, pages, imagesDropped };
}
