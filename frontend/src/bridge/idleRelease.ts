import { AppService } from "../../bindings/devkit/internal/bridge";
import type { AgentIdleReleaseState } from "../../bindings/devkit/internal/bridge/models";
import { callBridge } from "./client";

export type { AgentIdleReleaseState };

/** The person's own setting for releasing idle agent sessions; window only. */
export function agentIdleRelease(): Promise<AgentIdleReleaseState> {
  return callBridge(() => AppService.AgentIdleReleaseGet());
}

/** Fails with a validation error when a time is outside 2 minutes to 24 hours. */
export function setAgentIdleRelease(next: AgentIdleReleaseState): Promise<AgentIdleReleaseState> {
  return callBridge(() => AppService.AgentIdleReleaseSet(next.enabled, next.childMinutes, next.topMinutes));
}
