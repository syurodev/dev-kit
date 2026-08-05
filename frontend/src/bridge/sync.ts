import { AppService } from "../../bindings/changeme/internal/bridge";
import { callBridge } from "./client";

export type SyncStatus = {
  connected: boolean;
  endpoint: string;
  accountId: string;
  deviceId: string;
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

export function syncConnect(endpoint: string, accessToken: string): Promise<void> {
  return callBridge(
    () => AppService.SyncConnect(endpoint, accessToken) as Promise<void>,
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
