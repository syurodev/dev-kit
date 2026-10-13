import { Browser } from "@wailsio/runtime";
import { toast } from "sonner";
import { copyText } from "./clipboard";

type Deps = { open: (url: string) => Promise<void>; copy: (text: string) => Promise<void>; notify: (message: string) => void };

const ALLOWED = /^(https?:|mailto:)/i;

/** The testable core: open what is allowed, copy it when the runtime cannot. */
export function openExternalWith(deps: Deps) {
  return async (url: string): Promise<void> => {
    const trimmed = url.trim();
    if (!ALLOWED.test(trimmed)) return;
    try {
      await deps.open(trimmed);
    } catch {
      await deps.copy(trimmed);
      deps.notify("Link copied");
    }
  };
}

/**
 * Opens a link in the system browser. An <a target="_blank"> does nothing in
 * the Wails WebView, which is why agent links never opened. Server mode has
 * no native browser to hand it to, so the link is copied instead.
 */
export const openExternal = openExternalWith({
  open: (url) => Browser.OpenURL(url),
  copy: (text) => copyText(text),
  notify: (message) => void toast(message),
});
