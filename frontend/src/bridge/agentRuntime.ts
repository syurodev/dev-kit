import { Events } from "@wailsio/runtime";
import { AppService } from "../../bindings/devkit/internal/bridge";
import type {
  InstalledRuntime,
  Requirement,
  Status,
} from "../../bindings/devkit/internal/agentruntime/models";
import type { AuthMethod } from "../../bindings/devkit/internal/agenthub/models";
import { callBridge, callBridgeCancellable, type CancellableCall } from "./client";
import { toAppError } from "./errors";

export type { AuthMethod, InstalledRuntime, Requirement };

/** Status with `requirements` never null — Go's nil slice arrives as null. */
export type AgentRuntimeStatus = Omit<Status, "requirements"> & { requirements: Requirement[] };

export type ClaudeBinarySource = "bundled" | "machine";

/** Claude's adapter, the runtime DevKit installs for the "claude" vendor. */
export const CLAUDE_AGENT = "claude-acp";

/** OpenAI's Codex CLI, run as `codex app-server` for the "codex" vendor. */
export const CODEX_AGENT = "codex";

/**
 * opencode, run as `opencode acp` for the "opencode" vendor. The one runtime
 * with no tested version: DevKit installs npm's latest.
 */
export const OPENCODE_AGENT = "opencode";

function normalize(status: Status): AgentRuntimeStatus {
  return { ...status, requirements: status.requirements ?? [] };
}

export function agentRuntimeStatus(agent: string): Promise<AgentRuntimeStatus> {
  return callBridge(async () => normalize(await AppService.AgentRuntimeStatus(agent)));
}

export function agentRuntimeCheckLatest(agent: string): Promise<AgentRuntimeStatus> {
  return callBridge(async () => normalize(await AppService.AgentRuntimeCheckLatest(agent)));
}

/**
 * Installs and activates a version — DevKit's tested one when `version` is "".
 * npm's output arrives through onAgentRuntimeInstallOutput while it runs;
 * cancelling kills npm and discards the staged tree.
 */
export function agentRuntimeInstall(
  agent: string,
  version = "",
): CancellableCall<AgentRuntimeStatus> {
  return callBridgeCancellable(() => AppService.AgentRuntimeInstall(agent, version), normalize);
}

export function agentRuntimeActivate(agent: string, version: string): Promise<AgentRuntimeStatus> {
  return callBridge(async () => normalize(await AppService.AgentRuntimeActivate(agent, version)));
}

export function agentRuntimeRemove(agent: string, version: string): Promise<AgentRuntimeStatus> {
  return callBridge(async () => normalize(await AppService.AgentRuntimeRemove(agent, version)));
}

export function agentRuntimeSetClaudeBinary(source: ClaudeBinarySource): Promise<AgentRuntimeStatus> {
  return callBridge(async () => normalize(await AppService.AgentRuntimeSetClaudeBinary(source)));
}

const INSTALL_OUTPUT_EVENT = "agenthub:runtimeInstallOutput";

/** Every line npm writes during an install, already redacted. Returns unsubscribe. */
export function onAgentRuntimeInstallOutput(
  handler: (agent: string, line: string) => void,
): () => void {
  return Events.On(INSTALL_OUTPUT_EVENT, (ev) => {
    handler(String(ev.data?.agent ?? ""), String(ev.data?.line ?? ""));
  });
}

export function isAgentNotInstalledError(error: unknown): boolean {
  return toAppError(error).code === "agent_not_installed";
}

export function isRuntimeRequirementError(error: unknown): boolean {
  return toAppError(error).code === "runtime_requirement";
}

/** Sign-in is per agent type ("claude"), not per runtime id. */
export const CLAUDE_VENDOR = "claude";

/**
 * The sign-ins an agent offers. Each is the agent's own login command; DevKit
 * never shows a credential field of its own.
 */
export function agentHubAuthMethods(agent: string): Promise<AuthMethod[]> {
  return callBridge(async () => (await AppService.AgentHubAuthMethods(agent)) ?? []);
}

/**
 * Starts one sign-in in a terminal session and resolves to its id. Only the
 * method id is sent; the backend decides what that runs.
 */
export function agentHubStartSignIn(
  agent: string,
  methodId: string,
  rows: number,
  cols: number,
): Promise<string> {
  return callBridge(() => AppService.AgentHubStartSignIn(agent, methodId, rows, cols));
}

export function isAgentAuthRequiredError(error: unknown): boolean {
  return toAppError(error).code === "agent_auth_required";
}

