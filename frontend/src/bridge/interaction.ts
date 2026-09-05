import { callBridge } from "./client";
import { AppService } from "../../bindings/devkit/internal/bridge";

export type InteractionType = "select" | "confirm";
export type InteractionKind = "ssh" | "database" | "kafka" | "";

export type PendingInteraction = {
  id: string;
  type: InteractionType;
  kind: InteractionKind;
  env: string;
  tool: string;
  summary: string;
  allowAlwaysOffered: boolean;
  caller: string;
};

function toPending(raw: {
  id: string;
  type: string;
  kind?: string;
  env?: string;
  tool?: string;
  summary?: string;
  allowAlwaysOffered?: boolean;
  caller?: string;
} | null): PendingInteraction | null {
  if (!raw) {
    return null;
  }
  const type: InteractionType = raw.type === "confirm" ? "confirm" : "select";
  const kind =
    raw.kind === "ssh" || raw.kind === "database" || raw.kind === "kafka"
      ? raw.kind
      : "";
  return {
    id: raw.id,
    type,
    kind,
    env: raw.env ?? "",
    tool: raw.tool ?? "",
    summary: raw.summary ?? "",
    allowAlwaysOffered: Boolean(raw.allowAlwaysOffered),
    caller: raw.caller ?? "",
  };
}

export function getInteractionPending(): Promise<PendingInteraction | null> {
  return callBridge(async () => {
    const pending = await AppService.InteractionPending();
    return toPending(pending);
  });
}

export function getInteractionPendingList(): Promise<PendingInteraction[]> {
  return callBridge(async () => {
    const list = (await AppService.InteractionPendingList()) ?? [];
    return list
      .map((raw) => toPending(raw))
      .filter((p): p is PendingInteraction => p !== null);
  });
}

export function completeInteraction(id: string, value: string): Promise<void> {
  return callBridge(() => AppService.InteractionComplete(id, value) as Promise<void>);
}

export function denyInteraction(id: string): Promise<void> {
  return callBridge(() => AppService.InteractionDeny(id) as Promise<void>);
}
