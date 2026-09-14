import { describe, expect, test } from "bun:test";
import { anchoredScrollTop, settlePrependAnchor, type PrependAnchor } from "./surfaces/TranscriptStream";
import { createElement } from "react";
import { renderToString } from "react-dom/server";
import {
  applySessionUpdates,
  initTranscript,
  modelFromNewestPage,
  prependTranscriptEntries,
  useAgentTranscript,
} from "./transcript";
import { EMPTY_TRANSCRIPT_MODEL, useAgentSessionsStore } from "../../stores/agentSessions";
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

describe("settlePrependAnchor", () => {
  // The commits a load-older goes through, in the order the store produces
  // them: the request flips loadingOlder on before any page exists.
  test("holds the anchor while the page is in flight and restores the line once it lands", () => {
    let anchor: PrependAnchor | null = { scrollTop: 50, scrollHeight: 2000, firstItemId: "a" };

    // loadingOlder turns on; nothing has been inserted yet.
    let step = settlePrependAnchor(anchor, true, { scrollTop: 50, scrollHeight: 2000, firstItemId: "a" });
    expect(step.scrollTop).toBeNull();
    anchor = step.anchor;
    expect(anchor).not.toBeNull();

    // A token lands below and the reader nudges up while waiting.
    step = settlePrependAnchor(anchor, true, { scrollTop: 30, scrollHeight: 2100, firstItemId: "a" });
    expect(step.scrollTop).toBeNull();
    anchor = step.anchor;

    // The older page lands above: 600px inserted.
    step = settlePrependAnchor(anchor, false, { scrollTop: 30, scrollHeight: 2700, firstItemId: "older" });
    expect(step.scrollTop).toBe(630);
    expect(step.anchor).toBeNull();
  });

  test("a failed page drops the anchor without moving the view, so the next approach retries", () => {
    const anchor: PrependAnchor = { scrollTop: 50, scrollHeight: 2000, firstItemId: "a" };
    const held = settlePrependAnchor(anchor, true, { scrollTop: 50, scrollHeight: 2000, firstItemId: "a" });
    // The page failed while a streamed token grew the foot: nothing landed above.
    const step = settlePrependAnchor(held.anchor, false, { scrollTop: 50, scrollHeight: 2150, firstItemId: "a" });
    expect(step).toEqual({ anchor: null, scrollTop: null });
  });

  test("without an anchor a commit does nothing", () => {
    expect(settlePrependAnchor(null, false, { scrollTop: 0, scrollHeight: 10, firstItemId: "a" })).toEqual({
      anchor: null,
      scrollTop: null,
    });
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

describe("modelFromNewestPage", () => {
  const chunk = (kind: string, messageId: string, text: string) => ({
    kind,
    messageId,
    content: { type: "text", text },
  });

  test("restores session state that sits above the newest window", () => {
    // A long session: the state rows keep the seq their kind first appeared
    // at, far above the 200 rows the newest page holds.
    const entries = Array.from({ length: 200 }, (_, i) =>
      pageEntry(301 + i, { Kind: "agent_message_chunk", Text: `row ${i}`, Payload: { messageId: `m${i}` } }),
    );
    const state = [
      pageEntry(3, { Kind: "current_mode_update", Payload: { mode: "plan" } }),
      pageEntry(4, { Kind: "config_option_update", Payload: { configOptions: [{ id: "model", name: "Model" }] } }),
      pageEntry(5, { Kind: "available_commands_update", Payload: { commands: [{ name: "review" }] } }),
      pageEntry(6, { Kind: "usage_update", Payload: { usage: { used: 10, size: 100, amount: 0.1 } } }),
    ];
    const model = modelFromNewestPage({ entries, state }, [], "s1");
    expect(model.mode).toBe("plan");
    expect(model.configOptions.map((option) => option.id)).toEqual(["model"]);
    expect(model.commands.map((command) => command.name)).toEqual(["review"]);
    expect(model.usage?.used).toBe(10);
    expect(model.items).toHaveLength(200);
  });

  test("restores the advertised modes and keeps them across the agent's bare mode update", () => {
    const modes = [
      { id: "default", name: "Default" },
      { id: "plan", name: "Plan", description: "Read only" },
    ];
    const state = [pageEntry(3, { Kind: "current_mode_update", Payload: { mode: "default", availableModes: modes } })];
    const model = modelFromNewestPage({ entries: [], state }, [], "s1");
    expect(model.mode).toBe("default");
    expect(model.availableModes).toEqual(modes);

    // The agent's own update names only the new mode.
    const live = applySessionUpdates(model, [{ kind: "current_mode_update", mode: "plan" }]);
    expect(live.mode).toBe("plan");
    expect(live.availableModes).toEqual(modes);

    // A resume answers with the list again, and that one replaces it.
    const resumed = applySessionUpdates(live, [
      { kind: "current_mode_update", mode: "default", availableModes: [modes[0]] },
    ]);
    expect(resumed.availableModes).toEqual([modes[0]]);
  });

  test("does not append chunks the page already stored", () => {
    // The backend flushed "Hello wor" before reading; "ld" was still in flight.
    const entries = [
      pageEntry(1, { Kind: "user_message_chunk", Text: "hi", Payload: { messageId: "u1" } }),
      pageEntry(2, { Kind: "agent_message_chunk", Text: "Hello wor", Payload: { messageId: "m1" } }),
    ];
    const buffered = [
      chunk("user_message_chunk", "u1", "hi"),
      chunk("agent_message_chunk", "m1", "Hel"),
      chunk("agent_message_chunk", "m1", "lo "),
      chunk("agent_message_chunk", "m1", "wor"),
      chunk("agent_message_chunk", "m1", "ld"),
      chunk("agent_message_chunk", "m2", "Next"),
    ];
    const model = modelFromNewestPage({ entries, state: [] }, buffered, "s1");
    expect(model.items.map((item) => item.text)).toEqual(["hi", "Hello world", "Next"]);
  });
});

describe("prependTranscriptEntries state", () => {
  test("takes session state from an older page when the model has none", () => {
    const current = initTranscript([pageEntry(9, { Kind: "agent_message_chunk", Text: "newest" })]);
    const merged = prependTranscriptEntries(current, [
      pageEntry(1, { Kind: "current_mode_update", Payload: { mode: "plan" } }),
      pageEntry(2, { Kind: "available_commands_update", Payload: { commands: [{ name: "review" }] } }),
    ]);
    expect(merged.mode).toBe("plan");
    expect(merged.commands.map((command) => command.name)).toEqual(["review"]);
    expect(merged.items.map((item) => item.text)).toEqual(["newest"]);
  });
});

describe("useAgentTranscript while its surface is hidden", () => {
  // Hiding the Agent Hub tab disables the hook. An empty model there unmounted
  // every turn (open/closed state gone) and re-grouped the whole history on return.
  test("renders the model the store holds, not an empty one", () => {
    useAgentSessionsStore.getState().setTranscript("hidden-1", {
      ...EMPTY_TRANSCRIPT_MODEL,
      items: [{ id: "a", kind: "agent_message_chunk", text: "kept" }],
    });
    function Probe() {
      const transcript = useAgentTranscript("hidden-1", { enabled: false });
      return createElement("span", null, transcript.items.map((item) => item.text).join(","));
    }
    expect(renderToString(createElement(Probe))).toContain("kept");
  });
});

describe("initTranscript seq", () => {
  test("carries the row's seq onto the item", () => {
    const model = initTranscript([pageEntry(7, { Kind: "plan" })]);
    expect(model.items[0].seq).toBe(7);
  });
});
