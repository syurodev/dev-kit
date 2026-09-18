import { Events } from "@wailsio/runtime";
import { callBridge } from "./client";
import { AppService } from "../../bindings/devkit/internal/bridge";
import { toAppError } from "./errors";
import type {
  AgentSession as RawSession,
  AgentType as RawAgentType,
  InvokeResult as RawInvokeResult,
  TranscriptEntry,
  ContextFile as RawContextFile,
  CheckpointSummary,
  FileDiff,
  RestorePreview,
  RestoreRequest,
  RestoreResult,
  RewindPreview,
  RewindRequest,
  RewindResult,
} from "../../bindings/devkit/internal/agenthub/models";
import type { SessionUpdate } from "../modules/agenthub/types";

export type AgentVendor = "claude";

/**
 * The Claude agent as the hub knows it — the `agentType` a session is created
 * with. Not CLAUDE_AGENT from bridge/agentRuntime: that is "claude-acp", the id
 * of the adapter DevKit installs, and a session created with it fails with
 * "Unknown agent vendor". Two ids for one agent, one per job.
 */
export const CLAUDE_VENDOR: AgentVendor = "claude";

export type SessionStatus =
  | "idle"
  | "running"
  | "awaiting_permission"
  | "error"
  | "closed"
  | "";

export type AgentSession = {
  id: string;
  agentType: AgentVendor | string;
  projectPath: string;
  status: SessionStatus;
  parentSessionId: string | null;
  depth: number;
  title: string | null;
  acpSessionId: string;
};

export type InvokeResult = {
  status: string;
  summary: string;
  filesChanged: string[];
  turnCount: number;
  stopReason: string;
};

export type CreateSessionInput = {
  agentType: AgentVendor | string;
  cwd: string;
  parentSessionId?: string;
  title?: string | null;
};

export type InvokeSessionInput = {
  sessionId: string;
  prompt: string;
  /** Absolute paths DevKit reads and sends with the prompt. */
  attachments?: string[];
  images?: AgentImageAttachment[];
  callerSessionId?: string;
};

export type AgentImageAttachment = {
  id: string;
  name: string;
  mimeType: string;
  data: string;
  previewUrl: string;
  size: number;
};

export type AgentContextFile = {
  path: string;
  name: string;
  description?: string;
  kind?: "file" | "folder" | "agent";
};

export type SessionUpdateEvent = {
  sessionId: string;
  /** The one payload copy: normalized, with `raw` kept only for kinds the
 * backend does not model (unknown kinds' whole content lives there). */
  parsed: SessionUpdate | null;
};

export type AgentTurnFinishedEvent = {
  sessionId: string;
  status: SessionStatus;
  outcome: "completed" | "cancelled" | "error" | "";
};

export type PendingPermission = {
  id: string;
  sessionId: string;
  params: unknown;
  caller: string;
  title: string;
  depth: number;
  /**
   * When the request arrived and when the gate closes on its own, both stamped
   * by the backend. The UI must render this deadline rather than start its own
   * clock on mount: a toast often mounts long after the request arrived — the
   * rail was closed, the user was in another module — and a fresh countdown
   * would promise time that no longer exists. ISO strings, empty when unknown.
   */
  createdAt: string;
  expiresAt: string;
};

/**
 * A question an agent is waiting for the user to answer (ACP
 * elicitation/create, form mode). AskUserQuestion, an MCP server's question
 * and the refusal-fallback prompt all arrive as this.
 *
 * No `expiresAt`, unlike PendingPermission: a question has no deadline — the
 * turn waits for as long as the user takes, and Skip or Stop is the way out.
 */
export type PendingElicitation = {
  id: string;
  sessionId: string;
  /** The tool call the agent emitted before asking; empty for an MCP server's question. */
  toolCallId: string;
  message: string;
  /** The request as the agent sent it, `requestedSchema` included. */
  params: unknown;
  caller: string;
  title: string;
  depth: number;
  createdAt: string;
};

export type ElicitationClosedEvent = {
  id: string;
  sessionId: string;
  reason: "answered" | "cancelled" | "turn_stopped" | string;
};

/** The three answers ACP has for a question; the hub keeps only what was asked. */
export type ElicitationResponse =
  | { action: "accept"; content: Record<string, string | number | boolean | string[]> }
  | { action: "decline" }
  | { action: "cancel" };

