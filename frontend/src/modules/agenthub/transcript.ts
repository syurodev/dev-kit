import { useCallback, useEffect, useMemo, useSyncExternalStore } from "react";
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
  TranscriptItem,
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
  ConfigOption,
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
    configOptions: [],
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

function livePlanIndex(items: TranscriptItem[]): number {
  for (let index = items.length - 1; index >= 0; index -= 1) {
    const kind = String(items[index].kind);
    if (kind === "user_message_chunk") return -1;
    if (kind === "plan") return index;
  }
  return -1;
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
 * - Plan updates replace the live plan for the current turn.
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
  let configOptions = model.configOptions;
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

    if (kind === UpdateKind.UpdateConfigOptions || kind === "config_option_update") {
      if (update.configOptions) {
        configOptions = update.configOptions.map((option) => ({ ...option, options: option.options ? [...option.options] : [] }));
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
      const existingIndex = livePlanIndex(items);
      const timestamp = now();
      const existing = existingIndex >= 0 ? items[existingIndex] : undefined;
      const next = {
        id: existing?.id ?? generateId(),
        kind,
        plan: update.plan ? [...update.plan] : [],
        raw: update.raw,
        createdAt: existing?.createdAt ?? timestamp,
        updatedAt: timestamp,
      };
      if (existingIndex >= 0) items[existingIndex] = next;
      else items.push(next);
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
    configOptions,
    toolCallIndex,
  };
}

/**
 * Initializes a TranscriptModel from persisted TranscriptEntry rows.
 * Matches the Go recorder's coalescing and extraction rules.
 */
export function initTranscript(
  entries: (TranscriptEntry | Record<string, any>)[],
  sessionId?: string,
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

    if (kind === UpdateKind.UpdateConfigOptions || kind === "config_option_update") {
      if (payload.configOptions) model.configOptions = payload.configOptions;
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
          sessionId,
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
          sessionId,
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
      const existingIndex = livePlanIndex(model.items);
      const existing = existingIndex >= 0 ? model.items[existingIndex] : undefined;
      const next = {
        id: existing?.id ?? id,
        kind,
        seq,
        sessionId,
        plan: payload.plan ?? [],
        raw: payload.raw,
        createdAt: existing?.createdAt ?? createdAt,
        updatedAt,
      };
      if (existingIndex >= 0) model.items[existingIndex] = next;
      else model.items.push(next);
      continue;
    }

    // Unmodelled / unknown kinds: preserved intact with raw payload
    model.items.push({
      id,
      kind,
      seq,
      sessionId,
      raw: payload.raw ?? asEntry.Payload ?? rec.payload,
      createdAt,
      updatedAt,
    });
  }

  return model;
}

/**
 * Session-level values the model lacks, filled from another page. Values the
 * model already holds win: they came from the newest page or a live update,
 * and nothing an older page carries is newer than either.
 */
function fillSessionState(model: TranscriptModel, page: TranscriptModel): TranscriptModel {
  const usage = model.usage ?? page.usage;
  const mode = model.mode ?? page.mode;
  const commands = model.commands.length > 0 || page.commands.length === 0 ? model.commands : page.commands;
  const configOptions =
    model.configOptions.length > 0 || page.configOptions.length === 0 ? model.configOptions : page.configOptions;
  if (
    usage === model.usage &&
    mode === model.mode &&
    commands === model.commands &&
    configOptions === model.configOptions
  ) {
    return model;
  }
  return { ...model, usage, mode, commands, configOptions };
}

/**
 * Prepends one older page onto the front of a model. The backend snaps page
 * boundaries to turn starts, so no chunk run is ever split across the seam
 * and the two item lists concatenate; the tool call index is rebuilt with the
 * older page's offsets underneath. Session-level values the model already
 * holds stay; ones it lacks are taken from the page.
 */
