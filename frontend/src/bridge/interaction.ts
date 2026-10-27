import { callBridge } from "./client";
import { AppService } from "../../bindings/devkit/internal/bridge";

export type InteractionType = "select" | "confirm";
export type InteractionKind =
  | "ssh"
  | "database"
  | "kafka"
  | "git_account"
  /** A change to the workspace tool-group profile or a session's groups: always asked. */
  | "harness"
  /** Where an agent's request for tool groups is saved: this session or the workspace. */
  | "harness_scope"
  | "";

export type AccountOption = {
  id: string;
  label: string;
  provider: string;
  subject: string;
};

export type PendingInteraction = {
  id: string;
  type: InteractionType;
  kind: InteractionKind;
  env: string;
  tool: string;
  summary: string;
  allowAlwaysOffered: boolean;
  caller: string;
  /** Accounts to choose between; only a git_account selection has any. */
  options: AccountOption[];
  /** What a kept answer is kept for ("acme-corp on github.com"); empty means it cannot be kept. */
  remember: string;
};

export type ScopedApproval = { id: string; caller: string; tool: string; profileId: string; env: string; fingerprint: string; createdAt: string; lastUsedAt?: string; expiresAt: string; remaining: number };

function toPending(raw: {
  id: string;
  type: string;
  kind?: string;
  env?: string;
  tool?: string;
  summary?: string;
  allowAlwaysOffered?: boolean;
  caller?: string;
  options?: { id: string; label: string; provider: string; subject?: string }[] | null;
  remember?: string;
} | null): PendingInteraction | null {
  if (!raw) {
    return null;
  }
  const type: InteractionType = raw.type === "confirm" ? "confirm" : "select";
  const kind =
    raw.kind === "ssh" ||
    raw.kind === "database" ||
    raw.kind === "kafka" ||
    raw.kind === "git_account" ||
    raw.kind === "harness" ||
    raw.kind === "harness_scope"
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
    options: (raw.options ?? []).map((o) => ({
      id: o.id,
      label: o.label,
      provider: o.provider,
      subject: o.subject ?? "",
    })),
    remember: raw.remember ?? "",
  };
}

export function getScopedApprovals(): Promise<ScopedApproval[]> {
  return callBridge(async () => (await AppService.InteractionScopedApprovals()) ?? []);
}

export function revokeScopedApproval(id: string): Promise<void> {
  return callBridge(() => AppService.InteractionRevokeScopedApproval(id) as Promise<void>);
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

/**
 * The value that completes a git_account selection: the account chosen, and
 * whether to keep the answer for next time. The backend only honours an id it
 * offered, so this cannot name some other account.
 */
export function accountAnswer(id: string, remember: boolean): string {
  return `${remember ? "remember" : "once"}:${id}`;
}