export type PermissionOption = {
  optionId: string;
  name: string;
  kind: string;
};

const SESSION_UPDATE_EVENT = "agenthub:sessionUpdate";
const PERMISSION_EVENT = "agenthub:permission";
const ELICITATION_EVENT = "agenthub:elicitation";
const ELICITATION_CLOSED_EVENT = "agenthub:elicitationClosed";
const TURN_FINISHED_EVENT = "agenthub:turnFinished";
const NOTIFICATION_ACTION_EVENT = "agenthub:notificationAction";
const CHECKPOINT_EVENT = "agenthub:checkpoint";
const FILES_RESTORED_EVENT = "agenthub:filesRestored";
const SESSION_REWOUND_EVENT = "agenthub:sessionRewound";

function asVendor(value: string): AgentVendor | string {
  return value;
}

function asStatus(value: string): SessionStatus {
  switch (value) {
    case "idle":
    case "running":
    case "awaiting_permission":
    case "error":
    case "closed":
      return value;
    default:
      return "";
  }
}

function toSession(raw: RawSession): AgentSession {
  return {
    id: raw.ID,
    agentType: asVendor(String(raw.AgentType ?? "")),
    projectPath: raw.ProjectPath ?? "",
    status: asStatus(String(raw.Status ?? "")),
    parentSessionId: raw.ParentSessionID ?? null,
    depth: raw.Depth ?? 0,
    title: raw.Title ?? null,
    acpSessionId: raw.ACPSessionID ?? "",
  };
}

function toInvokeResult(raw: RawInvokeResult): InvokeResult {
  return {
    status: raw.status ?? "",
    summary: raw.summary ?? "",
    filesChanged: raw.filesChanged ?? [],
    turnCount: raw.turnCount ?? 0,
    stopReason: raw.stopReason ?? "",
  };
}

export function agentHubListAgents(): Promise<AgentSession[]> {
  return callBridge(async () => {
    const list = await AppService.AgentHubListAgents();
    return (list ?? []).map(toSession);
  });
}

export function agentHubListVendorTypes(): Promise<string[]> {
  return callBridge(async () => {
    const list = await AppService.AgentHubListVendorTypes();
    return (list ?? []).map((t: RawAgentType) => String(t));
  });
}

export function agentHubSetMode(sessionId: string, modeId: string): Promise<void> {
  return callBridge(() => AppService.AgentHubSetMode(sessionId, modeId) as Promise<void>);
}

export function agentHubSetConfigOption(
  sessionId: string,
  optionId: string,
  value: string,
): Promise<void> {
  return callBridge(() => AppService.AgentHubSetConfigOption(sessionId, optionId, value) as Promise<void>);
}

export function agentHubListContextFiles(
  sessionId: string,
  query = "",
): Promise<AgentContextFile[]> {
  return callBridge(async () => {
    const files = await AppService.AgentHubListContextFiles(sessionId, query);
    return (files ?? []).map((file: RawContextFile) => ({
      path: file.path,
      name: file.name,
      description: file.description ?? undefined,
      kind: (file.kind as AgentContextFile["kind"]) ?? undefined,
    }));
  });
}

export function agentHubCreateSession(
  input: CreateSessionInput,
): Promise<AgentSession> {
  return callBridge(async () => {
    const created = await AppService.AgentHubCreateSession({
      AgentType: input.agentType as RawAgentType,
      Cwd: input.cwd,
      ParentSessionID: input.parentSessionId ?? "",
      Title: input.title ?? null,
    });
    return toSession(created);
  });
}

export function agentHubGetStatus(sessionId: string): Promise<AgentSession> {
  return callBridge(async () => {
    const status = await AppService.AgentHubGetStatus(sessionId);
    return toSession(status);
  });
}

export function agentHubInvoke(
  input: InvokeSessionInput,
): Promise<InvokeResult> {
  return callBridge(async () => {
    const result = await AppService.AgentHubInvoke({
      SessionID: input.sessionId,
      Prompt: input.prompt,
      Attachments: input.attachments ?? [],
      Images: (input.images ?? []).map(({ name, mimeType, data }) => ({ name, mimeType, data })),
      CallerSessionID: input.callerSessionId ?? "",
    });
    return toInvokeResult(result);
  });
}

export function agentHubCancel(sessionId: string): Promise<void> {
  return callBridge(
    () => AppService.AgentHubCancel(sessionId) as Promise<void>,
  );
}

