import { AppService } from "../../bindings/devkit/internal/bridge";
import { callBridge } from "./client";

export type SyncStatus = {
  connected: boolean;
  endpoint: string;
  accountId: string;
  deviceId: string;
  expiresAt: string;
};

export type AuthStatus = {
  authenticated: boolean;
  expiresAt: string;
};

export type SyncReport = {
  pushAttempted: number;
  pushSent: number;
  pushRetried: number;
  pushConflicts: number;
  pulled: number;
  applied: number;
};

export type SyncConflict = {
  recordId: string;
  recordType: string;
  localChangeKey: string;
  remoteChangeKey: string;
  detectedAt: string;
};

export function syncConflicts(): Promise<SyncConflict[]> {
  return callBridge(async () => {
    const conflicts = (await AppService.SyncConflicts()) ?? [];
    return conflicts.map((conflict) => ({
      recordId: conflict.record_id,
      recordType: conflict.record_type,
      localChangeKey: conflict.local_change_key,
      remoteChangeKey: conflict.remote_change_key,
      detectedAt: conflict.detected_at ? String(conflict.detected_at) : "",
    }));
  });
}

export type SyncConflictDetail = {
  recordId: string;
  recordType: string;
  theirsCaptured: boolean;
  mergeable: boolean;
  oursText: string;
  theirsText: string;
};

export type ResolveStrategy = "ours" | "theirs" | "merge";

export function syncConflictDetail(id: string): Promise<SyncConflictDetail> {
  return callBridge(async () => {
    const detail = await AppService.SyncConflictDetail(id);
    return {
      recordId: detail.record_id,
      recordType: detail.record_type,
      theirsCaptured: detail.theirs_captured,
      mergeable: detail.mergeable,
      oursText: detail.ours_text ?? "",
      theirsText: detail.theirs_text ?? "",
    };
  });
}

export function syncResolveConflict(
  id: string,
  strategy: ResolveStrategy,
  mergedText = "",
): Promise<void> {
  return callBridge(
    () => AppService.SyncResolveConflict(id, strategy, mergedText) as Promise<void>,
  );
}

export function syncStatus(): Promise<SyncStatus> {
  return callBridge(async () => {
    const status = await AppService.SyncStatus();
    return {
      connected: status.connected,
      endpoint: status.endpoint ?? "",
      accountId: status.account_id ?? "",
      deviceId: status.device_id ?? "",
      expiresAt: status.expires_at ? String(status.expires_at) : "",
    };
  });
}

export function authStatus(): Promise<AuthStatus> {
  return callBridge(async () => {
    const status = await AppService.AuthStatus();
    return {
      authenticated: status.authenticated,
      expiresAt: status.expires_at ? String(status.expires_at) : "",
    };
  });
}

export function authLogout(): Promise<void> {
  return callBridge(() => AppService.AuthLogout() as Promise<void>);
}

export function syncConnect(): Promise<void> {
  return callBridge(
    () => AppService.SyncConnect() as Promise<void>,
  );
}

export function syncNow(): Promise<SyncReport> {
  return callBridge(async () => {
    const report = await AppService.SyncNow();
    return {
      pushAttempted: report.push_attempted,
      pushSent: report.push_sent,
      pushRetried: report.push_retried,
      pushConflicts: report.push_conflicts,
      pulled: report.pulled,
      applied: report.applied,
    };
  });
}

export function syncDisconnect(): Promise<void> {
  return callBridge(() => AppService.SyncDisconnect() as Promise<void>);
}
