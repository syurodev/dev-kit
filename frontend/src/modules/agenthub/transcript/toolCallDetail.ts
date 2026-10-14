import * as React from "react";
import { queryOptions, useQuery } from "@tanstack/react-query";

import { queryClient } from "@/lib/query";
import { agentHubToolCallDetail } from "../../../bridge/agenthub";
import { parsePayload } from "../transcript";
import type { ToolCall } from "../types";

/**
 * The whole tool call out of an agentHubToolCallDetail row. Payload is a Go
 * json.RawMessage, so the bridge hands it over already decoded — an object,
 * not a string. Parsing `String(payload)` parsed "[object Object]", threw,
 * and "Show full output" always showed nothing.
 */
export function toolCallFromDetail(payload: unknown): ToolCall | null {
  return parsePayload(payload).toolCall ?? null;
}

/**
 * The full row of one tool call, as a query: one fetch however many things
 * ask — a result card, its row's "Show full output", the same card remounted
 * as the virtualised transcript scrolls. A stored row does not change, so it
 * never goes stale; it is dropped a while after nothing shows it, because a
 * full row can be megabytes.
 */
export function toolCallDetailQuery(sessionId: string, callId: string) {
  return queryOptions({
    queryKey: ["agenthub", "toolCallDetail", sessionId, callId] as readonly string[],
    queryFn: async (): Promise<ToolCall> =>
      toolCallFromDetail((await agentHubToolCallDetail(sessionId, callId))?.Payload) ?? { id: callId },
    staleTime: Infinity,
    gcTime: 2 * 60_000,
  });
}

// The key every row that cannot load shares: whole, or with no session to ask.
const NOTHING_TO_LOAD = ["agenthub", "toolCallDetail", "none"] as const;

/**
 * What useFullToolCall observes: the call's full row when it is cut and has a
 * session to load it from, enabled once `asked`; otherwise one shared,
 * disabled key, so a transcript of whole rows adds no cache entry per row.
 */
export function fullToolCallQuery(call: Pick<ToolCall, "id" | "truncated">, sessionId: string | undefined, asked: boolean) {
  if (!call.truncated || !sessionId) {
    return queryOptions({
      queryKey: NOTHING_TO_LOAD as readonly string[],
      queryFn: async (): Promise<ToolCall> => ({ id: call.id }),
      enabled: false,
    });
  }
  return { ...toolCallDetailQuery(sessionId, call.id), enabled: asked };
}

/**
 * The whole row behind a truncated preview. Pages carry bounded previews —
 * raw fields, text content, diffs — so one huge terminal dump cannot make
 * every page load heavy; the full row is fetched only when something asks:
 * `load()` ("Show full output"), or `auto` (a result card whose view has
 * nothing to read in the preview). Once anyone has it, everyone showing the
 * call does.
 */
export function useFullToolCall(call: Pick<ToolCall, "id" | "truncated">, sessionId?: string, auto = false) {
  const canLoad = Boolean(call.truncated && sessionId);
  const [asked, setAsked] = React.useState(false);
  const query = useQuery(
    fullToolCallQuery(call, sessionId, auto || asked),
    // Passed, not read from context: rows render in tests and previews with no provider.
    queryClient,
  );
  const { isError, refetch } = query;
  const load = React.useCallback(() => {
    setAsked(true);
    if (isError) void refetch();
  }, [isError, refetch]);
  return {
    fullCall: canLoad ? (query.data ?? null) : null,
    loading: query.isFetching,
    failed: isError,
    canLoad,
    load: canLoad ? load : undefined,
  };
}

/**
 * The call a result view reads: the full row answers for the output, the call
 * on screen for who and how. The full row is the whole answer, so it is never
 * read as a cut one.
 */
export function resultCallOf(call: ToolCall, fullCall: ToolCall | null): ToolCall {
  return fullCall
    ? { ...fullCall, name: call.name ?? fullCall.name, title: call.title ?? fullCall.title, status: call.status, truncated: false }
    : call;
}

/**
 * The full row behind a truncated call when something has already loaded it
 * — never a fetch. For readers outside React (the follow panel's target, its
 * toasts) that should use a whole answer when there is one to hand.
 */
export function cachedFullToolCall(call: Pick<ToolCall, "id" | "truncated">, sessionId?: string): ToolCall | null {
  if (!call.truncated || !sessionId) return null;
  return queryClient.getQueryData(toolCallDetailQuery(sessionId, call.id).queryKey) ?? null;
}