/**
 * Opens the native folder picker. Resolves to "" when the user cancelled —
 * cancelling is not a failure, and the dialog that called this should stay
 * open with the field untouched.
 */
export function agentHubChooseFiles(startIn = ""): Promise<string[]> {
  return callBridge(async () =>
    (await AppService.AgentHubChooseFiles(startIn)) ?? [],
  );
}

export function agentHubChooseDirectory(startIn = ""): Promise<string> {
  return callBridge(
    () => AppService.AgentHubChooseDirectory(startIn) as Promise<string>,
  );
}

/** What a pending permission would do to one file; see AgentHubPermissionFileDiff. */
export type PermissionFileDiff = {
  path: string;
  before: string;
  after: string;
  binary: boolean;
  /** No file there yet: the edit creates it. */
  beforeMissing: boolean;
  truncated: boolean;
  /** The snippet the agent replaces is no longer in the file. */
  stale: boolean;
};

/**
 * Previews each file a pending permission edits, read from disk: the file as
 * it is and as it would be. Empty for a request that edits no file.
 */
export function agentHubPermissionFileDiff(id: string): Promise<PermissionFileDiff[]> {
  return callBridge(async () => {
    const raw = ((await AppService.AgentHubPermissionFileDiff(id)) ?? []) as unknown as Array<Record<string, unknown>>;
    return raw.map((d) => ({
      path: String(d.path ?? ""),
      before: String(d.before ?? ""),
      after: String(d.after ?? ""),
      binary: Boolean(d.binary),
      beforeMissing: Boolean(d.beforeMissing),
      truncated: Boolean(d.truncated),
      stale: Boolean(d.stale),
    }));
  });
}

/**
 * The file edits a request_permission carries, as the agent sent them: ACP
 * diff content items. Shown at once, before the whole-file preview arrives.
 */
export function permissionFileEdits(params: unknown): PermissionFileDiff[] {
  const toolCall = (params as { toolCall?: { content?: unknown } } | null)?.toolCall;
  const content = Array.isArray(toolCall?.content) ? toolCall.content : [];
  return content.flatMap((item) => {
    const d = item as { type?: unknown; path?: unknown; oldText?: unknown; newText?: unknown };
    if (d.type !== "diff" || typeof d.path !== "string" || !d.path) return [];
    const oldText = typeof d.oldText === "string" ? d.oldText : null;
    return [{
      path: d.path,
      before: oldText ?? "",
      after: typeof d.newText === "string" ? d.newText : "",
      binary: false,
      // A Write's null oldText cannot tell new from overwrite; the disk read can.
      beforeMissing: false,
      truncated: false,
      stale: false,
    }];
  });
}

/**
 * An empty folder for one session started with no project. The hub needs a
 * working directory and refuses / and the home folder, so "no project" is a
 * scratch folder of its own under DevKit's config dir.
 */
export function agentHubNewScratchDirectory(): Promise<string> {
  return callBridge(() => AppService.AgentHubNewScratchDirectory() as Promise<string>);
}

/** Where the scratch folders live; the sidebar groups sessions under it as "No project". */
export function agentHubScratchRoot(): Promise<string> {
  return callBridge(() => AppService.AgentHubScratchRoot() as Promise<string>);
}

/**
 * Puts a message into the turn the session is running, instead of queueing it
 * behind that turn — what the vendors' own apps do when you type while the
 * agent works. Resolves to the outcome: "injected", or "promptRequired" when
 * nothing was running and the caller should send it as an ordinary prompt.
 */
export function agentHubSteer(sessionId: string, text: string): Promise<string> {
  return callBridge(() => AppService.AgentHubSteer(sessionId, text) as Promise<string>);
}

/**
 * Sets what the session is working towards, or clears it when the objective is
 * empty. The goal comes back on a session_info_update, so nothing is assumed
 * here about whether it took.
 */
export function agentHubSetGoal(sessionId: string, objective: string): Promise<void> {
  return callBridge(() => AppService.AgentHubSetGoal(sessionId, objective) as Promise<void>);
}

/**
 * Ends the session: the agent process is shut down and its MCP endpoint
 * released. Not the same as agentHubCancel, which stops the turn in flight and
 * leaves the agent running. Not undoable — the session stays as history.
 */
export function agentHubCloseSession(sessionId: string): Promise<void> {
  return callBridge(
    () => AppService.AgentHubCloseSession(sessionId) as Promise<void>,
  );
}

