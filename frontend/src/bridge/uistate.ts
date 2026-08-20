import { AppService } from "../../bindings/devkit/internal/bridge";
import { callBridge } from "./client";
import type { PersistStorage, StorageValue } from "zustand/middleware";

export function getUIState(key: string): Promise<string> {
  return callBridge(() => AppService.UIStateGet(key));
}

export function setUIState(key: string, value: string): Promise<void> {
  return callBridge(() => AppService.UIStateSet(key, value));
}

function debounce<Args extends unknown[]>(
  fn: (...args: Args) => void,
  delayMs: number,
): (...args: Args) => void {
  let timer: ReturnType<typeof setTimeout> | undefined;
  return (...args: Args) => {
    clearTimeout(timer);
    timer = setTimeout(() => fn(...args), delayMs);
  };
}

/**
 * One implementation of Zustand's PersistStorage interface, shared by every
 * store that needs to survive an app restart — see
 * docs/superpowers/specs/2026-08-20-app-shell-sidebar-design.md § ui_state.
 * getItem never debounces (read once at store init); setItem does, so a
 * resize drag doesn't hammer the bridge on every pixel.
 */
export const uiStateStorage: PersistStorage<unknown> = {
  getItem: async (key) => {
    const raw = await getUIState(key);
    if (!raw) return null;
    return JSON.parse(raw) as StorageValue<unknown>;
  },
  setItem: debounce((key: string, value: StorageValue<unknown>) => {
    void setUIState(key, JSON.stringify(value));
  }, 400),
  removeItem: (key) => {
    void setUIState(key, "");
  },
};