export function prependTranscriptEntries(
  model: TranscriptModel,
  entries: (TranscriptEntry | Record<string, any>)[],
  sessionId?: string,
): TranscriptModel {
  const page = initTranscript(entries, sessionId);
  const base = fillSessionState(model, page);
  if (page.items.length === 0) return base;
  const toolCallIndex = new Map(page.toolCallIndex);
  for (const [id, index] of base.toolCallIndex) {
    toolCallIndex.set(id, index + page.items.length);
  }
  return {
    ...base,
    items: [...page.items, ...base.items],
    toolCallIndex,
  };
}

/**
 * Chunk updates that arrived while the newest page was loading are usually in
 * that page already — the backend flushes the recorder before it reads — and
 * appending them again doubled the tail of a streaming reply. Tool calls,
 * plans and session state merge idempotently; chunks do not. So for each
 * message the page holds, the longest leading run of its buffered chunks that
 * the stored text already ends with is dropped: the recorder writes chunks in
 * arrival order, so what it has is always a prefix of what was buffered.
 *
 * ponytail: a text-suffix match, not a cursor. A chunk still between emit and
 * record when the page was read, whose text also happens to repeat the end of
 * the stored text, is dropped too; an exact answer needs a per-update sequence
 * on the live event.
 */
export function dropRecordedChunks(model: TranscriptModel, updates: SessionUpdate[]): SessionUpdate[] {
  const stored = new Map<string, string>();
  for (const item of model.items) {
    if (isChunkKind(item.kind) && item.messageId) stored.set(`${item.kind} ${item.messageId}`, item.text ?? "");
  }
  const buffered = new Map<string, number[]>();
  updates.forEach((update, index) => {
    const key = `${update.kind} ${update.messageId ?? ""}`;
    if (!isChunkKind(update.kind) || !update.messageId || !stored.has(key)) return;
    buffered.set(key, [...(buffered.get(key) ?? []), index]);
  });
  const dropped = new Set<number>();
  for (const [key, indexes] of buffered) {
    const text = stored.get(key) ?? "";
    for (let run = indexes.length; run > 0; run -= 1) {
      const joined = indexes.slice(0, run).map((index) => extractChunkText(updates[index])).join("");
      if (text.endsWith(joined)) {
        for (const index of indexes.slice(0, run)) dropped.add(index);
        break;
      }
    }
  }
  return dropped.size === 0 ? updates : updates.filter((_, index) => !dropped.has(index));
}

/**
 * The model a session opens on: the newest page, the session state from above
 * its window, and whatever arrived live while it loaded that the page does not
 * already hold.
 */
export function modelFromNewestPage(
  page: {
    entries: (TranscriptEntry | Record<string, any>)[];
    state: (TranscriptEntry | Record<string, any>)[];
  },
  loadingUpdates: SessionUpdate[],
  sessionId?: string,
): TranscriptModel {
  const model = initTranscript([...page.state, ...page.entries], sessionId);
  return applySessionUpdates(model, dropRecordedChunks(model, loadingUpdates));
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

  replaceModel(model: TranscriptModel): void {
    this.model = model;
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
  /** Bumped by a reload; an older page asked for in an earlier generation is dropped. */
  generation: number;
};

const runtimes = new Map<string, TranscriptRuntime>();
/** How many sessions keep a transcript in memory; the rest reload their newest page when reopened. */
export const TRANSCRIPT_CACHE_LIMIT = 8;
let streamStarted = false;

export function updateTranscriptControls(
  sessionId: string,
  update: (model: TranscriptModel) => TranscriptModel,
): TranscriptModel | null {
  const runtime = runtimes.get(sessionId);
  if (!runtime) return null;
  const next = update(runtime.model);
  runtime.model = next;
  runtime.buffer?.replaceModel(next);
  publishTranscript(sessionId, next);
  return next;
}

const NO_MORE_PAGES = { hasMore: false, loadingOlder: false };

type AgentSessionsStoreState = ReturnType<typeof useAgentSessionsStore.getState>;
const noopSubscribe = () => () => {};

/**
 * A store value, subscribed to only while enabled. Disabled, a token flush no
 * longer renders the surface, but the value is still the store's own: an
 * empty stand-in unmounted every turn (and its open/closed state) when the
 * Agent Hub tab was hidden, and returning re-grouped the whole history. On
 * re-enable the current snapshot is read in the same render, so nothing flashes.
 */
function useTranscriptStoreValue<T>(enabled: boolean, selector: (state: AgentSessionsStoreState) => T): T {
  const getSnapshot = useCallback(() => selector(useAgentSessionsStore.getState()), [selector]);
  return useSyncExternalStore(
    enabled ? useAgentSessionsStore.subscribe : noopSubscribe,
    getSnapshot,
    getSnapshot,
  );
}

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
  // A session that leaves the store (deleted, or missing from a reloaded
  // list) takes its transcript with it: nothing can open it again.
  useAgentSessionsStore.subscribe((state, previous) => {
    if (state.sessionsById === previous.sessionsById) return;
    for (const id of runtimes.keys()) {
      if (!state.sessionsById[id]) forgetAgentTranscript(id);
    }
  });
}

