/**
 * One payload whatever the vendor sends (docs/dev-kit/tool-result-view.mdx).
 *
 * The same DevKit tool result reaches the transcript in three shapes:
 *
 * - **Claude** (claude-agent-acp) leaves `rawOutput` out whenever there is
 *   content: the result is `content[].content.text` — the JSON of the tool's
 *   structuredContent, or an AppError ("<code>: <message>") fenced in ``` when
 *   the call failed. It names the call `mcp__devkit__<tool>`.
 * - **Codex** puts the MCP result itself in `rawOutput` —
 *   `{content:[{type:"text",text}], structuredContent}` — a failure as
 *   `{error}`, and a result over its size cap as `{truncated:true, bytes}`.
 *   Its calls have no `name`; the title is `devkit.<tool>`.
 * - **Anything else** carries the payload in `rawOutput`, as an object or as
 *   JSON text still to parse.
 *
 * And a page read cuts any raw field over 8 KiB to a preview, a JSON *string*
 * of its first bytes, and sets `truncated`.
 *
 * Views read only what this file makes of a call, so none of them has to know
 * which vendor it is looking at. The types are structural on purpose: the
 * workbench may not import Agent Hub's `ToolCall`, and Agent Hub's satisfies
 * these as it is.
 */

import { parseToolOutput, type ToolResultInput } from "./toolResultViews";

/** One ACP content block — the fields a result view may read. */
export type ToolContentBlock = {
  type: string;
  text?: string;
  mimeType?: string;
  data?: string;
  uri?: string;
  name?: string;
};

/** One ACP tool-call content entry (`content` / `diff` / `terminal`). */
export type ToolCallContentLike = {
  type: string;
  content?: ToolContentBlock;
  /** Set by the page read: this entry's text was cut to a preview. */
  truncated?: boolean;
};

/** What this file reads of a tool call. */
export type ToolCallLike = {
  name?: string;
  title?: string;
  status?: string;
  content?: ToolCallContentLike[];
  rawInput?: unknown;
  rawOutput?: unknown;
  /** Set by the page read when oversized raw fields were cut to a preview. */
  truncated?: boolean;
};

/** A result to show, an error to report, or — null — nothing to read here. */
export type ToolPayload = { payload: unknown } | { error: string };

const CODEX_SERVER_PREFIX = "devkit.";
const FENCE = /^```[a-z]*\n([\s\S]*?)\n```$/;
/** internal/mcp's AppError text: "<code>: <message>", code like `query_failed` or `pdf.notFound`. */
const APP_ERROR = /^[A-Za-z_.]+:/;
/** The keys an MCP CallToolResult may carry — anything else is a tool's own payload. */
const MCP_RESULT_KEYS = new Set(["content", "structuredContent", "isError", "_meta"]);

/**
 * The tool name to look a view up by. Claude's `name` is the wire form, which
 * the registry already resolves; a Codex call has no name, only the title
 * `devkit.<tool>`, whose tail is the Go name itself. Anything else — a
 * vendor's own tool, a title that is a sentence — has no DevKit name: "".
 */
