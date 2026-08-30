/**
 * A way for a module to say "the vault locked under me" without owning the
 * unlock flow.
 *
 * VaultGate already knows how to unlock and already re-reads vault state; it
 * simply had no reason to re-read after mount. A module that hits a locked
 * vault mid-session dispatches this, VaultGate re-checks, and the app shows
 * the unlock screen it would have shown at startup. No vault state in a store,
 * no second unlock form to keep in sync with the first.
 */
const EVENT = "devkit:vault-unlock-requested";

export function requestVaultUnlock(): void {
  window.dispatchEvent(new CustomEvent(EVENT));
}

export function onVaultUnlockRequested(handler: () => void): () => void {
  window.addEventListener(EVENT, handler);
  return () => window.removeEventListener(EVENT, handler);
}