async function loadTranscriptRuntime(sessionId: string, runtime: TranscriptRuntime): Promise<void> {
  try {
    // The newest window, not the whole conversation: an old session used to
    // ship months of rows on every open. Older pages arrive on demand as the
    // reader scrolls up (loadOlderTranscript).
    const page = await agentHubTranscriptPage(sessionId, 0, 0);
    if (runtimes.get(sessionId) !== runtime) return;
    const model = modelFromNewestPage(page, runtime.loadingUpdates, sessionId);
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
export function loadOlderTranscript(sessionId: string): void {
  const runtime = runtimes.get(sessionId);
  if (!runtime || runtime.loading || runtime.loadingOlder) return;
  const before = runtime.oldestSeq;
  if (before === null) return;
  const state = useAgentSessionsStore.getState();
  const paging = state.transcriptPagingById[sessionId] ?? NO_MORE_PAGES;
  if (!paging.hasMore) return;

  runtime.loadingOlder = true;
  // A reload meanwhile replaces the window this page would extend; landing it
  // on the new one leaves a gap or a duplicate at the seam.
  const generation = runtime.generation;
  const current = () => runtimes.get(sessionId) === runtime && runtime.generation === generation;
  state.setTranscriptPaging(sessionId, { ...paging, loadingOlder: true });
  void (async () => {
    try {
      const page = await agentHubTranscriptPage(sessionId, before, 0);
      if (!current()) return;
      // Land anything the buffer still holds first: the prepend merges from
      // the buffer's own model, and losing the pending frame would drop the
      // newest messages the reader just watched arrive.
      runtime.buffer?.flush();
      const merged = prependTranscriptEntries(runtime.model, page.entries, sessionId);
      runtime.model = merged;
      runtime.buffer = makeBuffer(sessionId, merged);
      runtime.oldestSeq = page.entries.length > 0 ? entrySeq(page.entries[0]) : null;
      // One publish: a render between the items and loadingOlder=false would
      // show the stream a prepend that still looks in flight, and it would
      // take the grown height as the baseline instead of compensating for it.
      useAgentSessionsStore.setState((state) => ({
        transcriptsById: { ...state.transcriptsById, [sessionId]: merged },
        transcriptPagingById: {
          ...state.transcriptPagingById,
          [sessionId]: { hasMore: page.hasMore, loadingOlder: false },
        },
      }));
    } catch {
      if (!current()) return;
      // The walk stops but the history is still up there; say so rather than
      // pretending the transcript is complete.
      useAgentSessionsStore
        .getState()
        .setTranscriptPaging(sessionId, { hasMore: true, loadingOlder: false });
    } finally {
      if (runtime.generation === generation) runtime.loadingOlder = false;
    }
  })();
}

export function ensureTranscript(sessionId: string): void {
  if (!sessionId) return;
  startAgentTranscriptStream();
  let runtime = runtimes.get(sessionId);
  if (runtime) {
    // Reopened, so most recent again: a Map iterates oldest insertion first.
    runtimes.delete(sessionId);
    runtimes.set(sessionId, runtime);
  } else {
    runtime = {
      model: createInitialTranscriptModel(),
      loading: true,
      loadingUpdates: [],
      buffer: null,
      loadPromise: null,
      oldestSeq: null,
      loadingOlder: false,
      generation: 0,
    };
    runtimes.set(sessionId, runtime);
    // Sessions not opened for a while give their transcript and older pages
    // back; reopening one loads its newest page again, as a first open does.
    for (const id of runtimes.keys()) {
      if (runtimes.size <= TRANSCRIPT_CACHE_LIMIT) break;
      forgetAgentTranscript(id);
    }
  }
  if (!runtime.loadPromise && runtime.loading) {
    useAgentSessionsStore.getState().setTranscriptLoading(sessionId, true);
    runtime.loadPromise = loadTranscriptRuntime(sessionId, runtime);
  }
}

export function reloadTranscript(sessionId: string): void {
  const runtime = runtimes.get(sessionId);
  if (!runtime || runtime.loading) return;
  runtime.loading = true;
  // An older page still in flight extends the window being replaced (loadOlderTranscript).
  runtime.generation += 1;
  runtime.loadingOlder = false;
  runtime.loadingUpdates = [];
  runtime.oldestSeq = null;
  runtime.buffer?.flush();
  runtime.buffer = null;
  useAgentSessionsStore.getState().setTranscriptLoading(sessionId, true);
  runtime.loadPromise = loadTranscriptRuntime(sessionId, runtime);
}

/**
 * Drops a session's transcript: its runtime and what the store holds for it.
 * Called when the session leaves the store (startAgentTranscriptStream) and
 * when it falls out of the cache (ensureTranscript).
 */
export function forgetAgentTranscript(sessionId: string): void {
  runtimes.get(sessionId)?.buffer?.dispose();
  runtimes.delete(sessionId);
  useAgentSessionsStore.setState((state) => {
    const { [sessionId]: _model, ...transcriptsById } = state.transcriptsById;
    const { [sessionId]: _loading, ...transcriptLoadingById } = state.transcriptLoadingById;
    const { [sessionId]: _error, ...transcriptErrorsById } = state.transcriptErrorsById;
    const { [sessionId]: _paging, ...transcriptPagingById } = state.transcriptPagingById;
    return { transcriptsById, transcriptLoadingById, transcriptErrorsById, transcriptPagingById };
  });
}

/** React is only a reader of the shell-owned model and never owns the stream. */
export function useAgentTranscript(sessionId: string, options: { enabled?: boolean } = {}) {
  const enabled = options.enabled ?? true;
  const modelSelector = useMemo(
    () => (state: AgentSessionsStoreState) => state.transcriptsById[sessionId] ?? EMPTY_TRANSCRIPT_MODEL,
    [sessionId],
  );
  const loadingSelector = useMemo(
    () => (state: AgentSessionsStoreState) => Boolean(sessionId) && Boolean(state.transcriptLoadingById[sessionId]),
    [sessionId],
  );
  const errorSelector = useMemo(
    () => (state: AgentSessionsStoreState) => state.transcriptErrorsById[sessionId] ?? null,
    [sessionId],
  );
  const pagingSelector = useMemo(
    () => (state: AgentSessionsStoreState) => state.transcriptPagingById[sessionId] ?? NO_MORE_PAGES,
    [sessionId],
  );
  const model = useTranscriptStoreValue(enabled, modelSelector);
  const loading = useTranscriptStoreValue(enabled, loadingSelector);
  const error = useTranscriptStoreValue(enabled, errorSelector);
  const paging = useTranscriptStoreValue(enabled, pagingSelector);

  useEffect(() => {
    if (!enabled) return;
    ensureTranscript(sessionId);
  }, [enabled, sessionId]);

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
