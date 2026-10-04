/**
 * A way to open the title bar's "New workspace" dialog from elsewhere — the
 * first-run checklist's "Create workspace". Same shape as settingsRequest: the
 * WorkspaceSwitcher owns the dialog and listens.
 */
const EVENT = "devkit:new-workspace-requested";

export function requestNewWorkspace(): void {
  window.dispatchEvent(new CustomEvent(EVENT));
}

export function onNewWorkspaceRequested(handler: () => void): () => void {
  window.addEventListener(EVENT, handler);
  return () => window.removeEventListener(EVENT, handler);
}
