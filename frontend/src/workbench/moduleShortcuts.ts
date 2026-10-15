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

/**
 * ⌘⇧T / Ctrl⇧T: reopen the tab closed last (browsers, VS Code) — from a text
 * field too, as there. Not while a dialog holds the keyboard (the command
 * palette is one): a tab opening behind it would be out of sight, and the
 * dialog is what the keys are for.
 */
export function isReopenTabShortcut(event: KeyLike, dialogOpen: boolean): boolean {
  if (!(event.metaKey || event.ctrlKey) || !event.shiftKey || event.altKey) return false;
  return event.key.toLowerCase() === "t" && !dialogOpen;
}

/** True for an element where keys are text: a field, an editor, a terminal. */
export function editsText(target: EventTarget | null): boolean {
  if (!(target instanceof Element)) return false;
  return target.closest("input, textarea, select, [contenteditable]:not([contenteditable='false']), .xterm") !== null;
}

type PeekKeyLike = KeyLike & {
  /** The physical key. Option changes what `key` says on macOS (⌥1 is "¡"); this does not. */
  code: string;
  getModifierState?: (key: string) => boolean;
};

/**
 * Maps ⌘⌥1–⌘⌥9 (Ctrl+Alt+1–9 elsewhere) to a zero-based switcher slot: the
 * chord that opens a slot's peek — a module's open tabs, a group's members —
 * without the pointer.
 *
 * Read from `code`, not `key`: with ⌥ held macOS reports "¡™£" for the digit
 * row. Accepts either ⌘ or Ctrl, as moduleIndexFromEvent does.
 *
 * Two places it must not fire. On Windows and Linux Ctrl+Alt *is* AltGr, which
 * types characters on many layouts, so with the caret in a text field — or when
 * the platform says AltGraph is down — it stays out of the way, as Ctrl+K does.
 * And the Notes editor binds ⌘⌥1–⌘⌥6 to headings; a `richText` focus keeps them.
 */
export function peekIndexFromEvent(
  event: PeekKeyLike,
  focus: { editsText: boolean; richText: boolean },
): number | null {
  if (!event.altKey || event.shiftKey || !(event.metaKey || event.ctrlKey)) return null;
  if (focus.richText) return null;
  if (!event.metaKey && (focus.editsText || event.getModifierState?.("AltGraph"))) return null;
  const digit = /^(?:Digit|Numpad)([1-9])$/.exec(event.code);
  return digit ? Number(digit[1]) - 1 : null;
}

/** True where ⌘⌥1–⌘⌥6 mean headings: inside the Notes editor. */
export function inRichTextEditor(target: EventTarget | null): boolean {
  return target instanceof Element && target.closest("[data-slate-editor]") !== null;
}
