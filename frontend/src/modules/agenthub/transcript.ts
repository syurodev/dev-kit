import { useCallback, useEffect, useRef, useState } from "react";
import { UpdateKind, type TranscriptEntry } from "../../../bindings/devkit/internal/agenthub/models";
import { agentHubTranscript, onAgentHubSessionUpdate } from "../../bridge/agenthub";
import type {
  SessionUpdate,
  ToolCall,
  TranscriptAction,
  TranscriptItem,
  TranscriptModel,
} from "./types";

export type {
  AvailableCommand,
  ContentBlock,
  PlanEntry,
  SessionUpdate,
  ToolCall,
  ToolCallContent,
  ToolCallLocation,
  TranscriptAction,
  TranscriptItem,
  TranscriptModel,
  Usage,
} from "./types";

let fallbackIdCounter = 0;
function generateId(): string {
  if (typeof crypto !== "undefined" && typeof crypto.randomUUID === "function") {
    return crypto.randomUUID();
  }
  return `trans-${Date.now()}-${++fallbackIdCounter}`;
}

export function createInitialTranscriptModel(): TranscriptModel {
  return {
    items: [],
    usage: null,
    commands: [],
    mode: null,
  };
}

/**
 * ACP streaming chunk kinds that coalesce into consecutive text runs.
 * Mirrors internal/agenthub/update.go: (SessionUpdate).IsChunk()
 */
export function isChunkKind(kind: UpdateKind | string): boolean {
  return (
    kind === UpdateKind.UpdateUserMessageChunk ||
    kind === UpdateKind.UpdateAgentMessageChunk ||
    kind === UpdateKind.UpdateAgentThoughtChunk ||
    kind === "user_message_chunk" ||
    kind === "agent_message_chunk" ||
    kind === "agent_thought_chunk"
  );
}

/**
 * Extracts chunk text from a session update.
 * Mirrors internal/agenthub/update.go: (SessionUpdate).Text()
 */
export function extractChunkText(update: SessionUpdate): string {
  if (!update.content) {
    return "";
  }
  return update.content.text ?? "";
}

/**
 * Evaluates message boundary equality.
 * Empty message IDs mean no boundary was declared, matching Go's sameMessage.
 * Mirrors internal/agenthub/transcript.go: sameMessage
 */
export function isSameMessage(prevMessageId?: string, nextMessageId?: string): boolean {
  if (!nextMessageId) {
    return true;
  }
  if (!prevMessageId) {
    return true;
  }
  return prevMessageId === nextMessageId;
}

/**
 * Merges a tool_call_update onto an existing tool call record.
 * Empty string / zero-length fields in next mean unchanged, not cleared.
 * Content and locations replace wholesale when present.
 * Mirrors internal/agenthub/transcript.go: mergeToolCall
 */
export function mergeToolCall(
  prev?: ToolCall | null,
  next?: ToolCall | null,
): ToolCall {
  if (!prev) {
    return next ? { ...next } : { id: "" };
  }
  if (!next) {
    return { ...prev };
  }

  const merged: ToolCall = { ...prev };
  if (next.id) {
    merged.id = next.id;
  }
  if (next.title) {
    merged.title = next.title;
  }
  if (next.kind) {
    merged.kind = next.kind;
  }
  if (next.status) {
    merged.status = next.status;
  }
  if (next.content && next.content.length > 0) {
    merged.content = next.content;
  }
  if (next.locations && next.locations.length > 0) {
    merged.locations = next.locations;
  }
  if (next.rawInput !== undefined && next.rawInput !== null && next.rawInput !== "") {
    merged.rawInput = next.rawInput;
  }
  if (next.rawOutput !== undefined && next.rawOutput !== null && next.rawOutput !== "") {
    merged.rawOutput = next.rawOutput;
  }
  return merged;
}

export function parsePayload(payload: unknown): Partial<SessionUpdate> {
  if (!payload) {
    return {};
  }
  if (typeof payload === "string") {
    try {
      return JSON.parse(payload);
    } catch {
      return {};
    }
  }
  if (typeof payload === "object") {
    return payload as Partial<SessionUpdate>;
  }
  return {};
}

/**
 * Applies a live SessionUpdate onto a TranscriptModel.
 * Reproduces the Go recorder's semantics:
 * - Chunks coalesce while kind AND messageId match the immediately preceding item.
 * - Tool call updates merge onto the same toolCallId, empty fields left intact.
 * - Plan updates append as new rows.
 * - Usage updates session-level usage (not added to transcript items).
 * - Available commands update session-level commands list.
 * - Current mode updates session-level mode.
 * - Unknown kinds are appended as intact rows with raw payload preserved.
 */