/**
 * The API key chosen in Settings is gone or was refused. A sign-in panel
 * would not help — the key wins at every spawn — so this points at Settings.
 */
export function isAgentApiKeyError(error: unknown): boolean {
  return toAppError(error).code === "agent_api_key_unusable";
}

const SIGN_IN_LABELS: Record<string, string> = {
  "claude-ai-login": "Sign in with Claude subscription",
  "console-login": "Sign in with Anthropic Console",
  "claude-login": "Sign in with Claude",
  // The agent names it "Login with opencode", which read as "Sign in with Login with opencode".
  "opencode-login": "Sign in to opencode",
};

/** The button for a method: known ids get plain words, anything new its own name. */
export function signInLabel(method: Pick<AuthMethod, "id" | "name">): string {
  return SIGN_IN_LABELS[method.id] ?? `Sign in with ${method.name || method.id}`;
}

export type SignInOutcome = { signedIn: boolean; message: string };

/**
 * What a sign-in's exit code means. Only 0 retries the failed action; -1 is a
 * tab closed (or a process killed) before the login finished.
 */
export function signInOutcome(code: number): SignInOutcome {
  if (code === 0) return { signedIn: true, message: "Signed in." };
  if (code < 0) return { signedIn: false, message: "Sign-in stopped before it finished." };
  return { signedIn: false, message: `Sign-in exited with code ${code}.` };
}

/** Nothing to run yet, or the machine lost what the runtime needs. */
export function runtimeBlocked(status: AgentRuntimeStatus): boolean {
  return !status.current || status.requirements.some((r) => !r.met);
}

/**
 * The one line that says where an agent stands. `latest` is set only when the
 * registry has something newer than both the tested and the installed version.
 */
export function runtimeStatusLine(
  status: AgentRuntimeStatus,
  name = "Claude Agent",
): {
  headline: string;
  latest: string | null;
} {
  const { current, testedVersion, latestVersion } = status;
  // An agent with no tested version (opencode) is always the latest DevKit could
  // install; "tested" and "not tested" would both be untrue of it.
  const pinned = testedVersion !== "";
  const headline = !current
    ? "Not installed"
    : `${name} ${current.version}${pinned ? (current.version === testedVersion ? " · tested" : " · not tested by DevKit") : ""}`;
  // Unpinned: there is only something to update when something is installed;
  // with nothing installed "Install latest" is the one action.
  const newer =
    latestVersion &&
    (pinned
      ? compareVersions(latestVersion, testedVersion) > 0 && (!current || compareVersions(latestVersion, current.version) > 0)
      : current !== null && compareVersions(latestVersion, current.version) > 0);
  const latest = newer ? (pinned ? `${latestVersion} available (not tested by DevKit)` : `${latestVersion} available`) : null;
  return { headline, latest };
}

/**
 * The small line under an install's name. Claude's adapter is Node code over an
 * SDK, so both versions matter; a native binary (Codex, opencode) has no SDK and
 * Node was only there to install it, so it is just the disk it takes.
 */
export function runtimeDetail(current: Pick<InstalledRuntime, "sdkVersion" | "nodeVersion">, diskBytes: number): string {
  const disk = `${formatBytes(diskBytes)} on disk`;
  return current.sdkVersion && current.nodeVersion ? `SDK ${current.sdkVersion} · Node ${current.nodeVersion} · ${disk}` : disk;
}

/** The backticked commands in an install hint ("e.g. `brew install node`"), for Copy buttons. */
export function hintCommands(hint: string): string[] {
  return [...hint.matchAll(/`([^`]+)`/g)].map((match) => match[1]);
}

export function formatBytes(bytes: number): string {
  if (!bytes || bytes < 1024) return `${bytes || 0} B`;
  const units = ["KB", "MB", "GB", "TB"];
  let value = bytes / 1024;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit += 1;
  }
  return `${value < 10 ? value.toFixed(1) : Math.round(value)} ${units[unit]}`;
}

/**
 * Orders two dotted versions by their numeric parts, so "0.10.0" sorts after
 * "0.9.0". A string comparison would say otherwise, and the button that moves
 * off an untested version would call a downgrade an update.
 */
export function compareVersions(a: string, b: string): number {
  const left = a.match(/\d+/g)?.map(Number) ?? [];
  const right = b.match(/\d+/g)?.map(Number) ?? [];
  for (let i = 0; i < Math.max(left.length, right.length); i += 1) {
    const diff = (left[i] ?? 0) - (right[i] ?? 0);
    if (diff !== 0) return Math.sign(diff);
  }
  return 0;
}