export function agentHubRenameSession(sessionId: string, title: string): Promise<void> {
  return callBridge(() => AppService.AgentHubRenameSession(sessionId, title) as Promise<void>);
}

/**
 * Permanently removes a session, its transcript and every descendant. Unlike
 * agentHubCloseSession, which keeps the row as history, this leaves nothing.
 */
export function agentHubDeleteSession(sessionId: string): Promise<void> {
  return callBridge(
    () => AppService.AgentHubDeleteSession(sessionId) as Promise<void>,
  );
}

export type TranscriptPage = {
  entries: TranscriptEntry[];
  hasMore: boolean;
  /** Session-level rows (mode, config options, commands, usage) from above
   * the newest page's window; empty on older pages. */
  state: TranscriptEntry[];
};

/**
 * The newest window of a session's transcript, oldest entry first. Open on
 * this and walk backwards with the oldest seq of the page held — the
 * unbounded listing this replaces made a session with months of history pay
 * for all of it on every open. limit 0 asks the backend for its default.
 */
export function agentHubTranscriptPage(
  sessionId: string,
  beforeSeq = 0,
  limit = 0,
): Promise<TranscriptPage> {
  return callBridge(async () => {
    const page = await AppService.AgentHubTranscriptPage(sessionId, beforeSeq, limit);
    return {
      entries: page?.entries ?? [],
      hasMore: Boolean(page?.hasMore),
      state: page?.state ?? [],
    };
  });
}

/**
 * One stored tool call, payload intact — the "show the whole output" behind
 * the page's truncated previews.
 */
export function agentHubToolCallDetail(
  sessionId: string,
  toolCallId: string,
): Promise<TranscriptEntry> {
  return callBridge(() => AppService.AgentHubToolCallDetail(sessionId, toolCallId));
}

export function agentHubResolvePermission(
  id: string,
  outcome: unknown,
): Promise<void> {
  return callBridge(
    () => AppService.AgentHubResolvePermission(id, outcome) as Promise<void>,
  );
}

export function selectedPermissionOutcome(optionId: string): unknown {
  return { outcome: { outcome: "selected", optionId } };
}

export function cancelledPermissionOutcome(): unknown {
  return { outcome: { outcome: "cancelled" } };
}

export function agentHubRespondElicitation(id: string, response: ElicitationResponse): Promise<void> {
  return callBridge(() => AppService.AgentHubRespondElicitation(id, response) as Promise<void>);
}

/** Submit: only the keys that were answered. */
export function acceptElicitation(content: Record<string, string | number | boolean | string[]>): ElicitationResponse {
  return { action: "accept", content };
}

/** Skip the whole question: the tool runs on with no answers and the turn continues. */
export function declineElicitation(): ElicitationResponse {
  return { action: "decline" };
}

/** Abandon it: the tool call that asked is aborted. */
export function cancelElicitation(): ElicitationResponse {
  return { action: "cancel" };
}

export function isSandboxUnavailableError(error: unknown): boolean {
  const app = toAppError(error);
  const msg = app.message.toLowerCase();
  return msg.includes("sandbox") || app.message.includes("sandboxUnavailable");
}

export function sandboxUnavailableMessage(): string {
  return "Agent sandbox is unavailable. On Linux install bubblewrap (bwrap); on macOS Seatbelt/sandbox-exec is required.";
}

function parseSessionUpdate(data: unknown): SessionUpdateEvent | null {
  if (!data || typeof data !== "object") {
    return null;
  }
  const record = data as Record<string, unknown>;
  const sessionId =
    typeof record.sessionId === "string"
      ? record.sessionId
      : typeof record.SessionID === "string"
        ? record.SessionID
        : "";
  if (!sessionId) {
    return null;
  }
  return {
    sessionId,
    parsed: (record.parsed ?? record.Parsed ?? null) as SessionUpdate | null,
  };
}

