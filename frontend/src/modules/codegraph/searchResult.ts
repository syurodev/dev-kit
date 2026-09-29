import { parseToolOutput } from "@/workbench/toolResultViews";

/** Mirrors internal/codegraph/codegraph.go: SymbolMatch — codegraph.searchSymbol's rawOutput (a list). */
export type SymbolMatch = {
  nodeId: string;
  label: string;
  kind: string;
  filePath: string;
  line: number;
};

/** One row of codegraph.listEndpoints's rawOutput — internal/codegraph/codegraph.go: EntryPoint. */
export type EntryPointMatch = {
  id: string;
  verb: string;
  target: string;
};

function isSymbolMatchList(value: unknown): value is SymbolMatch[] {
  return (
    Array.isArray(value) &&
    value.every((v) => v && typeof v === "object" && typeof (v as Record<string, unknown>).nodeId === "string")
  );
}

/**
 * One row, whichever way it was written: `verb`/`target` today, and
 * `httpMethod`/`path` in a transcript from before entry points had kinds —
 * a transcript outlives the build that wrote it.
 */
function toEntryPointMatch(value: unknown): EntryPointMatch | null {
  if (!value || typeof value !== "object") return null;
  const v = value as Record<string, unknown>;
  const verb = v.verb ?? v.httpMethod;
  const target = v.target ?? v.path;
  if (typeof v.id !== "string" || typeof verb !== "string" || typeof target !== "string") return null;
  return { id: v.id, verb, target };
}

/** Guards codegraph.searchSymbol's result before the Card component is imported. */
export function parseSymbolMatches(rawOutput: unknown): SymbolMatch[] | null {
  const parsed = parseToolOutput(rawOutput);
  return isSymbolMatchList(parsed) && parsed.length > 0 ? parsed : null;
}

/** Guards codegraph.listEndpoints's result before the Card component is imported. */
export function parseEntryPointMatches(rawOutput: unknown): EntryPointMatch[] | null {
  const parsed = parseToolOutput(rawOutput);
  if (!Array.isArray(parsed) || parsed.length === 0) return null;
  const rows = parsed.map(toEntryPointMatch);
  return rows.every((r): r is EntryPointMatch => r !== null) ? rows : null;
}
