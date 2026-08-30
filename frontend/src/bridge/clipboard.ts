import { Clipboard } from "@wailsio/runtime";

/**
 * The webview's own navigator.clipboard is unreliable inside WKWebView, and
 * silently so. The runtime's Clipboard goes through the host instead.
 */
export function copyText(text: string): Promise<void> {
  return Clipboard.SetText(text);
}