export function applySessionUpdate(
  model: TranscriptModel,
  update: SessionUpdate,
): TranscriptModel {
  const kind = update.kind;

  if (kind === UpdateKind.UpdateUsage || kind === "usage_update") {
    return {
      ...model,
      usage: update.usage ? { ...update.usage } : model.usage,
    };
  }

  if (kind === UpdateKind.UpdateAvailableCommands || kind === "available_commands_update") {
    return {
      ...model,
      commands: update.commands ? [...update.commands] : model.commands,
    };
  }

  if (kind === UpdateKind.UpdateCurrentMode || kind === "current_mode_update") {
    return {
      ...model,
      mode: update.mode ?? model.mode,
    };
  }

  if (isChunkKind(kind)) {
    const chunkText = extractChunkText(update);
    const lastIndex = model.items.length - 1;
    const last = lastIndex >= 0 ? model.items[lastIndex] : null;

    if (last && last.kind === kind && isSameMessage(last.messageId, update.messageId)) {
      const updatedItem: TranscriptItem = {
        ...last,
        text: (last.text ?? "") + chunkText,
        updatedAt: new Date().toISOString(),
      };
      const nextItems = [...model.items];
      nextItems[lastIndex] = updatedItem;
      return { ...model, items: nextItems };
    }

    const newItem: TranscriptItem = {
      id: generateId(),
      kind,
      text: chunkText,
      messageId: update.messageId,
      raw: update.raw,
      createdAt: new Date().toISOString(),
      updatedAt: new Date().toISOString(),
    };
    return { ...model, items: [...model.items, newItem] };
  }

  if (
    kind === UpdateKind.UpdateToolCall ||
    kind === UpdateKind.UpdateToolCallUpdate ||
    kind === "tool_call" ||
    kind === "tool_call_update" ||
    (update.toolCall && update.toolCall.id)
  ) {
    const call = update.toolCall ?? { id: "" };
    const toolCallId = call.id;

    if (toolCallId) {
      const existingIndex = model.items.findIndex(
        (item) => item.toolCallId === toolCallId || item.toolCall?.id === toolCallId,
      );

      if (existingIndex !== -1) {
        const existing = model.items[existingIndex];
        const merged = mergeToolCall(existing.toolCall, call);
        const updatedItem: TranscriptItem = {
          ...existing,
          toolCall: merged,
          raw: update.raw ?? existing.raw,
          updatedAt: new Date().toISOString(),
        };
        const nextItems = [...model.items];
        nextItems[existingIndex] = updatedItem;
        return { ...model, items: nextItems };
      }
    }

    const newItem: TranscriptItem = {
      id: generateId(),
      kind,
      toolCallId,
      toolCall: { ...call },
      raw: update.raw,
      createdAt: new Date().toISOString(),
      updatedAt: new Date().toISOString(),
    };
    return { ...model, items: [...model.items, newItem] };
  }

  if (kind === UpdateKind.UpdatePlan || kind === "plan") {
    const newItem: TranscriptItem = {
      id: generateId(),
      kind,
      plan: update.plan ? [...update.plan] : [],
      raw: update.raw,
      createdAt: new Date().toISOString(),
      updatedAt: new Date().toISOString(),
    };
    return { ...model, items: [...model.items, newItem] };
  }

  // Unknown / unmodelled kinds: kept intact with raw payload
  const newItem: TranscriptItem = {
    id: generateId(),
    kind,
    raw: update.raw,
    createdAt: new Date().toISOString(),
    updatedAt: new Date().toISOString(),
  };
  return { ...model, items: [...model.items, newItem] };
}

/**
 * Initializes a TranscriptModel from persisted TranscriptEntry rows.
 * Matches the Go recorder's coalescing and extraction rules.
 */