export function toolNameOf(call: Pick<ToolCallLike, "name" | "title">): string {
  if (call.name) return call.name;
  const title = call.title ?? "";
  if (!title.startsWith(CODEX_SERVER_PREFIX)) return "";
  const tool = title.slice(CODEX_SERVER_PREFIX.length);
  return /^[A-Za-z0-9_.]+$/.test(tool) ? tool : "";
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

function failed(status: string | undefined): boolean {
  return status?.toLowerCase() === "failed";
}

/** Codex's `{error}` — the only key, a string. */
function codexError(value: Record<string, unknown>): string | null {
  const keys = Object.keys(value);
  return keys.length === 1 && typeof value.error === "string" ? value.error : null;
}

/** Codex's size stub: `{truncated:true, bytes}` (and a `type` for a non-MCP item), nothing else. */
function isCodexStub(value: Record<string, unknown>): boolean {
  if (value.truncated !== true || typeof value.bytes !== "number") return false;
  return Object.keys(value).every((key) => key === "truncated" || key === "bytes" || key === "type");
}

/** An MCP CallToolResult as Codex hands it over: a content array and nothing a tool would call its own. */
function isMcpResult(value: Record<string, unknown>): boolean {
  return Array.isArray(value.content) && Object.keys(value).every((key) => MCP_RESULT_KEYS.has(key));
}

function textOf(blocks: readonly (ToolContentBlock | undefined)[]): string {
  return blocks
    .filter((block): block is ToolContentBlock => block?.type === "text" && typeof block.text === "string")
    .map((block) => block.text!)
    .join("\n\n");
}

/** The error text a fenced block holds, or the text as it is. */
function unfenced(text: string): string {
  const match = FENCE.exec(text.trim());
  return match ? match[1].trim() : text.trim();
}

/**
 * A result's text: an error when the call failed or the text is a fenced
 * AppError; otherwise its JSON (envelope unwrapped) or, not being JSON, the
 * text itself — unless the text is a cut preview, which is null so the caller
 * loads the full row instead of showing half a document.
 */
function fromText(text: string, isError: boolean, truncated: boolean): ToolPayload | null {
  const fenced = FENCE.exec(text.trim());
  if (fenced && APP_ERROR.test(fenced[1].trim())) return { error: fenced[1].trim() };
  if (isError) return { error: unfenced(text) || "failed" };
  if (text === "") return null;
  try {
    return { payload: parseToolOutput(JSON.parse(text)) };
  } catch {
    return truncated ? null : { payload: text };
  }
}

function fromRawOutput(raw: unknown, isError: boolean, truncated: boolean): ToolPayload | null {
  let value = raw;
  if (typeof value === "string") {
    try {
      value = JSON.parse(value);
    } catch {
      return fromText(value as string, isError, truncated);
    }
  }
  if (isRecord(value)) {
    const error = codexError(value);
    if (error !== null) return { error };
    if (isCodexStub(value)) return isError ? { error: "failed" } : null;
    if (isMcpResult(value)) {
      const mcpError = isError || value.isError === true;
      if (!mcpError && value.structuredContent !== undefined && value.structuredContent !== null) {
        return { payload: parseToolOutput(value.structuredContent) };
      }
      return fromText(textOf(value.content as ToolContentBlock[]), mcpError, truncated);
    }
  }
  if (isError) return { error: typeof raw === "string" ? unfenced(raw) || "failed" : "failed" };
  if (value === null || value === undefined) return null;
  return { payload: parseToolOutput(value) };
}

/**
 * What a tool call returned, read the same way for every vendor: `{payload}`
 * (an object, an array or text) for a result, `{error}` for a failure, null
 * when there is nothing to read — no output yet, Codex's size stub, or a cut
 * preview that does not parse (load the full row and ask again).
 */
export function toolPayloadOf(call: ToolCallLike): ToolPayload | null {
  const isError = failed(call.status);
  const content = call.content ?? [];
  const truncated = Boolean(call.truncated) || content.some((item) => item.truncated);
  if (call.rawOutput !== undefined && call.rawOutput !== null) {
    return fromRawOutput(call.rawOutput, isError, truncated);
  }
  const text = textOf(content.filter((item) => item.type === "content").map((item) => item.content));
  if (text !== "") return fromText(text, isError, truncated);
  return isError ? { error: "failed" } : null;
}

/** The content blocks a call carries — images ride here, not in the payload. */
function contentBlocksOf(call: ToolCallLike): ToolContentBlock[] {
  const blocks: ToolContentBlock[] = [];
  for (const item of call.content ?? []) {
    if (item.type === "content" && item.content) blocks.push(item.content);
  }
  if (blocks.length === 0 && call.rawOutput && typeof call.rawOutput === "object" && !Array.isArray(call.rawOutput)) {
    const raw = call.rawOutput as Record<string, unknown>;
    if (isMcpResult(raw)) {
      for (const block of raw.content as unknown[]) {
        if (isRecord(block) && typeof block.type === "string") blocks.push(block as ToolContentBlock);
      }
    }
  }
  return blocks;
}

/**
 * What a result view is handed for this call, or null when it has nothing
 * to show: an error (the row keeps today's failed treatment — views see only
 * successful payloads) or no payload to read.
 */
export function toolResultInputOf(call: ToolCallLike, heading?: string): ToolResultInput | null {
  const result = toolPayloadOf(call);
  if (!result || "error" in result) return null;
  const input: ToolResultInput = {
    name: toolNameOf(call),
    rawInput: call.rawInput,
    rawOutput: result.payload,
    content: contentBlocksOf(call),
    status: call.status,
  };
  if (heading !== undefined) input.heading = heading;
  return input;
}