function parsePermission(data: unknown): PendingPermission | null {
  if (!data || typeof data !== "object") {
    return null;
  }
  const record = data as Record<string, unknown>;
  const id = typeof record.id === "string" ? record.id : "";
  const sessionId =
    typeof record.sessionId === "string"
      ? record.sessionId
      : typeof record.SessionID === "string"
        ? record.SessionID
        : "";
  if (!id || !sessionId) {
    return null;
  }
  return {
    id,
    sessionId,
    params: record.params ?? record.Params ?? null,
    caller: typeof record.caller === "string" ? record.caller : `agent:${sessionId}`,
    title: typeof record.title === "string" ? record.title : "",
    depth: typeof record.depth === "number" ? record.depth : 0,
    createdAt: isoString(record.createdAt ?? record.CreatedAt),
    expiresAt: isoString(record.expiresAt ?? record.ExpiresAt),
  };
}

export function parseElicitation(data: unknown): PendingElicitation | null {
  if (!data || typeof data !== "object") {
    return null;
  }
  const record = data as Record<string, unknown>;
  const id = typeof record.id === "string" ? record.id : typeof record.ID === "string" ? record.ID : "";
  const sessionId =
    typeof record.sessionId === "string"
      ? record.sessionId
      : typeof record.SessionID === "string"
        ? record.SessionID
        : "";
  if (!id || !sessionId) {
    return null;
  }
  const params = record.params ?? record.Params ?? null;
  const paramsRecord = params && typeof params === "object" ? (params as Record<string, unknown>) : {};
  const message = record.message ?? record.Message ?? paramsRecord.message;
  const toolCallId = record.toolCallId ?? record.ToolCallID ?? paramsRecord.toolCallId;
  return {
    id,
    sessionId,
    toolCallId: typeof toolCallId === "string" ? toolCallId : "",
    message: typeof message === "string" ? message : "",
    params,
    caller: typeof record.caller === "string" ? record.caller : `agent:${sessionId}`,
    title: typeof record.title === "string" ? record.title : "",
    depth: typeof record.depth === "number" ? record.depth : 0,
    createdAt: isoString(record.createdAt ?? record.CreatedAt),
  };
}

export function agentHubPendingElicitations(): Promise<PendingElicitation[]> {
  return callBridge(async () => {
    const list = (await AppService.AgentHubPendingElicitations()) ?? [];
    return list.map((raw) => parseElicitation(raw)).filter((e): e is PendingElicitation => e !== null);
  });
}

export function onAgentHubElicitation(handler: (pending: PendingElicitation) => void): () => void {
  return Events.On(ELICITATION_EVENT, (ev) => {
    const parsed = parseElicitation(Array.isArray(ev?.data) ? ev.data[0] : ev?.data);
    if (parsed) handler(parsed);
  });
}

/** Answered (here or in another window), taken back by the agent, or its turn was stopped. */
export function onAgentHubElicitationClosed(handler: (event: ElicitationClosedEvent) => void): () => void {
  return Events.On(ELICITATION_CLOSED_EVENT, (ev) => {
    const data = Array.isArray(ev?.data) ? ev.data[0] : ev?.data;
    if (!data || typeof data !== "object") return;
    const record = data as Record<string, unknown>;
    if (typeof record.id !== "string" || !record.id) return;
    handler({
      id: record.id,
      sessionId: typeof record.sessionId === "string" ? record.sessionId : "",
      reason: typeof record.reason === "string" ? record.reason : "",
    });
  });
}

function isoString(value: unknown): string {
  if (typeof value !== "string") return "";
  // Go's zero time, which is how a field with no value can still arrive when
  // it is not marshalled with omitzero. Empty is what "none" means here.
  return value.startsWith("0001-01-01") ? "" : value;
}

export function agentHubPendingPermissions(): Promise<PendingPermission[]> {
  return callBridge(async () => {
    const list = (await AppService.AgentHubPendingPermissions()) ?? [];
    return list
      .map((raw) =>
        parsePermission({
          id: (raw as any).ID ?? raw.id,
          sessionId: (raw as any).SessionID ?? raw.sessionId,
          params: (raw as any).Params ?? raw.params,
          createdAt: (raw as any).CreatedAt ?? (raw as any).createdAt,
          expiresAt: (raw as any).ExpiresAt ?? (raw as any).expiresAt,
        }),
      )
      .filter((p): p is PendingPermission => p !== null);
  });
}

export function onAgentHubSessionUpdate(
  handler: (event: SessionUpdateEvent) => void,
): () => void {
  return Events.On(SESSION_UPDATE_EVENT, (ev) => {
    const parsed = parseSessionUpdate(ev?.data);
    if (parsed) {
      handler(parsed);
    }
  });
}

