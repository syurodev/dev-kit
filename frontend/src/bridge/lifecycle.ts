import { Events } from "@wailsio/runtime";

/**
 * Sent by the backend when the user quits, a moment before the app goes (#50;
 * ShouldQuit in main.go). What is still inside a debounce window — the last
 * edits to a console, the last UI state — is written now instead of lost.
 */
export const BEFORE_QUIT_EVENT = "devkit:before-quit";

export function onBeforeQuit(flush: () => void): void {
  // Tests and SSR have no Wails runtime to listen on.
  if (typeof window === "undefined") return;
  Events.On(BEFORE_QUIT_EVENT, flush);
}
