import { useEffect } from "react";
import { UpdateKind, type TranscriptEntry } from "../../../bindings/devkit/internal/agenthub/models";
import { agentHubTranscriptPage, onAgentHubSessionUpdate } from "../../bridge/agenthub";
import {
  EMPTY_TRANSCRIPT_MODEL,
  useAgentSessionsStore,
} from "../../stores/agentSessions";
import type {
  SessionUpdate,
  ToolCall,
  TranscriptAction,
  TranscriptModel,
  Subagent,
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
    toolCallIndex: new Map(),
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
  return applySessionUpdates(model, [update]);
}

/**
 * Applies one frame's updates with one shallow item-array copy at most. The
 * mutable locals are deliberately scoped to this batch: the model in the
 * store remains immutable to readers between publishes.
 */
export function applySessionUpdates(
  model: TranscriptModel,
  updates: SessionUpdate[],
): TranscriptModel {
  let items = model.items;
  let itemsCopied = false;
  let toolCallIndex = model.toolCallIndex;
  let toolCallIndexCopied = false;
  let usage = model.usage;
  let commands = model.commands;
  let mode = model.mode;
  let changed = false;

  const copyItems = () => {
    if (!itemsCopied) {
      items = [...items];
      itemsCopied = true;
    }
  };
  const copyToolCallIndex = () => {
    if (!toolCallIndexCopied) {
      toolCallIndex = new Map(toolCallIndex);
      toolCallIndexCopied = true;
    }
  };
  const now = () => new Date().toISOString();

  for (const update of updates) {
    const kind = update.kind;

    if (kind === UpdateKind.UpdateUsage || kind === "usage_update") {
      if (update.usage) {
        usage = { ...update.usage };
        changed = true;
      }
      continue;
    }

    if (kind === UpdateKind.UpdateAvailableCommands || kind === "available_commands_update") {
      if (update.commands) {
        commands = [...update.commands];
        changed = true;
      }
      continue;
    }

    if (kind === UpdateKind.UpdateCurrentMode || kind === "current_mode_update") {
      if (update.mode !== undefined) {
        mode = update.mode;
        changed = true;
      }
      continue;
    }

    if (isChunkKind(kind)) {
      const chunkText = extractChunkText(update);
      const lastIndex = items.length - 1;
      const last = lastIndex >= 0 ? items[lastIndex] : null;
      copyItems();
      if (last && last.kind === kind && isSameMessage(last.messageId, update.messageId)) {
        items[lastIndex] = {
          ...last,
          text: (last.text ?? "") + chunkText,
          updatedAt: now(),
        };
      } else {
        items.push({
          id: generateId(),
          kind,
          text: chunkText,
          messageId: update.messageId,
          raw: update.raw,
          createdAt: now(),
          updatedAt: now(),
        });
      }
      changed = true;
      continue;
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
      const existingIndex = toolCallId ? (toolCallIndex.get(toolCallId) ?? -1) : -1;

      if (existingIndex !== -1) {
        const existing = items[existingIndex];
        copyItems();
        items[existingIndex] = {
          ...existing,
          toolCall: mergeToolCall(existing.toolCall, call),
          raw: update.raw ?? existing.raw,
          updatedAt: now(),
        };
      } else {
        copyItems();
        const index = items.length;
        items.push({
          id: generateId(),
          kind,
          toolCallId,
          toolCall: { ...call },
          raw: update.raw,
          createdAt: now(),
          updatedAt: now(),
        });
        if (toolCallId) {
          copyToolCallIndex();
          toolCallIndex.set(toolCallId, index);
        }
      }
      changed = true;
      continue;
    }

    if (kind === UpdateKind.UpdatePlan || kind === "plan") {
      copyItems();
      items.push({
        id: generateId(),
        kind,
        plan: update.plan ? [...update.plan] : [],
        raw: update.raw,
        createdAt: now(),
        updatedAt: now(),
      });
      changed = true;
      continue;
    }

    // Subagents: one card per subagent, not one row per event. Spawn carries the
    // name and the task, the state update carries only the id — so they are
    // paired here the way tool calls are, rather than rendered as two lines that
    // say half a thing each.
    if (
      kind === UpdateKind.UpdateSubagentSpawned ||
      kind === UpdateKind.UpdateSubagentState ||
      kind === "subagent_spawned" ||
      kind === "subagent_state_update"
    ) {
      const incoming = update.subagent;
      if (incoming?.id) {
        const index = items.findIndex((item) => item.subagent?.id === incoming.id);
        copyItems();
        if (index >= 0) {
          const previous = items[index];
          const merged: Subagent = { ...previous.subagent, ...incoming };
          // Spawn is the only message with a name; a state update must not blank it.
          merged.name = incoming.name || previous.subagent?.name;
          merged.task = incoming.task || previous.subagent?.task;
          items[index] = {
            ...previous,
            subagent: merged,
            raw: update.raw ?? previous.raw,
            updatedAt: now(),
          };
        } else {
          items.push({
            id: generateId(),
            kind: UpdateKind.UpdateSubagentSpawned,
            subagent: { ...incoming },
            raw: update.raw,
            createdAt: now(),
            updatedAt: now(),
          });
        }
        changed = true;
        continue;
      }
    }

    // Unknown / unmodelled kinds: kept intact with raw payload.
    copyItems();
    items.push({
      id: generateId(),
      kind,
      raw: update.raw,
      createdAt: now(),
      updatedAt: now(),
    });
    changed = true;
  }

  if (!changed) return model;
  return {
    ...model,
    items,
    usage,
    commands,
    mode,
    toolCallIndex,
  };
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
    const seqRaw = asEntry.Seq ?? rec.seq;
    const seq = typeof seqRaw === "number" && seqRaw > 0 ? seqRaw : undefined;

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
          seq,
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
      const existingIndex = activeToolCallId
        ? (model.toolCallIndex.get(activeToolCallId) ?? -1)
        : -1;

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
          seq,
          toolCallId: activeToolCallId,
          toolCall: call,
          raw: payload.raw,
          createdAt,
          updatedAt,
        });
        if (activeToolCallId) {
          model.toolCallIndex.set(activeToolCallId, model.items.length - 1);
        }
      }
      continue;
    }

    if (kind === UpdateKind.UpdatePlan || kind === "plan") {
      model.items.push({
        id,
        kind,
        seq,
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
      seq,
      raw: payload.raw ?? asEntry.Payload ?? rec.payload,
      createdAt,
      updatedAt,
    });
  }

  return model;
}

