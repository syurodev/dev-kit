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
import type { ConfigOption } from "../modules/agenthub/types";

export type AgentVendor = "claude" | "codex" | "opencode";

/**
 * The Claude agent as the hub knows it — the `agentType` a session is created
 * with. Not CLAUDE_AGENT from bridge/agentRuntime: that is "claude-acp", the id
 * of the adapter DevKit installs, and a session created with it fails with
 * "Unknown agent vendor". Two ids for one agent, one per job.
 */
export const CLAUDE_VENDOR: AgentVendor = "claude";

/**
 * Codex as the hub knows it. Its runtime id (CODEX_AGENT) happens to be the
 * same string; the vendor table (modules/agenthub/vendors) keeps the two jobs
 * apart anyway, so nothing depends on that.
 */
export const CODEX_VENDOR: AgentVendor = "codex";

/**
 * opencode as the hub knows it. Its runtime id (OPENCODE_AGENT) is the same
 * string, kept apart in the vendor table like Codex's.
 */
export const OPENCODE_VENDOR: AgentVendor = "opencode";

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
  workspaceId?: string;
  status: SessionStatus;
  parentSessionId: string | null;
  depth: number;
  title: string | null;
  acpSessionId: string;
  /** Where the session got to — written when a turn ends. Empty until one has. */
  preview?: string;
  /**
   * When the backend last wrote the session row, in epoch milliseconds: a
   * status change (a turn starting or ending, a permission wait), a rename or
   * the vendor id. Not touched by transcript chunks, so ordering by it does
   * not move while an agent streams. Absent on a row built locally.
   */
  updatedAt?: number;
  /**
   * The vendor account the session runs as. "" is the built-in one (the machine's
   * own Claude login, DevKit's own Codex home); after a move back it can also be
   * the "<vendor>-default" id. The bound JSON calls this field AccountID.
   */
  accountId?: string;
  /**
   * What the session's live process is doing that no DevKit turn started: a turn
   * the vendor began itself, and background tasks. Absent when it does neither.
   * Read from the row and kept current by `agenthub:activity`; never sent back.
   */
  activity?: SessionActivity;
};

/** The vendor's own work on a session (internal/agenthub/bgwork.go). */
export type SessionActivity = {
  /** The vendor runs a turn of its own (taskType "turn"). */
  agentTurn: boolean;
  /** Running background tasks other than that turn. */
  tasks: number;
  /**
   * Orders two reads or events of one session: the higher is newer (Go counts every change and
   * keeps counting across a process going away). A read that was in flight when an event landed
   * has the lower one. Absent: 0.
   */
  rev?: number;
  /**
   * With no agent turn: true when it ended by the vendor's own update, false when the process
   * went away (closed, reloaded, cancelled, crashed) so the turn did not finish. Absent: unknown,
   * read as ended.
   */
  ended?: boolean;
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
  workspaceId?: string;
  parentSessionId?: string;
  title?: string | null;
  /**
   * Extra services granted read-write alongside cwd — the rest of a
   * workspace (docs/dev-kit/workspace.mdx) when the draft's picker chose the
   * whole thing rather than one service. Only the UI path sets this; there
   * is no way to reach it over MCP (internal/mcp/tools_agenthub.go has no
   * such field), so an agent-created session can never widen its own jail.
   */
  extraReadWritePaths?: string[];
  initialConfig?: Record<string, string>;
};

