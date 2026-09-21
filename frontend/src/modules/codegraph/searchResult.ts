import { parseToolOutput } from "@/workbench/toolResultViews";

/** Mirrors internal/codegraph/codegraph.go: SymbolMatch — codegraph.searchSymbol's rawOutput (a list). */
export type SymbolMatch = {
  nodeId: string;
  label: string;
  kind: string;
  filePath: string;
  line: number;
};

/** Mirrors internal/codegraph/codegraph.go: Endpoint — codegraph.listEndpoints's rawOutput (a list). */
export type EndpointMatch = {
  id: string;
  httpMethod: string;
  path: string;
};

function isSymbolMatchList(value: unknown): value is SymbolMatch[] {
  return (
    Array.isArray(value) &&
    value.every((v) => v && typeof v === "object" && typeof (v as Record<string, unknown>).nodeId === "string")
  );
}

function isEndpointList(value: unknown): value is EndpointMatch[] {
  return (
    Array.isArray(value) &&
    value.every((v) => v && typeof v === "object" && typeof (v as Record<string, unknown>).httpMethod === "string")
  );
}

/** Guards codegraph.searchSymbol's result before the Card component is imported. */
export function parseSymbolMatches(rawOutput: unknown): SymbolMatch[] | null {
  const parsed = parseToolOutput(rawOutput);
  return isSymbolMatchList(parsed) && parsed.length > 0 ? parsed : null;
}

/** Guards codegraph.listEndpoints's result before the Card component is imported. */
export function parseEndpointMatches(rawOutput: unknown): EndpointMatch[] | null {
  const parsed = parseToolOutput(rawOutput);
  return isEndpointList(parsed) && parsed.length > 0 ? parsed : null;
}
