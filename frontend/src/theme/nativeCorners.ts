/**
 * Stamps `data-os` on <html> so index.css can pick the window's corner radius.
 *
 * The window's corner is drawn by the OS and we cannot change it, which makes
 * it the one measurement DevKit has to import rather than specify. Everything
 * inside the window stays on the ladder in index.css.
 *
 * No Go needed: Wails already reads `kern.osproductversion` on darwin and
 * hands it over as OSInfo.Version (internal/operatingsystem/os_darwin.go).
 *
 * Failure and every unrecognised platform fall through to no attribute, which
 * is the default `--radius-window` — the geometry the shell already had. A
 * platform is only listed once its window corner has actually been measured;
 * Windows and Linux are deliberately absent rather than guessed at.
 */
export async function applyNativeCorners(): Promise<void> {
  if (!("_wails" in window)) return;

  try {
    // Imported here, not at module scope: the Wails runtime logs a browser
    // warning on import, and this module is otherwise a pure function that
    // tests can reach without pulling the shell in.
    const { System } = await import("@wailsio/runtime");
    const env = await System.Environment();
    const os = nativeCornerKey(env.OS, env.OSInfo?.Version);
    if (os) document.documentElement.dataset.os = os;
  } catch {
    // A shell that cannot ask the platform keeps the default corner. There is
    // nothing to report: the fallback is a complete, correct look.
  }
}

/** Exported for the test; `undefined` means "use the default radius". */
export function nativeCornerKey(
  os: string | undefined,
  version: string | undefined,
): string | undefined {
  if (os !== "darwin") return undefined;
  // "27.0" -> 27. Anything unparseable is an unknown macOS, so: default.
  const major = Number.parseInt(version ?? "", 10);
  if (!Number.isFinite(major)) return undefined;
  return major >= 26 ? "macos-26" : undefined;
}
