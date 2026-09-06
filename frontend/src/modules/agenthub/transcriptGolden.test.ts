import { describe, expect, it } from "bun:test";

import golden from "../../../../internal/agenthub/testdata/transcript_golden.json";
import type { SessionUpdate } from "./types";
import {
  applySessionUpdate,
  createInitialTranscriptModel,
  initTranscript,
} from "./transcript";

/**
 * These tests read the same artifact the Go recorder writes
 * (`internal/agenthub/testdata/transcript_golden.json`, regenerate with
 * `UPDATE_GOLDEN=1 go test ./internal/agenthub/ -run TestTranscriptGolden`).
 *
 * The point is to stop both sides being checked against a hand-written idea of
 * what the other one does. The other transcript tests assert that this reducer
 * is self-consistent; these assert it agrees with the Go implementation that
 * actually persists the rows. If the two ever diverge, a reload mid-conversation
 * silently changes what the user is reading — which is the one failure mode a
 * transcript must not have.
 */
describe("golden fixture parity with the Go recorder", () => {
  const parsed = golden.parsed as unknown as SessionUpdate[];
  const allRows = golden.rows as unknown as Record<string, unknown>[];

  // usage / commands / mode are session state, not conversation. Go keeps one
  // upserted row each so a reload can restore them; the reducer folds them into
  // model.usage and model.commands instead of rendering them. So the row list
  // and the item list are deliberately different lengths, and comparing them
  // raw would be comparing two different things.
  const SESSION_STATE = new Set([
    "usage_update",
    "available_commands_update",
    "current_mode_update",
  ]);
  const rows = allRows.filter((row) => !SESSION_STATE.has(row.Kind as string));

  function liveModel() {
    let model = createInitialTranscriptModel();
    for (const update of parsed) {
      model = applySessionUpdate(model, update);
    }
    return model;
  }

  it("produces the same number of rows as the Go recorder persisted", () => {
    expect(liveModel().items.length).toBe(rows.length);
  });

  it("agrees with the persisted rows kind by kind, in order", () => {
    const live = liveModel().items.map((item) => item.kind);
    const persisted = rows.map((row) => row.Kind);
    expect(live).toEqual(persisted as typeof live);
  });

  it("coalesces chunk text exactly as the recorder did", () => {
    const live = liveModel().items;
    rows.forEach((row, i) => {
      const text = (row.Text as string) ?? "";
      if (text !== "") {
        expect(live[i].text ?? "").toBe(text);
      }
    });
  });

  it("merges tool calls onto the same rows the recorder did", () => {
    const live = liveModel().items;
    rows.forEach((row, i) => {
      const toolCallId = (row.ToolCallID as string) ?? "";
      if (toolCallId === "") {
        return;
      }
      expect(live[i].toolCallId).toBe(toolCallId);

      const payload = row.Payload as { toolCall?: Record<string, unknown> };
      const persistedCall = payload?.toolCall;
      expect(persistedCall).toBeDefined();
      expect(live[i].toolCall?.status).toBe(persistedCall?.status as string);
      expect(live[i].toolCall?.title).toBe(persistedCall?.title as string);
      expect(live[i].toolCall?.kind).toBe(persistedCall?.kind as string);
    });
  });

  it("reaches the same model whether it streamed live or was loaded from the database", () => {
    const live = liveModel();
    // A real reload gets every row, session-state ones included — that is how
    // usage and the command list survive it.
    const loaded = initTranscript(allRows);

    expect(loaded.items.length).toBe(live.items.length);
    loaded.items.forEach((item, i) => {
      expect(item.kind).toBe(live.items[i].kind);
      expect(item.text ?? "").toBe(live.items[i].text ?? "");
      expect(item.toolCallId ?? "").toBe(live.items[i].toolCallId ?? "");
      expect(item.toolCall?.status).toBe(live.items[i].toolCall?.status);
      expect(item.toolCall?.content).toEqual(live.items[i].toolCall?.content);
    });
    expect(loaded.usage).toEqual(live.usage);
    expect(loaded.commands).toEqual(live.commands);
  });

  // The session-level rows are the reason a reload can still show the context
  // counter and the command menu before the next turn arrives.
  it("restores session state from the upserted rows rather than rendering them", () => {
    const loaded = initTranscript(allRows);

    expect(loaded.items.some((item) => SESSION_STATE.has(String(item.kind)))).toBe(false);
    expect(loaded.usage?.used).toBe(24503);
    expect(loaded.usage?.amount).toBe(0.21);
    expect(loaded.commands.map((c) => c.name)).toEqual(["compact"]);
  });

  // The fixture is only worth anything if it still covers the rules it was
  // built for. If someone trims goldenUpdates in Go, this fails rather than
  // quietly shrinking the contract.
  it("still covers every rule the fixture was built to pin", () => {
    const kinds = allRows.map((row) => row.Kind as string);
    expect(kinds).toContain("agent_message_chunk");
    expect(kinds).toContain("agent_thought_chunk");
    expect(kinds).toContain("tool_call");
    expect(kinds).toContain("plan");
    expect(kinds).toContain("available_commands_update");
    expect(kinds).toContain("usage_update");
    expect(kinds).toContain("unknown");

    // two distinct tool calls survived as separate rows
    const toolCallIds = rows
      .map((row) => row.ToolCallID as string)
      .filter((id) => id !== "");
    expect(new Set(toolCallIds).size).toBe(2);

    // a partial update did not clear the title set by the first tool_call
    const merged = rows.find((row) => row.ToolCallID === "c1");
    const call = (merged?.Payload as { toolCall?: Record<string, unknown> })
      ?.toolCall;
    expect(call?.title).toBe("Edit RefundService");
    expect(call?.status).toBe("completed");
    expect((call?.content as unknown[])?.length).toBe(1);
  });
});