export function onAgentHubPermission(
  handler: (pending: PendingPermission) => void,
): () => void {
  return Events.On(PERMISSION_EVENT, (ev) => {
    const parsed = parsePermission(ev?.data);
    if (parsed) {
      handler(parsed);
    }
  });
}

function parseSessionEvent(data: unknown): { sessionId: string; status?: SessionStatus; outcome?: AgentTurnFinishedEvent["outcome"] } | null {
  const record = Array.isArray(data) ? data[0] : data;
  if (!record || typeof record !== "object") return null;
  const value = record as Record<string, unknown>;
  if (typeof value.sessionId !== "string" || value.sessionId.length === 0) return null;
  return {
    sessionId: value.sessionId,
    status: typeof value.status === "string" ? asStatus(value.status) : undefined,
    outcome: typeof value.outcome === "string" ? value.outcome as AgentTurnFinishedEvent["outcome"] : undefined,
  };
}

export function onAgentHubTurnFinished(
  handler: (event: AgentTurnFinishedEvent) => void,
): () => void {
  return Events.On(TURN_FINISHED_EVENT, (ev) => {
    const parsed = parseSessionEvent(ev?.data);
    if (parsed) handler({ sessionId: parsed.sessionId, status: parsed.status ?? "idle", outcome: parsed.outcome ?? "" });
  });
}

/**
 * A turn's file checkpoint, once it is settled.
 *
 * It arrives after the turn has finished — the diff and the row are written off
 * the turn's path — and again if the agent's own report of what it changed
 * lands late, which re-attributes files the first pass could not account for.
 */
export function onAgentHubCheckpoint(
  handler: (sessionId: string, checkpoint: CheckpointSummary) => void,
): () => void {
  return Events.On(CHECKPOINT_EVENT, (ev) => {
    const record = Array.isArray(ev?.data) ? ev.data[0] : ev?.data;
    if (!record || typeof record !== "object") return;
    const value = record as Record<string, unknown>;
    const sessionId = typeof value.sessionId === "string" ? value.sessionId : "";
    const checkpoint = value.checkpoint;
    if (!sessionId || !checkpoint || typeof checkpoint !== "object") return;
    handler(sessionId, checkpoint as CheckpointSummary);
  });
}

export function agentHubListCheckpoints(sessionId: string): Promise<CheckpointSummary[]> {
  return callBridge(async () => {
    const records = await AppService.AgentHubListCheckpoints(sessionId);
    return (records ?? []) as CheckpointSummary[];
  });
}

/**
 * One changed file's two versions. The path is checked against the
 * checkpoint's own list on the backend, so this cannot read an arbitrary file
 * out of a project.
 */
export function agentHubCheckpointFileDiff(checkpointId: string, path: string): Promise<FileDiff> {
  return callBridge(async () => {
    const diff = await AppService.AgentHubCheckpointFileDiff(checkpointId, path);
    return diff as FileDiff;
  });
}

/**
 * Works out what putting a turn's files back would do, without touching
 * anything. The token it returns is the only handle on the plan — the plan
 * itself stays on the backend, so nothing a client says can name a tree.
 */
export function agentHubPlanRestore(request: RestoreRequest): Promise<RestorePreview> {
  return callBridge(async () => {
    const preview = await AppService.AgentHubPlanRestore(request);
    return preview as RestorePreview;
  });
}

/** Carries out a plan. `overwrite` names the conflicts the user chose to force. */
export function agentHubApplyRestore(token: string, overwrite: string[]): Promise<RestoreResult> {
  return callBridge(async () => {
    const result = await AppService.AgentHubApplyRestore(token, overwrite);
    return result as RestoreResult;
  });
}

/** Files on disk changed under whatever else is showing them. */
export function onAgentHubFilesRestored(
  handler: (sessionId: string, result: { written: string[]; deleted: string[]; safetyCheckpointId: string }) => void,
): () => void {
  return Events.On(FILES_RESTORED_EVENT, (ev) => {
    const record = Array.isArray(ev?.data) ? ev.data[0] : ev?.data;
    if (!record || typeof record !== "object") return;
    const value = record as Record<string, unknown>;
    const sessionId = typeof value.sessionId === "string" ? value.sessionId : "";
    if (!sessionId) return;
    handler(sessionId, {
      written: Array.isArray(value.written) ? (value.written as string[]) : [],
      deleted: Array.isArray(value.deleted) ? (value.deleted as string[]) : [],
      safetyCheckpointId: typeof value.safetyCheckpointId === "string" ? value.safetyCheckpointId : "",
    });
  });
}