/**
 * Prepends one older page onto the front of a model. The backend snaps page
 * boundaries to turn starts, so no chunk run is ever split across the seam
 * and the two item lists concatenate; the tool call index is rebuilt with the
 * older page's offsets underneath. Session-level values (usage, commands,
 * mode) stay the current ones — the page's are older by definition.
 */
export function prependTranscriptEntries(
  model: TranscriptModel,
  entries: (TranscriptEntry | Record<string, any>)[],
): TranscriptModel {
  const page = initTranscript(entries);
  if (page.items.length === 0) return model;
  const toolCallIndex = new Map(page.toolCallIndex);
  for (const [id, index] of model.toolCallIndex) {
    toolCallIndex.set(id, index + page.items.length);
  }
  return {
    ...model,
    items: [...page.items, ...model.items],
    toolCallIndex,
  };
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

export type TranscriptFrameScheduler = (callback: () => void) => () => void;

/** Schedule a frame, with a bounded fallback for hidden WebKit windows. */
export const scheduleTranscriptFrame: TranscriptFrameScheduler = (callback) => {
  let active = true;
  let frameId: number | undefined;
  const timeoutId = setTimeout(run, 50);

  function run() {
    if (!active) return;
    active = false;
    if (frameId !== undefined && typeof cancelAnimationFrame === "function") {
      cancelAnimationFrame(frameId);
    }
    clearTimeout(timeoutId);
    callback();
  }

  if (typeof requestAnimationFrame === "function") {
    frameId = requestAnimationFrame(run);
  }
  return () => {
    active = false;
    if (frameId !== undefined && typeof cancelAnimationFrame === "function") {
      cancelAnimationFrame(frameId);
    }
    clearTimeout(timeoutId);
  };
};

/**
 * Mutable event buffer used by the shell-owned stream. It publishes only on
 * flush, while retaining immutable snapshots for Zustand/React readers.
 */
export class TranscriptEventBuffer {
  private model: TranscriptModel;
  private pending: SessionUpdate[] = [];
  private cancelScheduledFlush: (() => void) | null = null;

  constructor(
    model: TranscriptModel,
    private readonly publish: (model: TranscriptModel) => void,
    private readonly schedule: TranscriptFrameScheduler = scheduleTranscriptFrame,
  ) {
    this.model = model;
  }

  enqueue(update: SessionUpdate): void {
    this.pending.push(update);
    if (this.cancelScheduledFlush) return;
    this.cancelScheduledFlush = this.schedule(() => {
      this.cancelScheduledFlush = null;
      this.flush();
    });
  }

  /** Control-plane updates take the current queue with them and publish now. */
  applyImmediate(update: SessionUpdate): void {
    this.cancelScheduledFlush?.();
    this.cancelScheduledFlush = null;
    this.pending.push(update);
    this.flush();
  }

  flush(): void {
    if (this.pending.length === 0) return;
    const pending = this.pending;
    this.pending = [];
    this.model = applySessionUpdates(this.model, pending);
    this.publish(this.model);
  }

  get currentModel(): TranscriptModel {
    return this.model;
  }

  dispose(): void {
    this.cancelScheduledFlush?.();
    this.cancelScheduledFlush = null;
    this.pending = [];
  }
}

const IMMEDIATE_UPDATE_KINDS = new Set<string>([
  UpdateKind.UpdateUsage,
  UpdateKind.UpdateAvailableCommands,
  UpdateKind.UpdateCurrentMode,
  UpdateKind.UpdateSessionInfo,
  UpdateKind.UpdateConfigOptions,
  UpdateKind.UpdateAsyncTaskSpawned,
  UpdateKind.UpdateAsyncTaskProgress,
  UpdateKind.UpdateAsyncTaskState,
  "usage_update",
  "available_commands_update",
  "current_mode_update",
  "session_info_update",
  "config_option_update",
  "async_task_spawned",
  "async_task_progress",
  "async_task_state_update",
]);

export function isImmediateTranscriptUpdate(update: SessionUpdate): boolean {
  return IMMEDIATE_UPDATE_KINDS.has(String(update.kind));
}

type TranscriptRuntime = {
  model: TranscriptModel;
  loading: boolean;
  loadingUpdates: SessionUpdate[];
  buffer: TranscriptEventBuffer | null;
  loadPromise: Promise<void> | null;
  /** The seq of the oldest page edge held; where the backwards walk resumes. */
  oldestSeq: number | null;
  loadingOlder: boolean;
};

const runtimes = new Map<string, TranscriptRuntime>();
let streamStarted = false;

const NO_MORE_PAGES = { hasMore: false, loadingOlder: false };

function entrySeq(entry: TranscriptEntry | Record<string, any>): number | null {
  const raw = (entry as TranscriptEntry).Seq ?? (entry as Record<string, any>).seq;
  return typeof raw === "number" && raw > 0 ? raw : null;
}

function publishTranscript(sessionId: string, model: TranscriptModel): void {
  const state = useAgentSessionsStore.getState();
  state.setTranscript(sessionId, model);
}

function makeBuffer(sessionId: string, model: TranscriptModel): TranscriptEventBuffer {
  return new TranscriptEventBuffer(model, (next) => {
    const runtime = runtimes.get(sessionId);
    if (!runtime) return;
    runtime.model = next;
    publishTranscript(sessionId, next);
  });
}

function receiveTranscriptUpdate(sessionId: string, update: SessionUpdate): void {
  const runtime = runtimes.get(sessionId);
  if (!runtime) return;
  if (runtime.loading) {
    runtime.loadingUpdates.push(update);
    return;
  }
  runtime.buffer ??= makeBuffer(sessionId, runtime.model);
  if (isImmediateTranscriptUpdate(update)) runtime.buffer.applyImmediate(update);
  else runtime.buffer.enqueue(update);
}

/** Starts the one shell-level event listener; repeated calls are harmless. */
export function startAgentTranscriptStream(): void {
  if (streamStarted) return;
  streamStarted = true;
  onAgentHubSessionUpdate((event) => {
    if (event.parsed) receiveTranscriptUpdate(event.sessionId, event.parsed);
  });
}

async function loadTranscriptRuntime(sessionId: string, runtime: TranscriptRuntime): Promise<void> {
  try {
    // The newest window, not the whole conversation: an old session used to
    // ship months of rows on every open. Older pages arrive on demand as the
    // reader scrolls up (loadOlderTranscript).
    const page = await agentHubTranscriptPage(sessionId, 0, 0);
    if (runtimes.get(sessionId) !== runtime) return;
    const model = applySessionUpdates(initTranscript(page.entries), runtime.loadingUpdates);
    runtime.loadingUpdates = [];
    runtime.model = model;
    runtime.buffer = makeBuffer(sessionId, model);
    runtime.loading = false;
    runtime.oldestSeq = page.entries.length > 0 ? entrySeq(page.entries[0]) : null;
    const state = useAgentSessionsStore.getState();
    state.setTranscript(sessionId, model);
    state.setTranscriptError(sessionId, null);
    state.setTranscriptPaging(sessionId, { hasMore: page.hasMore, loadingOlder: false });
  } catch (err) {
    if (runtimes.get(sessionId) !== runtime) return;
    const error = err instanceof Error ? err : new Error(String(err));
    const model = applySessionUpdates(createInitialTranscriptModel(), runtime.loadingUpdates);
    runtime.loadingUpdates = [];
    runtime.model = model;
    runtime.buffer = makeBuffer(sessionId, model);
    runtime.loading = false;
    runtime.oldestSeq = null;
    const state = useAgentSessionsStore.getState();
    state.setTranscript(sessionId, model);
    state.setTranscriptError(sessionId, error);
    state.setTranscriptPaging(sessionId, NO_MORE_PAGES);
  } finally {
    runtime.loadPromise = null;
    if (runtimes.get(sessionId) === runtime) {
      useAgentSessionsStore.getState().setTranscriptLoading(sessionId, false);
    }
  }
}

/**
 * Walks one page further back and prepends it. The view anchors the scroll
 * before the items land (TranscriptStream), so the line being read stays put
 * while the older history appears above it.
 */
function loadOlderTranscript(sessionId: string): void {
  const runtime = runtimes.get(sessionId);
  if (!runtime || runtime.loading || runtime.loadingOlder) return;
  const before = runtime.oldestSeq;
  if (before === null) return;
  const state = useAgentSessionsStore.getState();
  const paging = state.transcriptPagingById[sessionId] ?? NO_MORE_PAGES;
  if (!paging.hasMore) return;

  runtime.loadingOlder = true;
  state.setTranscriptPaging(sessionId, { ...paging, loadingOlder: true });
  void (async () => {
    try {
      const page = await agentHubTranscriptPage(sessionId, before, 0);
      if (runtimes.get(sessionId) !== runtime) return;
      // Land anything the buffer still holds first: the prepend merges from
      // the buffer's own model, and losing the pending frame would drop the
      // newest messages the reader just watched arrive.
      runtime.buffer?.flush();
      const merged = prependTranscriptEntries(runtime.model, page.entries);
      runtime.model = merged;
      runtime.buffer = makeBuffer(sessionId, merged);
      runtime.oldestSeq = page.entries.length > 0 ? entrySeq(page.entries[0]) : null;
      const next = useAgentSessionsStore.getState();
      next.setTranscript(sessionId, merged);
      next.setTranscriptPaging(sessionId, { hasMore: page.hasMore, loadingOlder: false });
    } catch {
      if (runtimes.get(sessionId) !== runtime) return;
      // The walk stops but the history is still up there; say so rather than
      // pretending the transcript is complete.
      useAgentSessionsStore
        .getState()
        .setTranscriptPaging(sessionId, { hasMore: true, loadingOlder: false });
    } finally {
      runtime.loadingOlder = false;
    }
  })();
}

function ensureTranscript(sessionId: string): void {
  if (!sessionId) return;
  startAgentTranscriptStream();
  let runtime = runtimes.get(sessionId);
  if (!runtime) {
    runtime = {
      model: createInitialTranscriptModel(),
      loading: true,
      loadingUpdates: [],
      buffer: null,
      loadPromise: null,
      oldestSeq: null,
      loadingOlder: false,
    };
    runtimes.set(sessionId, runtime);
  }
  if (!runtime.loadPromise && runtime.loading) {
    useAgentSessionsStore.getState().setTranscriptLoading(sessionId, true);
    runtime.loadPromise = loadTranscriptRuntime(sessionId, runtime);
  }
}

function reloadTranscript(sessionId: string): void {
  const runtime = runtimes.get(sessionId);
  if (!runtime || runtime.loading) return;
  runtime.loading = true;
  runtime.loadingUpdates = [];
  runtime.oldestSeq = null;
  runtime.buffer?.flush();
  runtime.buffer = null;
  useAgentSessionsStore.getState().setTranscriptLoading(sessionId, true);
  runtime.loadPromise = loadTranscriptRuntime(sessionId, runtime);
}

/** Drops the shell cache when the backend session is deleted. */
export function forgetAgentTranscript(sessionId: string): void {
  const runtime = runtimes.get(sessionId);
  runtime?.buffer?.dispose();
  runtimes.delete(sessionId);
}

/** React is only a reader of the shell-owned model and never owns the stream. */
export function useAgentTranscript(sessionId: string) {
  const model = useAgentSessionsStore(
    (state) => state.transcriptsById[sessionId] ?? EMPTY_TRANSCRIPT_MODEL,
  );
  const loading = useAgentSessionsStore(
    (state) => Boolean(sessionId) && Boolean(state.transcriptLoadingById[sessionId]),
  );
  const error = useAgentSessionsStore(
    (state) => state.transcriptErrorsById[sessionId] ?? null,
  );
  const paging = useAgentSessionsStore(
    (state) => state.transcriptPagingById[sessionId] ?? NO_MORE_PAGES,
  );

  useEffect(() => {
    ensureTranscript(sessionId);
  }, [sessionId]);

  return {
    ...model,
    loading,
    error,
    reload: () => reloadTranscript(sessionId),
    hasMore: paging.hasMore,
    loadingOlder: paging.loadingOlder,
    loadOlder: () => loadOlderTranscript(sessionId),
  };
}
