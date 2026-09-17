import { describe, expect, it } from "bun:test";
import { UpdateKind } from "../../../bindings/devkit/internal/agenthub/models";
import {
  applySessionUpdate,
  createInitialTranscriptModel,
  initTranscript,
  isChunkKind,
  isSameMessage,
  mergeToolCall,
  transcriptReducer,
  type SessionUpdate,
  type ToolCall,
  type TranscriptModel,
} from "./transcript";

describe("Agent Hub Transcript Reducer & Coalescing", () => {
  it("updates one live plan in place", () => {
    let model = createInitialTranscriptModel();
    model = applySessionUpdate(model, { kind: "user_message_chunk", content: { type: "text", text: "work" } });
    model = applySessionUpdate(model, { kind: "plan", plan: [{ content: "One", status: "in_progress" }] });
    const planId = model.items[1].id;
    model = applySessionUpdate(model, { kind: "plan", plan: [{ content: "One", status: "completed" }, { content: "Two" }] });
    expect(model.items).toHaveLength(2);
    expect(model.items[1].id).toBe(planId);
    expect(model.items[1].plan?.[1].content).toBe("Two");
  });

  it("coalesces consecutive chunks of the same kind", () => {
    let model: TranscriptModel = createInitialTranscriptModel();

    model = applySessionUpdate(model, {
      kind: UpdateKind.UpdateAgentMessageChunk,
      content: { type: "text", text: "Found " },
    });
    model = applySessionUpdate(model, {
      kind: UpdateKind.UpdateAgentMessageChunk,
      content: { type: "text", text: "the " },
    });
    model = applySessionUpdate(model, {
      kind: UpdateKind.UpdateAgentMessageChunk,
      content: { type: "text", text: "bug." },
    });

    expect(model.items.length).toBe(1);
    expect(model.items[0].kind).toBe(UpdateKind.UpdateAgentMessageChunk);
    expect(model.items[0].text).toBe("Found the bug.");
  });

  it("splits chunks of different kinds", () => {
    let model = createInitialTranscriptModel();

    model = applySessionUpdate(model, {
      kind: UpdateKind.UpdateAgentThoughtChunk,
      content: { type: "text", text: "pondering" },
    });
    model = applySessionUpdate(model, {
      kind: UpdateKind.UpdateAgentMessageChunk,
      content: { type: "text", text: "answer" },
    });

    expect(model.items.length).toBe(2);
    expect(model.items[0].kind).toBe(UpdateKind.UpdateAgentThoughtChunk);
    expect(model.items[0].text).toBe("pondering");
    expect(model.items[1].kind).toBe(UpdateKind.UpdateAgentMessageChunk);
    expect(model.items[1].text).toBe("answer");
  });

  it("splits chunks by messageId when declared", () => {
    let model = createInitialTranscriptModel();

    model = applySessionUpdate(model, {
      kind: UpdateKind.UpdateAgentMessageChunk,
      content: { type: "text", text: "first" },
      messageId: "m1",
    });
    model = applySessionUpdate(model, {
      kind: UpdateKind.UpdateAgentMessageChunk,
      content: { type: "text", text: " part" },
      messageId: "m1",
    });
    model = applySessionUpdate(model, {
      kind: UpdateKind.UpdateAgentMessageChunk,
      content: { type: "text", text: "second" },
      messageId: "m2",
    });

    expect(model.items.length).toBe(2);
    expect(model.items[0].text).toBe("first part");
    expect(model.items[0].messageId).toBe("m1");
    expect(model.items[1].text).toBe("second");
    expect(model.items[1].messageId).toBe("m2");
  });

  it("coalesces chunks when messageId is omitted on either side", () => {
    expect(isSameMessage(undefined, "m1")).toBe(true);
    expect(isSameMessage("m1", undefined)).toBe(true);
    expect(isSameMessage(undefined, undefined)).toBe(true);
    expect(isSameMessage("m1", "m1")).toBe(true);
    expect(isSameMessage("m1", "m2")).toBe(false);
  });

  it("starts a new row when a chunk arrives after an intervening tool call", () => {
    let model = createInitialTranscriptModel();

    model = applySessionUpdate(model, {
      kind: UpdateKind.UpdateAgentMessageChunk,
      content: { type: "text", text: "before" },
    });
    model = applySessionUpdate(model, {
      kind: UpdateKind.UpdateToolCall,
      toolCall: { id: "c1", status: "completed" },
    });
    model = applySessionUpdate(model, {
      kind: UpdateKind.UpdateAgentMessageChunk,
      content: { type: "text", text: "after" },
    });

    expect(model.items.length).toBe(3);
    expect(model.items[0].text).toBe("before");
    expect(model.items[1].toolCallId).toBe("c1");
    expect(model.items[2].text).toBe("after");
  });

  it("merges tool_call_update onto matching toolCallId, leaving empty fields unchanged", () => {
    let model = createInitialTranscriptModel();

    model = applySessionUpdate(model, {
      kind: UpdateKind.UpdateToolCall,
      toolCall: {
        id: "c1",
        title: "Edit RefundService",
        kind: "edit",
        status: "pending",
        locations: [{ path: "/RefundService.java", line: 42 }],
        rawInput: { file: "RefundService.java" },
      },
    });
    model = applySessionUpdate(model, {
      kind: UpdateKind.UpdateToolCallUpdate,
      toolCall: {
        id: "c1",
        status: "in_progress",
      },
    });
    model = applySessionUpdate(model, {
      kind: UpdateKind.UpdateToolCallUpdate,
      toolCall: {
        id: "c1",
        status: "completed",
        content: [
          {
            type: "diff",
            path: "/RefundService.java",
            newText: "return refund;",
          },
        ],
      },
    });

    expect(model.items.length).toBe(1);
    const call = model.items[0].toolCall;
    expect(call).toBeDefined();
    expect(call?.id).toBe("c1");
    expect(call?.title).toBe("Edit RefundService");
    expect(call?.kind).toBe("edit");
    expect(call?.status).toBe("completed");
    expect(call?.locations?.length).toBe(1);
    expect(call?.locations?.[0].path).toBe("/RefundService.java");
    expect(call?.rawInput).toEqual({ file: "RefundService.java" });
    expect(call?.content?.length).toBe(1);
    expect(call?.content?.[0].path).toBe("/RefundService.java");
  });

  it("replaces content and locations wholesale when present in tool update", () => {
    const prev: ToolCall = {
      id: "c1",
      content: [{ type: "diff", path: "/old.java", newText: "old" }],
      locations: [{ path: "/old.java" }],
    };
    const next: ToolCall = {
      id: "c1",
      content: [{ type: "diff", path: "/new.java", newText: "new" }],
      locations: [{ path: "/new.java" }],
    };

    const merged = mergeToolCall(prev, next);
    expect(merged.content?.length).toBe(1);
    expect(merged.content?.[0].path).toBe("/new.java");
    expect(merged.locations?.length).toBe(1);
    expect(merged.locations?.[0].path).toBe("/new.java");
  });

  it("keeps separate tool calls apart", () => {
    let model = createInitialTranscriptModel();

    model = applySessionUpdate(model, {
      kind: UpdateKind.UpdateToolCall,
      toolCall: { id: "c1", status: "completed" },
    });
    model = applySessionUpdate(model, {
      kind: UpdateKind.UpdateToolCall,
      toolCall: { id: "c2", status: "pending" },
    });

    expect(model.items.length).toBe(2);
    expect(model.items[0].toolCallId).toBe("c1");
    expect(model.items[1].toolCallId).toBe("c2");
  });

  it("merges tool call updates onto the correct row even if intervening items exist", () => {
    let model = createInitialTranscriptModel();

    model = applySessionUpdate(model, {
      kind: UpdateKind.UpdateToolCall,
      toolCall: { id: "c1", status: "in_progress", title: "Build" },
    });
    model = applySessionUpdate(model, {
      kind: UpdateKind.UpdateAgentMessageChunk,
      content: { type: "text", text: "Building target..." },
    });
    model = applySessionUpdate(model, {
      kind: UpdateKind.UpdateToolCallUpdate,
      toolCall: { id: "c1", status: "completed" },
    });

    expect(model.items.length).toBe(2);
    expect(model.items[0].toolCallId).toBe("c1");
    expect(model.items[0].toolCall?.status).toBe("completed");
    expect(model.items[0].toolCall?.title).toBe("Build");
    expect(model.items[1].kind).toBe(UpdateKind.UpdateAgentMessageChunk);
    expect(model.toolCallIndex.get("c1")).toBe(0);
  });

  it("updates session-level usage without adding a transcript row", () => {
    let model = createInitialTranscriptModel();

    model = applySessionUpdate(model, {
      kind: UpdateKind.UpdateUsage,
      usage: {
        used: 1200,
        size: 200000,
        amount: 0.045,
        currency: "USD",
      },
    });

    expect(model.items.length).toBe(0);
    expect(model.usage).toEqual({
      used: 1200,
      size: 200000,
      amount: 0.045,
      currency: "USD",
    });
  });

  it("updates session-level commands and mode without adding transcript rows", () => {
    let model = createInitialTranscriptModel();

    model = applySessionUpdate(model, {
      kind: UpdateKind.UpdateAvailableCommands,
      commands: [{ name: "/test", description: "Run test suite" }],
    });
    model = applySessionUpdate(model, {
      kind: UpdateKind.UpdateCurrentMode,
      mode: "architect",
    });

    expect(model.items.length).toBe(0);
    expect(model.commands).toEqual([{ name: "/test", description: "Run test suite" }]);
    expect(model.mode).toBe("architect");
  });

  it("preserves unknown and unmodelled kinds as intact rows with raw payload", () => {
    let model = createInitialTranscriptModel();

    model = applySessionUpdate(model, {
      kind: "future_acp_spec_kind",
      raw: { customField: 999, nested: { ok: true } },
    });

    expect(model.items.length).toBe(1);
    expect(model.items[0].kind).toBe("future_acp_spec_kind");
    expect(model.items[0].raw).toEqual({ customField: 999, nested: { ok: true } });
  });

  it("records plan updates as rows", () => {
    let model = createInitialTranscriptModel();

    model = applySessionUpdate(model, {
      kind: UpdateKind.UpdatePlan,
      plan: [
        { content: "Step 1", status: "completed" },
        { content: "Step 2", status: "in_progress" },
      ],
    });

    expect(model.items.length).toBe(1);
    expect(model.items[0].kind).toBe(UpdateKind.UpdatePlan);
    expect(model.items[0].plan?.length).toBe(2);
    expect(model.items[0].plan?.[0].content).toBe("Step 1");
  });

  it("recognizes chunk kinds accurately", () => {
    expect(isChunkKind(UpdateKind.UpdateUserMessageChunk)).toBe(true);
    expect(isChunkKind(UpdateKind.UpdateAgentMessageChunk)).toBe(true);
    expect(isChunkKind(UpdateKind.UpdateAgentThoughtChunk)).toBe(true);
    expect(isChunkKind(UpdateKind.UpdateToolCall)).toBe(false);
    expect(isChunkKind(UpdateKind.UpdatePlan)).toBe(false);
    expect(isChunkKind(UpdateKind.UpdateUsage)).toBe(false);
  });

  describe("initTranscript from persisted TranscriptEntry rows", () => {
    it("converts persisted database entries into render model", () => {
      const entries = [
        {
          ID: "e1",
          Seq: 1,
          Kind: UpdateKind.UpdateAgentMessageChunk,
          Text: "Initial greeting from recorded db.",
          Payload: { messageId: "m1" },
          CreatedAt: "2026-09-05T12:00:00Z",
          UpdatedAt: "2026-09-05T12:00:05Z",
        },
        {
          ID: "e2",
          Seq: 2,
          Kind: UpdateKind.UpdateToolCall,
          ToolCallID: "call-xyz",
          Payload: {
            toolCall: {
              id: "call-xyz",
              title: "Search repo",
              kind: "read",
              status: "completed",
            },
          },
          CreatedAt: "2026-09-05T12:00:10Z",
          UpdatedAt: "2026-09-05T12:00:12Z",
        },
        {
          ID: "e3",
          Seq: 3,
          Kind: UpdateKind.UpdateUsage,
          Payload: {
            usage: { used: 450, size: 100000, amount: 0.01, currency: "USD" },
          },
          CreatedAt: "2026-09-05T12:00:15Z",
        },
      ];

      const model = initTranscript(entries);

      expect(model.items.length).toBe(2);
      expect(model.items[0].id).toBe("e1");
      expect(model.items[0].text).toBe("Initial greeting from recorded db.");
      expect(model.items[1].toolCallId).toBe("call-xyz");
      expect(model.items[1].toolCall?.title).toBe("Search repo");
      expect(model.usage).toEqual({ used: 450, size: 100000, amount: 0.01, currency: "USD" });
    });

    it("seamlessly continues coalescing incoming live events after initial load", () => {
      const entries = [
        {
          ID: "e1",
          Kind: UpdateKind.UpdateAgentMessageChunk,
          Text: "Hello ",
          Payload: { messageId: "m1" },
        },
      ];

      let model = initTranscript(entries);
      expect(model.items.length).toBe(1);
      expect(model.items[0].text).toBe("Hello ");

      // Live stream chunk arrives for the same message
      model = applySessionUpdate(model, {
        kind: UpdateKind.UpdateAgentMessageChunk,
        content: { type: "text", text: "world!" },
        messageId: "m1",
      });

      expect(model.items.length).toBe(1);
      expect(model.items[0].text).toBe("Hello world!");
    });

    it("survives stringified JSON in Payload", () => {
      const entries = [
        {
          ID: "e1",
          Kind: UpdateKind.UpdateToolCall,
          ToolCallID: "c1",
          Payload: JSON.stringify({
            toolCall: { id: "c1", title: "Git status", status: "completed" },
          }),
        },
      ];

      const model = initTranscript(entries);
      expect(model.items.length).toBe(1);
      expect(model.items[0].toolCall?.title).toBe("Git status");
    });
  });

  describe("Reload Equivalence (Highest-risk Contract)", () => {
    it("guarantees live reducer model and persisted transcript model match exactly", () => {
      // 1. Live stream arrives as discrete events
      const liveEvents: SessionUpdate[] = [
        {
          kind: UpdateKind.UpdateAgentMessageChunk,
          content: { type: "text", text: "Inspecting " },
          messageId: "msg_1",
        },
        {
          kind: UpdateKind.UpdateAgentMessageChunk,
          content: { type: "text", text: "codebase..." },
          messageId: "msg_1",
        },
        {
          kind: UpdateKind.UpdateToolCall,
          toolCall: {
            id: "tool_1",
            title: "Grep search",
            kind: "search",
            status: "in_progress",
          },
        },
        {
          kind: UpdateKind.UpdateToolCallUpdate,
          toolCall: {
            id: "tool_1",
            status: "completed",
            content: [{ type: "diff", path: "/test.txt", newText: "fixed" }],
          },
        },
        {
          kind: UpdateKind.UpdateAgentMessageChunk,
          content: { type: "text", text: "Finished editing." },
          messageId: "msg_2",
        },
        {
          kind: UpdateKind.UpdateUsage,
          usage: { used: 300, size: 50000, amount: 0.02, currency: "USD" },
        },
      ];

      let liveModel = createInitialTranscriptModel();
      for (const ev of liveEvents) {
        liveModel = applySessionUpdate(liveModel, ev);
      }

      // 2. Simulate what the Go recorder persisted into SQLite:
      // - Consecutive chunks with same (kind, messageId) coalesced into 1 row
      // - Tool call updates merged onto toolCallId row
      // - Usage recorded into DB
      const persistedRows = [
        {
          ID: "db_row_1",
          Seq: 1,
          Kind: UpdateKind.UpdateAgentMessageChunk,
          Text: "Inspecting codebase...",
          Payload: {
            kind: UpdateKind.UpdateAgentMessageChunk,
            content: { type: "text", text: "Inspecting " },
            messageId: "msg_1",
          },
        },
        {
          ID: "db_row_2",
          Seq: 2,
          Kind: UpdateKind.UpdateToolCall,
          ToolCallID: "tool_1",
          Payload: {
            kind: UpdateKind.UpdateToolCall,
            toolCall: {
              id: "tool_1",
              title: "Grep search",
              kind: "search",
              status: "completed",
              content: [{ type: "diff", path: "/test.txt", newText: "fixed" }],
            },
          },
        },
        {
          ID: "db_row_3",
          Seq: 3,
          Kind: UpdateKind.UpdateAgentMessageChunk,
          Text: "Finished editing.",
          Payload: {
            kind: UpdateKind.UpdateAgentMessageChunk,
            content: { type: "text", text: "Finished editing." },
            messageId: "msg_2",
          },
        },
        {
          ID: "db_row_4",
          Seq: 4,
          Kind: UpdateKind.UpdateUsage,
          Payload: {
            kind: UpdateKind.UpdateUsage,
            usage: { used: 300, size: 50000, amount: 0.02, currency: "USD" },
          },
        },
      ];

      // 3. User reloads page, initial model constructed from persisted rows:
      const loadedModel = initTranscript(persistedRows);

      // 4. Assert exact parity
      expect(liveModel.items.length).toBe(loadedModel.items.length);
      expect(liveModel.items.length).toBe(3);

      // Row 0: Agent chunk 1
      expect(loadedModel.items[0].kind).toBe(liveModel.items[0].kind);
      expect(loadedModel.items[0].text).toBe(liveModel.items[0].text);
      expect(loadedModel.items[0].text).toBe("Inspecting codebase...");

      // Row 1: Tool call
      expect(loadedModel.items[1].toolCallId).toBe(liveModel.items[1].toolCallId);
      expect(loadedModel.items[1].toolCall?.title).toBe(liveModel.items[1].toolCall?.title);
      expect(loadedModel.items[1].toolCall?.status).toBe("completed");
      expect(loadedModel.items[1].toolCall?.content).toEqual(liveModel.items[1].toolCall?.content);

      // Row 2: Agent chunk 2
      expect(loadedModel.items[2].kind).toBe(liveModel.items[2].kind);
      expect(loadedModel.items[2].text).toBe(liveModel.items[2].text);
      expect(loadedModel.items[2].text).toBe("Finished editing.");

      // Session usage
      expect(loadedModel.usage).toEqual(liveModel.usage);
      expect(loadedModel.usage?.used).toBe(300);
    });
  });

  describe("transcriptReducer", () => {
    it("handles init, update, and reset actions", () => {
      const initial = createInitialTranscriptModel();

      const initialized = transcriptReducer(initial, {
        type: "init",
        entries: [
          {
            ID: "1",
            Kind: UpdateKind.UpdateAgentMessageChunk,
            Text: "Initial",
          },
        ],
      });
      expect(initialized.items.length).toBe(1);

      const updated = transcriptReducer(initialized, {
        type: "update",
        update: {
          kind: UpdateKind.UpdateAgentMessageChunk,
          content: { type: "text", text: " added" },
        },
      });
      expect(updated.items[0].text).toBe("Initial added");

      const reset = transcriptReducer(updated, { type: "reset" });
      expect(reset.items.length).toBe(0);
      expect(reset.usage).toBeNull();
    });
  });
});

