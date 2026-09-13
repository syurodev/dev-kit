import { Events } from "@wailsio/runtime";
import { AppService } from "../../bindings/devkit/internal/bridge";
import type {
  InstalledRuntime,
  Requirement,
  Status,
} from "../../bindings/devkit/internal/agentruntime/models";
import { callBridge, callBridgeCancellable, type CancellableCall } from "./client";
import { toAppError } from "./errors";

export type { InstalledRuntime, Requirement };

/** Status with `requirements` never null — Go's nil slice arrives as null. */
export type AgentRuntimeStatus = Omit<Status, "requirements"> & { requirements: Requirement[] };

export type ClaudeBinarySource = "bundled" | "machine";

/** The only agent with a managed runtime today. */
export const CLAUDE_AGENT = "claude-acp";

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

/** Nothing to run yet, or the machine lost what the runtime needs. */
export function runtimeBlocked(status: AgentRuntimeStatus): boolean {
  return !status.current || status.requirements.some((r) => !r.met);
}

/**
 * The one line that says where an agent stands. `latest` is set only when the
 * registry has something newer than both the tested and the installed version.
 */
export function runtimeStatusLine(status: AgentRuntimeStatus): {
  headline: string;
  latest: string | null;
} {
  const { current, testedVersion, latestVersion } = status;
  const headline = !current
    ? "Not installed"
    : `Claude Agent ${current.version}${current.version === testedVersion ? " · tested" : " · not tested by DevKit"}`;
  const latest =
    latestVersion && latestVersion !== testedVersion && latestVersion !== current?.version
      ? `${latestVersion} available (not tested by DevKit)`
      : null;
  return { headline, latest };
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
