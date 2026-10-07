import { AppService } from "../../bindings/devkit/internal/bridge";
import { callBridge } from "./client";

/**
 * Whether a newer DevKit release exists. It describes; DevKit downloads and
 * installs nothing, so the only thing the UI can do with it is show the
 * release page.
 */
export interface UpdateStatus {
  /** The running version; "dev" for a build that was not stamped by a release. */
  current: string;
  /**
   * Why there is nothing to report: "dev" (an unstamped build), "disabled"
   * (automatic checks are off) or "no_release" (nothing published yet). Empty
   * when the lookup ran.
   */
  skipped: "" | "dev" | "disabled" | "no_release";
  latest: string;
  available: boolean;
  /** True when `latest` is a beta — only ever offered to someone already on one. */
  prerelease: boolean;
  /** The release page; empty unless `available`. */
  url: string;
  publishedAt: string;
}

/** The version this binary was built from. */
export function getCurrentVersion(): Promise<string> {
  return callBridge(() => AppService.UpdatesCurrentVersion());
}

/**
 * Looks up the newest release on this build's channel (stable builds see
 * stable releases, beta builds see both).
 *
 * `automatic` is the startup check: it is skipped when the person turned
 * automatic checks off. The "Check now" button passes false and is always
 * answered.
 */
export function checkForUpdate(automatic: boolean): Promise<UpdateStatus> {
  return callBridge(async () => (await AppService.UpdatesCheck(automatic)) as UpdateStatus);
}

/** Whether DevKit checks for a newer release when it starts. On by default. */
export function getUpdateChecks(): Promise<boolean> {
  return callBridge(() => AppService.PreferencesGetUpdateChecks());
}

export function saveUpdateChecks(enabled: boolean): Promise<void> {
  return callBridge(() => AppService.PreferencesSetUpdateChecks(enabled));
}

/** "1.4.0" → "v1.4.0"; the dev placeholder is left alone. */
export function versionLabel(version: string): string {
  return /^\d/.test(version) ? `v${version}` : version;
}

/** One line for a finished check, for the Settings row. */
export function updateStatusLine(status: UpdateStatus): string {
  switch (status.skipped) {
    case "dev":
      return "This is a development build, so there is nothing to compare against.";
    case "disabled":
      return "Automatic checks are off.";
    case "no_release":
      return "No release has been published yet.";
  }
  if (status.available) {
    return `${versionLabel(status.latest)} is available.`;
  }
  return "You are on the latest version.";
}
