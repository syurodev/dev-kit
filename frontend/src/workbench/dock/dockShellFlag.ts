/**
 * EXPERIMENT — the dockable shell of docs/dev-kit/layout.mdx, off by default.
 *
 * A device-local switch, not a preference: it picks which shell renders, and
 * flipping it reloads the window. localStorage rather than ui_state because it
 * has to be readable synchronously before the first frame, and it is a
 * throw-away flag for the trial, not state the app owns.
 */
const KEY = "devkit.dockShell";

export function isDockShellEnabled(): boolean {
  try {
    return window.localStorage.getItem(KEY) === "1";
  } catch {
    return false;
  }
}

export function toggleDockShell(): void {
  try {
    if (isDockShellEnabled()) window.localStorage.removeItem(KEY);
    else window.localStorage.setItem(KEY, "1");
  } catch {
    return;
  }
  window.location.reload();
}