/**
 * Works out what taking a session back to a message would do — to the
 * conversation, the files, or both — without touching anything.
 */
export function agentHubPlanRewind(request: RewindRequest): Promise<RewindPreview> {
  return callBridge(async () => {
    const preview = await AppService.AgentHubPlanRewind(request);
    return preview as RewindPreview;
  });
}

export function agentHubApplyRewind(token: string, overwrite: string[]): Promise<RewindResult> {
  return callBridge(async () => {
    const result = await AppService.AgentHubApplyRewind(token, overwrite);
    return result as RewindResult;
  });
}

/** The rewinds that never finished, so a session can offer to put its files back. */
export function agentHubInterruptedRewinds(): Promise<CheckpointSummary[]> {
  return callBridge(async () => {
    const records = await AppService.AgentHubInterruptedRewinds();
    return (records ?? []) as CheckpointSummary[];
  });
}

export function agentHubDismissInterruptedRewind(checkpointId: string): Promise<void> {
  return callBridge(() => AppService.AgentHubDismissInterruptedRewind(checkpointId) as Promise<void>);
}

/**
 * A session's conversation was cut. The transcript on screen now holds messages
 * the session no longer has, which no other event would explain — so this is a
 * reload, not a patch.
 */
export function onAgentHubSessionRewound(
  handler: (sessionId: string, promptText: string) => void,
): () => void {
  return Events.On(SESSION_REWOUND_EVENT, (ev) => {
    const record = Array.isArray(ev?.data) ? ev.data[0] : ev?.data;
    if (!record || typeof record !== "object") return;
    const value = record as Record<string, unknown>;
    const sessionId = typeof value.sessionId === "string" ? value.sessionId : "";
    if (!sessionId) return;
    handler(sessionId, typeof value.promptText === "string" ? value.promptText : "");
  });
}

export function onAgentHubNotificationAction(
  handler: (sessionId: string) => void,
): () => void {
  return Events.On(NOTIFICATION_ACTION_EVENT, (ev) => {
    const parsed = parseSessionEvent(ev?.data);
    if (parsed) handler(parsed.sessionId);
  });
}

export function parsePermissionOptions(params: unknown): PermissionOption[] {
  if (!params || typeof params !== "object") {
    return [];
  }
  const record = params as Record<string, unknown>;
  const options = record.options;
  if (!Array.isArray(options)) {
    return [];
  }
  const out: PermissionOption[] = [];
  for (const item of options) {
    if (!item || typeof item !== "object") {
      continue;
    }
    const opt = item as Record<string, unknown>;
    const optionId =
      typeof opt.optionId === "string"
        ? opt.optionId
        : typeof opt.OptionId === "string"
          ? opt.OptionId
          : "";
    if (!optionId) {
      continue;
    }
    out.push({
      optionId,
      name: typeof opt.name === "string" ? opt.name : optionId,
      kind: typeof opt.kind === "string" ? opt.kind : "",
    });
  }
  return out;
}

export function permissionToolTitle(params: unknown): string {
  if (!params || typeof params !== "object") {
    return "Agent permission request";
  }
  const record = params as Record<string, unknown>;
  const toolCall = record.toolCall;
  if (toolCall && typeof toolCall === "object") {
    const tc = toolCall as Record<string, unknown>;
    if (typeof tc.title === "string" && tc.title.trim()) {
      return tc.title;
    }
  }
  return "Agent permission request";
}

/**
 * The literal command a permission request would run, when it has one. ACP
 * carries it on `toolCall.rawInput.command` (claude-agent-acp's Bash); a flat
 * `command` is kept for callers that already build requests that way.
 */
export function permissionCommand(params: unknown): string | undefined {
  if (!params || typeof params !== "object") return undefined;
  const record = params as Record<string, unknown>;
  if (typeof record.command === "string" && record.command.trim()) return record.command;
  const toolCall = record.toolCall;
  if (!toolCall || typeof toolCall !== "object") return undefined;
  const rawInput = (toolCall as Record<string, unknown>).rawInput;
  if (!rawInput || typeof rawInput !== "object") return undefined;
  const command = (rawInput as Record<string, unknown>).command;
  return typeof command === "string" && command.trim() ? command : undefined;
}