describe("prompt attachments and subagent origin", () => {
  const attachments = [{ name: "screen.png", kind: "image" }];

  it("keeps them on the live item", () => {
    let model = createInitialTranscriptModel();
    model = applySessionUpdate(model, { kind: "user_message_chunk", messageId: "p1", content: { type: "text", text: "" }, attachments });
    model = applySessionUpdate(model, { kind: "agent_message_chunk", messageId: "msg_sub", origin: "a0549d", content: { type: "text", text: "from the subagent" } });
    expect(model.items[0]).toMatchObject({ messageId: "p1", attachments });
    expect(model.items[1]).toMatchObject({ messageId: "msg_sub", origin: "a0549d" });
  });

  it("keeps them on a reloaded page, so live and reload agree", () => {
    const model = initTranscript([
      { ID: "r1", Seq: 1, Kind: "user_message_chunk", Text: "", Payload: { messageId: "p1", attachments }, CreatedAt: "2026-09-17T00:00:00Z" },
      { ID: "r2", Seq: 2, Kind: "agent_message_chunk", Text: "hi", Payload: { messageId: "msg_sub", origin: "a0549d" }, CreatedAt: "2026-09-17T00:00:01Z" },
    ]);
    expect(model.items[0]).toMatchObject({ messageId: "p1", attachments });
    expect(model.items[1]).toMatchObject({ origin: "a0549d" });
    // The session's own rows carry no origin key at all.
    expect("origin" in model.items[0]).toBe(false);
  });
});
