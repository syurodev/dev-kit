type KeyLike = {
  key: string;
  metaKey: boolean;
  ctrlKey: boolean;
  altKey: boolean;
  shiftKey: boolean;
};

/**
 * Maps ⌘1–⌘9 (or Ctrl on non-mac) to a zero-based module index. Accepts
 * either modifier rather than branching on navigator.platform, which is
 * unreliable inside Wails' WKWebView — the same reasoning Workbench.tsx's
 * existing ⌘⇧E handler documents.
 */
export function moduleIndexFromEvent(event: KeyLike): number | null {
  if (!(event.metaKey || event.ctrlKey)) return null;
  if (event.altKey || event.shiftKey) return null;
  if (!/^[1-9]$/.test(event.key)) return null;
  return Number(event.key) - 1;
}

/**
 * Whether a key event asks for the command palette.
 *
 * ⌘K always. Ctrl+K only when the focus is not somewhere text is edited:
 * there it already means "kill to end of line" — in every macOS text field, in
 * CodeMirror's emacs bindings, in a shell — and the platform cannot be asked
 * which of the two this machine means (see moduleIndexFromEvent). Someone on
 * Windows or Linux with the caret in an editor gets no palette from Ctrl+K,
 * which is the cheaper mistake: the titlebar button is still there, and
 * nothing they typed was eaten.
 */
export function isPaletteShortcut(event: KeyLike, focusEditsText: boolean): boolean {
  if (event.altKey || event.shiftKey) return false;
  if (event.key.toLowerCase() !== "k") return false;
  if (event.metaKey) return !event.ctrlKey;
  return event.ctrlKey && !focusEditsText;
}

/** True for an element where keys are text: a field, an editor, a terminal. */
export function editsText(target: EventTarget | null): boolean {
  if (!(target instanceof Element)) return false;
  return target.closest("input, textarea, select, [contenteditable]:not([contenteditable='false']), .xterm") !== null;
}
