import { useEffect } from "react";
import { Browser } from "@wailsio/runtime";
import { toast } from "sonner";
import { checkForUpdate, versionLabel } from "../bridge/updates";

const NOTIFIED_KEY = "devkit.update.notified";
// Long enough that the main thread is idle and the first screen has painted:
// nothing about an update is urgent enough to compete with startup.
const STARTUP_DELAY_MS = 5000;

function lastNotified(): string {
  try {
    return window.localStorage.getItem(NOTIFIED_KEY) ?? "";
  } catch {
    return "";
  }
}

function rememberNotified(version: string) {
  try {
    window.localStorage.setItem(NOTIFIED_KEY, version);
  } catch {
    // Private window or blocked storage: the worst case is being told again.
  }
}

/**
 * Tells you once per release, shortly after startup, that a newer one exists.
 * Renders nothing.
 *
 * The check is the backend's automatic one, which stays silent when automatic
 * checks are off, for a development build, and when it cannot reach GitHub —
 * a failed lookup at launch is not worth a toast. Declining the toast (or
 * dismissing it) is remembered per version, so the same release does not
 * return on every launch; the Settings page still offers it on demand.
 */
export function UpdateNotifier() {
  useEffect(() => {
    let cancelled = false;
    const timer = window.setTimeout(() => {
      checkForUpdate(true)
        .then((status) => {
          if (cancelled || !status.available || !status.url) return;
          if (lastNotified() === status.latest) return;
          rememberNotified(status.latest);
          toast(`DevKit ${versionLabel(status.latest)} is available`, {
            description: `You are on ${versionLabel(status.current)}.`,
            duration: 20_000,
            action: { label: "View release", onClick: () => void Browser.OpenURL(status.url) },
          });
        })
        .catch(() => {});
    }, STARTUP_DELAY_MS);
    return () => {
      cancelled = true;
      window.clearTimeout(timer);
    };
  }, []);

  return null;
}