export type InvokeSessionInput = {
  sessionId: string;
  prompt: string;
  /** Absolute paths DevKit reads and sends with the prompt. */
  attachments?: string[];
  images?: AgentImageAttachment[];
  /**
   * devkit:// EntityRef URIs (workbench/entityRef.ts) — cross-module
   * references such as a Code Graph endpoint. DevKit never resolves these
   * itself; the agent reads each one fresh through its owning module's MCP
   * tool (docs/dev-kit/cross-module.mdx § Primitive 1).
   */
  refs?: string[];
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
  scopedApprovalEligible?: boolean;
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

export type ScopedApproval = { id: string; caller: string; tool: string; profileId: string; env: string; fingerprint: string; createdAt: string; expiresAt: string; remaining: number };

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

export type PermissionClosedEvent = {
  id: string;
  sessionId: string;
  reason: "answered" | "withdrawn" | "turn_ended" | "expired" | string;
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
const PERMISSION_CLOSED_EVENT = "agenthub:permissionClosed";
const ELICITATION_CLOSED_EVENT = "agenthub:elicitationClosed";
const TURN_FINISHED_EVENT = "agenthub:turnFinished";
const TURN_STARTED_EVENT = "agenthub:turnStarted";
const SESSION_CREATED_EVENT = "agenthub:sessionCreated";
const ACTIVITY_EVENT = "agenthub:activity";
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

export function toSession(raw: RawSession): AgentSession {
  return {
    id: raw.ID,
    agentType: asVendor(String(raw.AgentType ?? "")),
    projectPath: raw.ProjectPath ?? "",
    workspaceId: raw.WorkspaceID ?? "",
    status: asStatus(String(raw.Status ?? "")),
    parentSessionId: raw.ParentSessionID ?? null,
    depth: raw.Depth ?? 0,
    title: raw.Title ?? null,
    acpSessionId: raw.ACPSessionID ?? "",
    preview: raw.Preview ?? "",
    updatedAt: toEpoch(raw.UpdatedAt),
    accountId: raw.AccountID ?? "",
    ...activityField(raw.activity),
  };
}

/** The activity of a row as a spread: nothing when the process does nothing of its own. */
function activityField(
  raw: { agentTurn?: boolean; tasks?: number; rev?: number; ended?: boolean } | null | undefined,
): { activity?: SessionActivity } {
  const agentTurn = raw?.agentTurn === true;
  const tasks = typeof raw?.tasks === "number" && raw.tasks > 0 ? Math.floor(raw.tasks) : 0;
  const rev = typeof raw?.rev === "number" && raw.rev > 0 ? Math.floor(raw.rev) : 0;
  // A row that once had activity keeps its revision with nothing running: it orders the read.
  if (!agentTurn && tasks === 0 && rev === 0) return {};
  return { activity: { agentTurn, tasks, ...(rev > 0 ? { rev } : {}), ...(raw?.ended === true ? { ended: true } : {}) } };
}

/**
 * `agenthub:activity`'s payload, as a session activity: the whole state, zero when nothing runs.
 * `ended` is explicit: only an event that says the turn did not finish (false) is read as such.
 */
export function toActivity(
  raw: { agentTurn?: unknown; tasks?: unknown; rev?: unknown; ended?: unknown } | null | undefined,
): SessionActivity {
  const rev = typeof raw?.rev === "number" && raw.rev > 0 ? Math.floor(raw.rev) : 0;
  return {
    agentTurn: raw?.agentTurn === true,
    tasks: typeof raw?.tasks === "number" && raw.tasks > 0 ? Math.floor(raw.tasks) : 0,
    ...(rev > 0 ? { rev } : {}),
    ...(typeof raw?.ended === "boolean" ? { ended: raw.ended } : {}),
  };
}

/** A Go time.Time over the bridge; the zero time and anything unparsable are "unknown". */
function toEpoch(value: unknown): number | undefined {
  if (typeof value !== "string") return undefined;
  const at = Date.parse(value);
  // Go's zero time is year 1, which parses to a large negative number.
  return Number.isFinite(at) && at > 0 ? at : undefined;
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

function contextFileFromRaw(file: RawContextFile): AgentContextFile {
  return {
    path: file.path,
    name: file.name,
    description: file.description ?? undefined,
    kind: (file.kind as AgentContextFile["kind"]) ?? undefined,
  };
}

/** What "@" can mention in a session: its folder and its workspace's others, filtered by query. */
export function agentHubListContextFiles(
  sessionId: string,
  query = "",
): Promise<AgentContextFile[]> {
  return callBridge(async () => {
    const files = await AppService.AgentHubListContextFiles(sessionId, query);
    return (files ?? []).map(contextFileFromRaw);
  });
}

/** agentHubListContextFiles for a draft: the folders it will work in, its own first. */
export function agentHubListDraftContextFiles(
  roots: string[],
  query = "",
): Promise<AgentContextFile[]> {
  return callBridge(async () => {
    const files = await AppService.AgentHubListDraftContextFiles(roots, query);
    return (files ?? []).map(contextFileFromRaw);
  });
}

export function agentHubCreateSession(
  input: CreateSessionInput,
): Promise<AgentSession> {
  return callBridge(async () => {
    const created = await AppService.AgentHubCreateSession({
      AgentType: input.agentType as RawAgentType,
      Cwd: input.cwd,
      WorkspaceID: input.workspaceId ?? "",
      ParentSessionID: input.parentSessionId ?? "",
      Title: input.title ?? null,
      ExtraReadWritePaths: input.extraReadWritePaths ?? [],
      InitialConfig: input.initialConfig ?? {},
      // Only an agent choosing a child's model uses these (agenthub.createAgentSession).
      Profile: "",
      ModelChoiceApproved: false,
    });
    return toSession(created);
  });
}

export function agentHubConfigCatalog(agent: string): Promise<ConfigOption[]> {
  return callBridge(async () => (await AppService.AgentHubConfigCatalog(agent) ?? []) as ConfigOption[]);
}

export type EnvironmentEntry = {
  name: string;
  kind: string;
  status: string;
  source?: string;
  detail: string;
  action?: string;
};

export type EnvironmentReport = {
  agent: string;
  scope: "session" | "next";
  entries: EnvironmentEntry[];
  checkedAt: number;
};

export function agentHubEnvironment(agent: string, sessionId = ""): Promise<EnvironmentReport> {
  return callBridge(async () => (await AppService.AgentHubEnvironment(agent, sessionId)) as EnvironmentReport);
}

export type AgentMCPConfig = { name: string; command?: string; args?: string[]; url?: string; envKeys?: string[]; enabled: boolean };
export type AgentCustomization = { revision: number; skillFolders: string[]; disabledSkills: string[]; disabledMCP: string[]; servers: AgentMCPConfig[] };
export function agentHubGetCustomization(vendor: string): Promise<AgentCustomization> {
  return callBridge(async () => (await AppService.AgentHubGetCustomization(vendor)) as AgentCustomization);
}
export function agentHubSetCustomization(vendor: string, value: AgentCustomization): Promise<AgentCustomization> {
  return callBridge(async () => (await AppService.AgentHubSetCustomization(vendor, value)) as AgentCustomization);
}
export function agentHubResumeSession(sessionId: string, reload = false): Promise<void> {
  return callBridge(() => AppService.AgentHubResumeSession(sessionId, reload));
}

export function agentHubPrepareSession(sessionId: string): Promise<void> {
  return callBridge(() => AppService.AgentHubPrepareSession(sessionId));
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
      Refs: input.refs ?? [],
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

/** What the composer's chip says about a file: how big, what DevKit does with it, and why it cannot go. */
export type AgentAttachmentInfo = {
  path: string;
  name: string;
  size: number;
  /** "text" | "image" | "pdf" | "word" | "excel" | "other" */
  kind: string;
  /** Empty when DevKit can send it. */
  problem: string;
};

export function agentHubDescribeFiles(paths: string[]): Promise<AgentAttachmentInfo[]> {
  return callBridge(async () =>
    ((await AppService.AgentHubDescribeFiles(paths)) ?? []).map((info) => ({
      path: info.path,
      name: info.name,
      size: info.size,
      kind: info.kind,
      problem: info.problem ?? "",
    })),
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
 * Tells Go how many of the person's own prompts this window holds for a session
 * and whether they paused them, so a message from another session waits behind
 * them instead of starting work the person put on hold.
 */
export function agentHubSetPromptQueue(sessionId: string, waiting: number, paused: boolean): Promise<void> {
  return callBridge(() => AppService.AgentHubSetPromptQueue(sessionId, waiting, paused) as Promise<void>);
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

export function agentHubCreateScopedApproval(id: string, uses = 1, lifetimeMinutes = 30): Promise<ScopedApproval> {
  return callBridge(() => AppService.AgentHubCreateScopedApproval(id, uses, lifetimeMinutes) as Promise<ScopedApproval>);
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
    scopedApprovalEligible: record.scopedApprovalEligible === true,
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

/** Go stopped waiting for a permission: answered, withdrawn by the agent, turn ended or expired. */
export function onAgentHubPermissionClosed(handler: (event: PermissionClosedEvent) => void): () => void {
  return Events.On(PERMISSION_CLOSED_EVENT, (ev) => {
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
          scopedApprovalEligible: (raw as any).scopedApprovalEligible,
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

/**
 * A session someone else created — an agent's child, or one from the terminal.
 * The window's own sessions are upserted from the call's answer and never
 * arrive here.
 */
export function onAgentHubSessionCreated(handler: (sessionId: string) => void): () => void {
  return Events.On(SESSION_CREATED_EVENT, (ev) => {
    const parsed = parseSessionEvent(ev?.data);
    if (parsed) handler(parsed.sessionId);
  });
}

/** A turn began, whoever started it — the window's own turns included. */
export function onAgentHubTurnStarted(handler: (sessionId: string) => void): () => void {
  return Events.On(TURN_STARTED_EVENT, (ev) => {
    const parsed = parseSessionEvent(ev?.data);
    if (parsed) handler(parsed.sessionId);
  });
}

/**
 * A session's own work changed: the vendor started or ended a turn or a task
 * without DevKit's help, or its process went away. The payload is the whole
 * current state, not a delta.
 */
export function onAgentHubActivity(
  handler: (event: { sessionId: string; activity: SessionActivity }) => void,
): () => void {
  return Events.On(ACTIVITY_EVENT, (ev) => {
    const data = Array.isArray(ev?.data) ? ev.data[0] : ev?.data;
    if (!data || typeof data !== "object") return;
    const value = data as Record<string, unknown>;
    if (typeof value.sessionId !== "string" || value.sessionId.length === 0) return;
    handler({ sessionId: value.sessionId, activity: toActivity(value) });
  });
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

/** A PDF attached to a session: what the summary card lists. */
export type AgentDocument = { id: string; name: string; pages: number; size: number };

/** The session's PDFs, oldest first. Metadata only — no bytes, no text. */
export function agentHubListDocuments(sessionId: string): Promise<AgentDocument[]> {
  return callBridge(async () => {
    const raw = (await AppService.AgentHubListDocuments(sessionId)) ?? [];
    return raw.map((d) => ({ id: d.id, name: d.name, pages: d.pages ?? 0, size: d.size ?? 0 }));
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

/** How much of a call's arguments a permission card shows; the rest is elided. */
const PERMISSION_ARGS_LIMIT = 240;

/**
 * What a request without a command is about: the question the agent put
 * (Codex asks before an MCP tool runs, and says which tool, as text content)
 * and the arguments of the call, as one compact line (`args`, elided) and in
 * full (`argsFull`, for the card's "Show full request"). Only for a call that is
 * not an edit or a read — an edit shows its diff, a read its path — so a
 * file's whole content never lands in the card as JSON.
 */
export function permissionDetail(params: unknown): { message?: string; args?: string; argsFull?: string } {
  if (!params || typeof params !== "object") return {};
  const toolCall = (params as Record<string, unknown>).toolCall;
  if (!toolCall || typeof toolCall !== "object") return {};
  const tc = toolCall as Record<string, unknown>;
  const kind = typeof tc.kind === "string" ? tc.kind : "";
  if (kind && kind !== "other" && kind !== "fetch") return {};
  const out: { message?: string; args?: string; argsFull?: string } = {};
  if (Array.isArray(tc.content)) {
    for (const entry of tc.content) {
      if (!entry || typeof entry !== "object") continue;
      const block = (entry as Record<string, unknown>).content;
      if (!block || typeof block !== "object") continue;
      const text = (block as Record<string, unknown>).text;
      if (typeof text === "string" && text.trim()) {
        out.message = text.trim();
        break;
      }
    }
  }
  const raw = tc.rawInput;
  if (raw && typeof raw === "object" && !Array.isArray(raw) && Object.keys(raw).length > 0) {
    const line = JSON.stringify(raw);
    out.args = line.length > PERMISSION_ARGS_LIMIT ? `${line.slice(0, PERMISSION_ARGS_LIMIT - 1)}…` : line;
    // The toast's one line is elided; the card shows (and expands to) all of it.
    out.argsFull = line;
  }
  return out;
}

const FILES_DROPPED_EVENT = "agenthub:filesDropped";

export type FilesDropped = {
  /** Which composer the files were dropped on (its data-composer-id). */
  composer: string;
  /** Absolute paths of regular files. */
  paths: string[];
  /** Folders and unreadable items that were left out. */
  skipped: number;
};

/** Reads a drop event's payload; anything malformed is not a drop. */
export function parseFilesDropped(data: unknown): FilesDropped | null {
  const record = (Array.isArray(data) ? data[0] : data) as Record<string, unknown> | null;
  if (!record || typeof record !== "object" || typeof record.composer !== "string") return null;
  return {
    composer: record.composer,
    paths: Array.isArray(record.paths) ? record.paths.filter((p): p is string => typeof p === "string") : [],
    skipped: typeof record.skipped === "number" ? record.skipped : 0,
  };
}

/** Files dragged from the OS onto a prompt composer (desktop only). Returns unsubscribe. */
export function onAgentHubFilesDropped(handler: (drop: FilesDropped) => void): () => void {
  return Events.On(FILES_DROPPED_EVENT, (ev) => {
    const drop = parseFilesDropped(ev?.data);
    if (drop) handler(drop);
  });
}

/** One of the person's "which model for which job" profiles (Settings → Agents). */
export type ModelProfile = {
  id: string;
  name: string;
  description: string;
  vendor: string;
  model: string;
  effort: string;
  notes: string;
  /** Also offer the profile to its vendor's own agents as a subagent (Claude's Agent tool, Codex's spawn_agent). */
  subagent: boolean;
  /** The subagent's instructions, in the person's words. */
  prompt: string;
};

/** A profile with whether its model is still on its vendor's list. */
export type ModelProfileCheck = ModelProfile & {
  /** "ok", or "unknownModel", "unknownEffort", "signInRequired", "notInstalled", "unavailable", "unknownVendor". */
  status: string;
  problem: string;
  /** What the vendor will call the subagent; empty when the switch is off. */
  subagentName: string;
  /** "active" (new sessions of the vendor have it), "skipped" (see subagentProblem), or "" when off. */
  subagentStatus: string;
  subagentProblem: string;
};

export type ModelProfilesState = { revision: number; profiles: ModelProfile[] };

function profileOf(raw: { id?: string; name?: string; description?: string; vendor?: string; model?: string; effort?: string; notes?: string; subagent?: boolean; prompt?: string }): ModelProfile {
  return {
    id: raw.id ?? "",
    name: raw.name ?? "",
    description: raw.description ?? "",
    vendor: raw.vendor ?? "",
    model: raw.model ?? "",
    effort: raw.effort ?? "",
    notes: raw.notes ?? "",
    subagent: raw.subagent ?? false,
    prompt: raw.prompt ?? "",
  };
}

export function agentHubGetModelProfiles(): Promise<ModelProfilesState> {
  return callBridge(async () => {
    const state = await AppService.AgentHubGetModelProfiles();
    return { revision: state.revision, profiles: (state.profiles ?? []).map(profileOf) };
  });
}

/** Replaces the list when `revision` is the stored one; the answer is what was stored. */
export function agentHubSetModelProfiles(revision: number, profiles: ModelProfile[]): Promise<ModelProfilesState> {
  return callBridge(async () => {
    const state = await AppService.AgentHubSetModelProfiles(revision, profiles);
    return { revision: state.revision, profiles: (state.profiles ?? []).map(profileOf) };
  });
}

export function agentHubCheckModelProfiles(): Promise<ModelProfileCheck[]> {
  return callBridge(async () =>
    ((await AppService.AgentHubCheckModelProfiles()) ?? []).map((raw) => ({
      ...profileOf(raw),
      status: raw.status ?? "",
      problem: raw.problem ?? "",
      subagentName: raw.subagentName ?? "",
      subagentStatus: raw.subagentStatus ?? "",
      subagentProblem: raw.subagentProblem ?? "",
    })),
  );
}

/** The cap on subagents. `codexMaxSubagents` is null while unset (DevKit passes Codex nothing). */
export type SubagentLimitsState = { revision: number; codexMaxSubagents: number | null };

export function agentHubGetSubagentLimits(): Promise<SubagentLimitsState> {
  return callBridge(async () => {
    const state = await AppService.AgentHubGetSubagentLimits();
    return { revision: state.revision, codexMaxSubagents: state.codexMaxSubagents ?? null };
  });
}

/** Replaces the cap when `revision` is the stored one; null clears it. The answer is what was stored. */
export function agentHubSetSubagentLimits(revision: number, codexMaxSubagents: number | null): Promise<SubagentLimitsState> {
  return callBridge(async () => {
    const state = await AppService.AgentHubSetSubagentLimits(revision, codexMaxSubagents);
    return { revision: state.revision, codexMaxSubagents: state.codexMaxSubagents ?? null };
  });
}
