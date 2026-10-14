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
import type { ToolContentBlock } from "./toolPayload";

/**
 * What a renderer is allowed to read — rule 2 of the doc: rawInput/rawOutput
 * already on the ToolCall, nothing fetched fresh. A dedicated type rather
 * than the transcript's own `ToolCall` (frontend/src/modules/agenthub/types.ts)
 * so a renderer owned by another module never has to import Agent Hub's
 * types to be written — it reads exactly these fields and nothing else
 * Agent Hub carries on a tool call.
 */
export type ToolResultInput = {
  /**
   * The tool's name: Claude's wire form or the Go name itself — see
   * wireForm below. Built by ./toolPayload.ts: toolNameOf, which also names
   * a Codex call (no `name`, title `devkit.<tool>`).
   */
  name: string;
  rawInput: unknown;
  /**
   * The tool's result, whichever vendor carried it and however
   * (./toolPayload.ts: toolPayloadOf) — an object or array already parsed,
   * or text that is not JSON. Never an error: a failed call keeps the
   * transcript's own failed row. Still read it through parseToolOutput,
   * which passes a parsed value through.
   */
  rawOutput: unknown;
  /** The call's content blocks (text, images) — where a result's pictures ride. */
  content?: ToolContentBlock[];
  /** ToolCall.status as the vendor reports it ("completed"). */
  status?: string;
  /**
   * What the transcript calls this call when it draws the card on its own —
   * the activity's verb, "Created task" (./toolActivity.ts). A card puts it
   * in place of its noun ("Task"); unset inside a run's fold.
   */
  heading?: string;
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

/**
 * Something registered per DevKit tool, found again from the name a vendor
 * reports — its own form or the wire-mangled one. Shared by this registry and
 * ./toolActivity.ts, so both answer for exactly the same names.
 */
export function createToolRegistry<T>() {
  const byExactName = new Map<string, T>();
  const byWireName = new Map<string, T>();
  return {
    register(name: string, value: T): () => void {
      byExactName.set(name, value);
      byWireName.set(wireForm(name), value);
      return () => {
        if (byExactName.get(name) === value) byExactName.delete(name);
        if (byWireName.get(wireForm(name)) === value) byWireName.delete(wireForm(name));
      };
    },
    lookup(callName: string | undefined): T | undefined {
      if (!callName) return undefined;
      return byWireName.get(callName) ?? byExactName.get(callName);
    },
  };
}

const views = createToolRegistry<ToolResultRenderer>();

/**
 * Registers a renderer for the tool named `name` — the Go source's own
 * `Name:` field (e.g. "codegraph.getApiFlow"), not the mangled wire form.
 * Registered in the owning module's manifest.ts (rule 6: the module that
 * owns the tool registers it), which is eager — the renderer component
 * itself should still load lazily, and may suspend while it does: the caller
 * of renderToolResult provides the Suspense boundary, so the transcript can
 * keep what was on screen (the running card) as the fallback instead of an
 * empty gap. Registering a name twice replaces it.
 */
export function registerToolResultView(name: string, renderer: ToolResultRenderer): () => void {
  return views.register(name, renderer);
}

function lookup(callName: string): ToolResultRenderer | undefined {
  return views.lookup(callName);
}

export function hasToolResultView(callName: string | undefined): boolean {
  return Boolean(callName && lookup(callName));
}

/**
 * Runs the registered renderer, if any. The node may suspend (a card that
 * loads lazily): render it inside a Suspense boundary. Returns null both when nobody
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
