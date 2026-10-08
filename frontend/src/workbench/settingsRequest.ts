import type { SettingsTab } from "@/modules/settings/settingsNavigation";

/**
 * A way for a module to open Settings on a section without owning the modal —
 * the Agent Hub's "Open agent settings". Same shape as vaultUnlockRequest: the
 * Workbench owns the modal and listens.
 */
const EVENT = "devkit:settings-requested";

export function requestSettings(tab?: SettingsTab): void {
  window.dispatchEvent(new CustomEvent<SettingsTab | undefined>(EVENT, { detail: tab }));
}

export function onSettingsRequested(handler: (tab: SettingsTab | undefined) => void): () => void {
  const listener = (event: Event) => handler((event as CustomEvent<SettingsTab | undefined>).detail);
  window.addEventListener(EVENT, listener);
  return () => window.removeEventListener(EVENT, listener);
}
