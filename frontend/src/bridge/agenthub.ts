import { Events } from "@wailsio/runtime";
import { callBridge } from "./client";
import { AppService } from "../../bindings/devkit/internal/bridge";
import { toAppError } from "./errors";
import type {
  AgentSession as RawSession,
  AgentType as RawAgentType,
  InvokeResult as RawInvokeResult,
} from "../../bindings/devkit/internal/agenthub/models";

export type AgentVendor = "mock" | "claude";

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
  callerSessionId?: string;
};

export type SessionUpdateEvent = {
  sessionId: string;
  update: unknown;
};

export type PendingPermission = {
  id: string;
  sessionId: string;
  params: unknown;
  caller: string;
  title: string;
  depth: number;
};

export type PermissionOption = {
  optionId: string;
  name: string;
  kind: string;
};

const SESSION_UPDATE_EVENT = "agenthub:sessionUpdate";
const PERMISSION_EVENT = "agenthub:permission";

function asVendor(value: string): AgentVendor | string {
  if (value === "mock" || value === "claude") {
    return value;
  }
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
  return "Agent sandbox is unavailable. On Linux install bubblewrap (bwrap); on macOS Seatbelt/sandbox-exec is required. Mock vendor works without a sandbox.";
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
    update: record.update ?? record.Update ?? null,
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
  };
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

export function extractUpdateText(update: unknown): string {
  if (!update || typeof update !== "object") {
    if (typeof update === "string") {
      return update;
    }
    return "";
  }
  const record = update as Record<string, unknown>;
  const content = record.content;
  if (content && typeof content === "object") {
    const block = content as Record<string, unknown>;
    if (typeof block.text === "string") {
      return block.text;
    }
  }
  if (typeof record.text === "string") {
    return record.text;
  }
  try {
    return JSON.stringify(update);
  } catch {
    return "";
  }
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
