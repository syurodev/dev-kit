/**
 * ToolResultView: how the module that owns a tool renders that tool's result
 * inline in the transcript, in place of the generic JSON block
 * (docs/dev-kit/tool-result-view.mdx). Same registry shape as
 * `registerIntent`/`registerAttachIntent` in ./intents.ts, and deliberately a
 * separate registry: a renderer answers "what does this result look like",
 * not "where does this navigate to" — a card built here still calls
 * `dispatchIntent` for its own buttons rather than inventing a third
 * mechanism.
 */

import type * as React from "react";

/**
 * What a renderer is allowed to read — rule 2 of the doc: rawInput/rawOutput
 * already on the ToolCall, nothing fetched fresh. A dedicated type rather
 * than the transcript's own `ToolCall` (frontend/src/modules/agenthub/types.ts)
 * so a renderer owned by another module never has to import Agent Hub's
 * types to be written — it reads exactly these three fields and nothing else
 * Agent Hub carries on a tool call.
 */
export type ToolResultInput = {
  /** ToolCall.name as the vendor reports it — see wireNameOf below. */
  name: string;
  rawInput: unknown;
  rawOutput: unknown;
};

export type ToolResultRenderer = (input: ToolResultInput) => React.ReactNode | null;

const WIRE_PREFIX = "mcp__devkit__";

/**
 * The wire-mangled form of a Go tool name, as Claude reports it in
 * ToolCall.name: "mcp__devkit__" + every "." turned into "_". Computed once
 * per registration, forward only, from the exact name the owning module
 * wrote — never guessed backwards from an arbitrary wire string, which is
 * ambiguous for a name that nests ("tasks.board.list") or already contains
 * "_" ("notes.set_organization"). Mirrored in
 * internal/mcp/tool_names_test.go, which guards the one thing this depends
 * on: no two Go tool names may mangle to the same wire form.
 */
function wireForm(name: string): string {
  return WIRE_PREFIX + name.split(".").join("_");
}

const byExactName = new Map<string, ToolResultRenderer>();
const byWireName = new Map<string, ToolResultRenderer>();

/**
 * Registers a renderer for the tool named `name` — the Go source's own
 * `Name:` field (e.g. "codegraph.getApiFlow"), not the mangled wire form.
 * Registered in the owning module's manifest.ts (rule 6: the module that
 * owns the tool registers it), which is eager — the renderer component
 * itself should still load lazily. Registering a name twice replaces it.
 */
export function registerToolResultView(name: string, renderer: ToolResultRenderer): () => void {
  byExactName.set(name, renderer);
  byWireName.set(wireForm(name), renderer);
  return () => {
    if (byExactName.get(name) === renderer) byExactName.delete(name);
    if (byWireName.get(wireForm(name)) === renderer) byWireName.delete(wireForm(name));
  };
}

function lookup(callName: string): ToolResultRenderer | undefined {
  return byWireName.get(callName) ?? byExactName.get(callName);
}

export function hasToolResultView(callName: string | undefined): boolean {
  return Boolean(callName && lookup(callName));
}

/**
 * Runs the registered renderer, if any. Returns null both when nobody
 * registered `input.name` and when the renderer itself declined (bad or
 * truncated rawOutput it could not make sense of) — the caller treats both
 * the same way: fall back to the generic JSON view, never an empty card
 * (rule 1: no renderer, or a renderer with nothing to show, is not a reason
 * to invent one).
 */
export function renderToolResult(input: ToolResultInput): React.ReactNode | null {
  if (!input.name) return null;
  const renderer = lookup(input.name);
  return renderer ? renderer(input) : null;
}

/**
 * rawOutput as a JSON value, however the transport handed it over — the
 * transport can carry a raw json.RawMessage as an already-parsed object, or
 * (a built-in vendor tool, or a truncated preview re-quoted as text by
 * internal/agenthub/service.go: previewRaw) as a JSON string still to
 * parse. Also unwraps the `{"result": …}` envelope internal/mcp/result.go's
 * `structured()` puts around a non-object tool result (a slice, a string, a
 * number) — but only when "result" is the object's *only* key, so a tool
 * that legitimately has a field named "result" is left alone.
 *
 * Returns null when the value isn't parseable JSON at all — including a
 * rawOutput cut short by ToolRawPreviewLimit — so a renderer can fall back
 * to the generic view instead of showing a half-parsed document.
 */
export function parseToolOutput(rawOutput: unknown): unknown | null {
  let value: unknown = rawOutput;
  if (typeof value === "string") {
    try {
      value = JSON.parse(value);
    } catch {
      return null;
    }
  }
  if (value === undefined) return null;
  if (
    value !== null &&
    typeof value === "object" &&
    !Array.isArray(value) &&
    Object.keys(value as Record<string, unknown>).length === 1 &&
    "result" in (value as Record<string, unknown>)
  ) {
    return (value as Record<string, unknown>).result ?? null;
  }
  return value;
}
