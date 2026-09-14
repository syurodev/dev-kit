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
  callerSessionId?: string;
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

export type PermissionOption = {
  optionId: string;
  name: string;
  kind: string;
};

const SESSION_UPDATE_EVENT = "agenthub:sessionUpdate";
const PERMISSION_EVENT = "agenthub:permission";

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

function isoString(value: unknown): string {
  return typeof value === "string" ? value : "";
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
