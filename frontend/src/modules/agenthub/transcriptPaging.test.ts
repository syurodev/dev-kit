import { describe, expect, test } from "bun:test";
import { anchoredScrollTop } from "./surfaces/TranscriptStream";
import { initTranscript, prependTranscriptEntries } from "./transcript";
import { EMPTY_TRANSCRIPT_MODEL } from "../../stores/agentSessions";
import type { TranscriptModel } from "./types";

const pageEntry = (seq: number, body: Record<string, unknown>) => ({
  ID: `e${seq}`,
  SessionID: "s1",
  Seq: seq,
  Kind: "",
  ToolCallID: "",
  Text: "",
  Payload: "",
  CreatedAt: "2026-09-14T00:00:00Z",
  UpdatedAt: "2026-09-14T00:00:00Z",
  ...body,
});

describe("anchoredScrollTop", () => {
  test("moves the viewport down by exactly the inserted height", () => {
    // The reader sits at 500px; a prepend adds 600px of older history above.
    expect(anchoredScrollTop(500, 2000, 2600)).toBe(1100);
  });

  test("leaves the position alone when nothing was inserted", () => {
    expect(anchoredScrollTop(500, 2000, 2000)).toBe(500);
  });

  test("at the very top of a fresh page", () => {
    expect(anchoredScrollTop(0, 1500, 2200)).toBe(700);
  });
});

describe("prependTranscriptEntries", () => {
  test("places the older page under the current items and re-indexes tool calls", () => {
    const current = initTranscript([
      pageEntry(5, { Kind: "agent_message_chunk", Text: "newest answer" }),
      pageEntry(6, {
        Kind: "tool_call",
        ToolCallID: "t1",
        Payload: JSON.stringify({ kind: "tool_call", toolCall: { id: "t1" } }),
      }),
    ]);

    const merged = prependTranscriptEntries(current, [
      pageEntry(3, { Kind: "user_message_chunk", Text: "older question" }),
      pageEntry(4, {
        Kind: "tool_call",
        ToolCallID: "t0",
        Payload: JSON.stringify({ kind: "tool_call", toolCall: { id: "t0" } }),
      }),
    ]);

    expect(merged.items.map((item) => item.seq)).toEqual([3, 4, 5, 6]);
    expect(merged.items[0].text).toBe("older question");
    // The seam: the page's last row and the held first row stay separate —
    // the backend snaps pages to turn starts so no run is ever split.
    expect(merged.toolCallIndex.get("t0")).toBe(1);
    expect(merged.toolCallIndex.get("t1")).toBe(3);
  });

  test("an empty page returns the model unchanged", () => {
    const model: TranscriptModel = {
      ...EMPTY_TRANSCRIPT_MODEL,
      items: [{ id: "a", kind: "agent_message_chunk", seq: 9, text: "hi" }],
    };
    expect(prependTranscriptEntries(model, [])).toBe(model);
  });

  test("keeps the current session-level values over the page's older ones", () => {
    const current = initTranscript([
      pageEntry(5, {
        Kind: "usage_update",
        Payload: JSON.stringify({ kind: "usage_update", usage: { used: 99, size: 100, amount: 0.5 } }),
      }),
    ]);
    const merged = prependTranscriptEntries(current, [
      pageEntry(1, {
        Kind: "usage_update",
        Payload: JSON.stringify({ kind: "usage_update", usage: { used: 1, size: 2, amount: 0 } }),
      }),
    ]);
    expect(merged.usage?.used).toBe(99);
  });
});

describe("initTranscript seq", () => {
  test("carries the row's seq onto the item", () => {
    const model = initTranscript([pageEntry(7, { Kind: "plan" })]);
    expect(model.items[0].seq).toBe(7);
  });
});
