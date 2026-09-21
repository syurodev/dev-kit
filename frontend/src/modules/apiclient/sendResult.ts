import { parseToolOutput } from "@/workbench/toolResultViews";

/**
 * The fields the card shows from internal/apiclient/apiclient.go: Response —
 * api.send's rawOutput. Never `body`: this is a saved request, sent for
 * real, so its response can carry real data — unlike api.fromEndpoint's
 * draft, which never leaves the source it was read from.
 */
export type SendResult = {
  status: number;
  statusText: string;
  truncated: boolean;
  bytes: number;
  durationMs: number;
};

function isSendResult(value: unknown): value is SendResult {
  if (!value || typeof value !== "object") return false;
  const v = value as Record<string, unknown>;
  return typeof v.status === "number" && typeof v.statusText === "string";
}

function requestIdFrom(rawInput: unknown): string | null {
  if (!rawInput || typeof rawInput !== "object") return null;
  const id = (rawInput as Record<string, unknown>).requestId;
  return typeof id === "string" && id ? id : null;
}

/**
 * Guards api.send's result before the Card component is imported.
 * requestId comes from rawInput (internal/mcp/tools_apiclient.go:
 * apiSendInput) — never a store lookup: rule 2 of
 * docs/dev-kit/tool-result-view.mdx.
 */
export function parseSendResult(
  rawInput: unknown,
  rawOutput: unknown,
): { response: SendResult; requestId: string } | null {
  const requestId = requestIdFrom(rawInput);
  const parsed = parseToolOutput(rawOutput);
  if (!requestId || !isSendResult(parsed)) return null;
  return { response: parsed, requestId };
}
