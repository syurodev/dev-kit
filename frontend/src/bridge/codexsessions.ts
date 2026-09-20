import { AppService } from "../../bindings/devkit/internal/bridge";
import { callBridge } from "./client";

/**
 * What DevKit observed of an external Codex CLI session — one DevKit did not
 * spawn and does not run. Codex's own lifecycle hooks call back into DevKit's
 * MCP server as they fire; see docs/dev-kit/agent-hub.mdx for the mechanism.
 */

export type CodexHookEvent =
  | "SessionStart"
  | "UserPromptSubmit"
  | "PreToolUse"
  | "PostToolUse"
  | "Stop"
  | "SessionEnd";

/** One session, summarized from its most recent event. */
export type CodexSession = {
  id: string;
  cwd: string;
  model: string;
  startedAt: string;
  lastEventAt: string;
  lastEvent: CodexHookEvent;
  eventCount: number;
};

/**
 * One hook call. `prompt`, `toolInput`, `toolResponse` and `lastMessage` are
 * sealed at rest and come back empty when the vault is locked — a locked
 * vault, not a missing event. `summary` is always readable: it is what lets
 * the list render without unlocking anything.
 */
export type CodexSessionEvent = {
  id: string;
  sessionId: string;
  event: CodexHookEvent;
  cwd: string;
  turnId?: string;
  model?: string;
  permissionMode?: string;
  toolName?: string;
  summary: string;
  prompt?: string;
  toolInput?: unknown;
  toolResponse?: unknown;
  lastMessage?: string;
  occurredAt: string;
};

export function listCodexSessions(): Promise<CodexSession[]> {
  return callBridge(
    async () => ((await AppService.CodexSessionsList()) ?? []) as CodexSession[],
  );
}

/** One session's transcript, oldest first. `limit` <= 0 means everything kept. */
export function codexSessionEvents(sessionId: string, limit = 0): Promise<CodexSessionEvent[]> {
  return callBridge(
    async () =>
      ((await AppService.CodexSessionEvents(sessionId, limit)) ?? []) as CodexSessionEvent[],
  );
}

export function clearCodexSession(sessionId: string): Promise<void> {
  return callBridge(async () => {
    await AppService.CodexSessionsClearSession(sessionId);
  });
}

/** Forgets every session this machine has observed. */
export function clearAllCodexSessions(): Promise<void> {
  return callBridge(async () => {
    await AppService.CodexSessionsClear();
  });
}