export function initTranscript(
  entries: (TranscriptEntry | Record<string, any>)[],
): TranscriptModel {
  const model: TranscriptModel = createInitialTranscriptModel();
  if (!Array.isArray(entries)) {
    return model;
  }

  for (const entry of entries) {
    const rec = entry as Record<string, any>;
    const asEntry = entry as TranscriptEntry;

    const id = asEntry.ID ?? rec.id ?? generateId();
    const kind = (asEntry.Kind ?? rec.kind ?? "") as UpdateKind | string;
    const toolCallId = (asEntry.ToolCallID ?? rec.toolCallId ?? "") as string;
    const text = (asEntry.Text ?? rec.text ?? "") as string;
    const payload = parsePayload(asEntry.Payload ?? rec.payload);
    const createdAt = asEntry.CreatedAt ?? rec.createdAt;
    const updatedAt = asEntry.UpdatedAt ?? rec.updatedAt;

    if (kind === UpdateKind.UpdateUsage || kind === "usage_update") {
      if (payload.usage) {
        model.usage = payload.usage;
      }
      continue;
    }

    if (kind === UpdateKind.UpdateAvailableCommands || kind === "available_commands_update") {
      if (payload.commands) {
        model.commands = payload.commands;
      }
      continue;
    }

    if (kind === UpdateKind.UpdateCurrentMode || kind === "current_mode_update") {
      if (payload.mode) {
        model.mode = payload.mode;
      }
      continue;
    }

    if (isChunkKind(kind)) {
      const chunkText = text || payload.content?.text || "";
      const messageId = payload.messageId;
      const lastIndex = model.items.length - 1;
      const last = lastIndex >= 0 ? model.items[lastIndex] : null;

      if (last && last.kind === kind && isSameMessage(last.messageId, messageId)) {
        model.items[lastIndex] = {
          ...last,
          text: (last.text ?? "") + chunkText,
          updatedAt: updatedAt ?? new Date().toISOString(),
        };
      } else {
        model.items.push({
          id,
          kind,
          text: chunkText,
          messageId,
          raw: payload.raw,
          createdAt,
          updatedAt,
        });
      }
      continue;
    }

    if (
      kind === UpdateKind.UpdateToolCall ||
      kind === UpdateKind.UpdateToolCallUpdate ||
      kind === "tool_call" ||
      kind === "tool_call_update" ||
      toolCallId
    ) {
      const call = payload.toolCall ?? { id: toolCallId };
      const activeToolCallId = toolCallId || call.id;
      const existingIndex = model.items.findIndex(
        (item) => item.toolCallId === activeToolCallId || item.toolCall?.id === activeToolCallId,
      );

      if (existingIndex !== -1) {
        const existing = model.items[existingIndex];
        model.items[existingIndex] = {
          ...existing,
          toolCall: mergeToolCall(existing.toolCall, call),
          raw: payload.raw ?? existing.raw,
          updatedAt: updatedAt ?? new Date().toISOString(),
        };
      } else {
        model.items.push({
          id,
          kind,
          toolCallId: activeToolCallId,
          toolCall: call,
          raw: payload.raw,
          createdAt,
          updatedAt,
        });
      }
      continue;
    }

    if (kind === UpdateKind.UpdatePlan || kind === "plan") {
      model.items.push({
        id,
        kind,
        plan: payload.plan ?? [],
        raw: payload.raw,
        createdAt,
        updatedAt,
      });
      continue;
    }

    // Unmodelled / unknown kinds: preserved intact with raw payload
    model.items.push({
      id,
      kind,
      raw: payload.raw ?? asEntry.Payload ?? rec.payload,
      createdAt,
      updatedAt,
    });
  }

  return model;
}

/**
 * Pure reducer function for transcript state transitions.
 */
export function transcriptReducer(
  state: TranscriptModel,
  action: TranscriptAction,
): TranscriptModel {
  switch (action.type) {
    case "init":
      return initTranscript(action.entries);
    case "update":
      return applySessionUpdate(state, action.update);
    case "reset":
      return createInitialTranscriptModel();
    default:
      return state;
  }
}

/**
 * React hook that loads persisted transcript rows once and subscribes
 * to incoming session updates for live conversation streaming.
 */
export function useAgentTranscript(sessionId: string) {
  const [model, setModel] = useState<TranscriptModel>(createInitialTranscriptModel);
  const [loading, setLoading] = useState<boolean>(Boolean(sessionId));
  const [error, setError] = useState<Error | null>(null);

  const loadingRef = useRef(loading);
  loadingRef.current = loading;
  const bufferedUpdatesRef = useRef<SessionUpdate[]>([]);

  const load = useCallback(async () => {
    if (!sessionId) {
      setModel(createInitialTranscriptModel());
      setLoading(false);
      return;
    }
    setLoading(true);
    setError(null);
    bufferedUpdatesRef.current = [];
    try {
      const entries = await agentHubTranscript(sessionId);
      let nextModel = initTranscript(entries);
      for (const buffered of bufferedUpdatesRef.current) {
        nextModel = applySessionUpdate(nextModel, buffered);
      }
      bufferedUpdatesRef.current = [];
      setModel(nextModel);
    } catch (err) {
      setError(err instanceof Error ? err : new Error(String(err)));
    } finally {
      setLoading(false);
    }
  }, [sessionId]);

  useEffect(() => {
    load();
  }, [load]);

  useEffect(() => {
    if (!sessionId) {
      return;
    }
    return onAgentHubSessionUpdate((event) => {
      if (event.sessionId !== sessionId) {
        return;
      }
      if (!event.parsed) {
        return;
      }
      if (loadingRef.current) {
        bufferedUpdatesRef.current.push(event.parsed);
      } else {
        setModel((prev) => applySessionUpdate(prev, event.parsed!));
      }
    });
  }, [sessionId]);

  return {
    ...model,
    loading,
    error,
    reload: load,
  };
}
