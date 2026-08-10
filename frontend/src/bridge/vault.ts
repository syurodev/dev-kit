import { callBridge } from "./client";
import { AppService } from "../../bindings/devkit/internal/bridge";

export type VaultState = "not_configured" | "locked" | "unlocked";

export function vaultStatus(): Promise<VaultState> {
  return callBridge(
    async () => (await AppService.VaultStatus()).state as VaultState,
  );
}

export function setupVault(masterPassword: string): Promise<string> {
  return callBridge(() => AppService.SetupVault(masterPassword));
}

export function unlockVault(masterPassword: string): Promise<void> {
  return callBridge(
    () => AppService.UnlockVault(masterPassword) as Promise<void>,
  );
}
