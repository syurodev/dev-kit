import { parseToolOutput } from "@/workbench/toolResultViews";

/**
 * Mirrors internal/apiclient/apiclient.go: EndpointDraft (Request, embedded,
 * plus its own two fields) — api.fromEndpoint's rawOutput.
 */
export type EndpointDraft = {
  method: string;
  url: string;
  name: string;
  bodyInferred: boolean;
  note: string;
};

function isEndpointDraft(value: unknown): value is EndpointDraft {
  if (!value || typeof value !== "object") return false;
  const v = value as Record<string, unknown>;
  return typeof v.method === "string" && typeof v.url === "string";
}

function endpointIdFrom(rawInput: unknown): string | null {
  if (!rawInput || typeof rawInput !== "object") return null;
  const id = (rawInput as Record<string, unknown>).endpointId;
  return typeof id === "string" && id ? id : null;
}

/**
 * Guards api.fromEndpoint's result before the Card component is imported.
 * Reads endpointId from rawInput — the tool's own call.RawInput
 * (internal/mcp/tools_apiclient.go: apiEndpointInput), never a store lookup:
 * rule 2 of docs/dev-kit/tool-result-view.mdx.
 */
export function parseEndpointDraft(
  rawInput: unknown,
  rawOutput: unknown,
): { draft: EndpointDraft; endpointId: string } | null {
  const endpointId = endpointIdFrom(rawInput);
  const parsed = parseToolOutput(rawOutput);
  if (!endpointId || !isEndpointDraft(parsed)) return null;
  return { draft: parsed, endpointId };
}
