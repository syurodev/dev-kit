import type { Platform } from "@/lib/platform";

/**
 * What the file manager is called where the person is. The action is the same
 * everywhere — show this file in the folder it lives in — and the name is not:
 * Finder, File Explorer, and on Linux whichever manager the desktop has.
 */
export function revealLabel(platform: Platform): string | null {
  switch (platform) {
    case "mac":
      return "Reveal in Finder";
    case "windows":
      return "Show in File Explorer";
    case "linux":
      return "Show in File Manager";
    // A browser (the gallery, server mode) has no file manager on the person's side to show it in.
    default:
      return null;
  }
}

/** The file's full path as the machine writes it: `/` on macOS and Linux, `\` for a Windows folder. */
export function fullPath(folderPath: string, relative: string): string {
  const windows = /^[A-Za-z]:[\\/]/.test(folderPath) || folderPath.startsWith("\\\\");
  const root = folderPath.replace(/[\\/]+$/, "");
  const rel = relative.replace(/^\/+/, "");
  return windows ? `${root}\\${rel}`.replace(/\//g, "\\") : `${root}/${rel}`;
}
