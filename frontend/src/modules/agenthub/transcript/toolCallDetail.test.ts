import { describe, expect, mock, test } from "bun:test";
import { QueryObserver } from "@tanstack/react-query";

// Only the one Wails call this file drives is replaced; the rest of the bridge is real.
let calls = 0;
const real = await import("../../../bridge/agenthub");
mock.module("../../../bridge/agenthub", () => ({
  ...real,
  agentHubToolCallDetail: async (sessionId: string, toolCallId: string) => {
    calls += 1;
    return { Payload: { toolCall: { id: toolCallId, rawOutput: { n: 1 }, sessionId } } };
  },
}));

const { toolCallDetailQuery } = await import("./toolCallDetail");
const { queryClient } = await import("@/lib/query");

describe("the full row of a cut tool call", () => {
  test("is fetched once, whoever asks and however often they mount", async () => {
    // Two observers are what two mounted components are to TanStack Query —
    // the card and its row's "Show full output", or one card remounted by
    // the transcript's virtualiser.
    const card = new QueryObserver(queryClient, toolCallDetailQuery("s1", "c1"));
    const row = new QueryObserver(queryClient, toolCallDetailQuery("s1", "c1"));
    const off = [card.subscribe(() => {}), row.subscribe(() => {})];
    await queryClient.fetchQuery(toolCallDetailQuery("s1", "c1"));
    for (const unsubscribe of off) unsubscribe();
    const remount = new QueryObserver(queryClient, toolCallDetailQuery("s1", "c1"));
    const unsubscribe = remount.subscribe(() => {});
    expect(remount.getCurrentResult().data?.rawOutput).toEqual({ n: 1 });
    unsubscribe();
    expect(calls).toBe(1);
  });
});
