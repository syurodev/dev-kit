import { Events } from "@wailsio/runtime";
import { AppService } from "../../bindings/devkit/internal/bridge";
import type { SignInFlow, SignInStatus } from "../../bindings/devkit/internal/agenthub/models";
import { callBridge } from "./client";

export type { SignInFlow, SignInStatus };

/**
 * Signing in inside DevKit, for a vendor that offers it (Codex). Claude's
 * sign-in runs its own login in a terminal instead — bridge/agentRuntime.
 *
 * DevKit starts the vendor's login and shows its page or code; the credential
 * lands in the vendor's own home, never here. There is no MCP tool for any of
 * it, and the backend refuses an agent caller.
 */

/** How a sign-in ended, from the agenthub:signInCompleted event. */
export type SignInResult = {
  agent: string;
  loginId: string;
  success: boolean;
  error: string;
};

const SIGN_IN_COMPLETED_EVENT = "agenthub:signInCompleted";

export function agentHubSignInStatus(agent: string): Promise<SignInStatus> {
  return callBridge(() => AppService.AgentHubSignInStatus(agent));
}

/** Starts the default sign-in; open `authUrl` for the person. */
export function agentHubStartBrowserSignIn(agent: string): Promise<SignInFlow> {
  return callBridge(() => AppService.AgentHubStartBrowserSignIn(agent));
}

/** Starts a sign-in by code; show `verificationUrl` and `userCode`. */
export function agentHubStartDeviceSignIn(agent: string): Promise<SignInFlow> {
  return callBridge(() => AppService.AgentHubStartDeviceSignIn(agent));
}

export function agentHubCancelSignIn(agent: string, loginId: string): Promise<void> {
  return callBridge(() => AppService.AgentHubCancelSignIn(agent, loginId) as Promise<void>);
}

export function agentHubSignOut(agent: string): Promise<void> {
  return callBridge(() => AppService.AgentHubSignOut(agent) as Promise<void>);
}

/** The Accounts token Codex signs in with, or "" for its ChatGPT sign-in. */
export function getCodexApiKeyAccount(): Promise<string> {
  return callBridge(async () => (await AppService.PreferencesGetCodexAPIKeyAccount()) ?? "");
}

/** Chooses that token by id; "" goes back to ChatGPT. The key never crosses. */
export function saveCodexApiKeyAccount(accountId: string): Promise<void> {
  return callBridge(() => AppService.PreferencesSetCodexAPIKeyAccount(accountId) as Promise<void>);
}

/** Reads the event's payload; anything malformed is not a result. */
export function parseSignInResult(data: unknown): SignInResult | null {
  const record = (Array.isArray(data) ? data[0] : data) as Record<string, unknown> | null;
  if (!record || typeof record !== "object") return null;
  if (typeof record.agent !== "string" || typeof record.loginId !== "string") return null;
  return {
    agent: record.agent,
    loginId: record.loginId,
    success: record.success === true,
    error: typeof record.error === "string" ? record.error : "",
  };
}

/** Every sign-in that ends — succeeded, failed, cancelled, timed out. Returns unsubscribe. */
export function onAgentSignInCompleted(handler: (result: SignInResult) => void): () => void {
  return Events.On(SIGN_IN_COMPLETED_EVENT, (ev) => {
    const result = parseSignInResult(ev?.data);
    if (result) handler(result);
  });
}

/** "ChatGPT · a@b.c · plus", "API key", or "Not signed in". */
export function signInStatusLine(status: SignInStatus, apiKeyLabel = ""): string {
  if (!status.signedIn) return "Not signed in";
  if (status.method === "apiKey") return apiKeyLabel ? `API key · ${apiKeyLabel}` : "API key";
  const parts = ["ChatGPT", status.email, status.plan].filter((part): part is string => Boolean(part));
  return parts.join(" · ");
}
