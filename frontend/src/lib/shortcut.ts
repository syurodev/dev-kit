/**
 * Shortcut labels, resolved per platform.
 *
 * Handlers should keep accepting either modifier (`e.metaKey || e.ctrlKey`) —
 * reading the event's actual state needs no platform guess. Labels cannot do
 * that: a button rendering ⌘ on Windows is simply wrong, and this app ships
 * there too.
 */
function isMac(): boolean {
  return typeof navigator !== "undefined" && /Mac|iPhone|iPad/i.test(navigator.platform);
}

/** The command/control glyph for the current platform. */
export function modKey(): string {
  return isMac() ? "⌘" : "Ctrl";
}

/**
 * A full shortcut label, e.g. shortcut("Enter") → "⌘↵" on macOS,
 * "Ctrl+Enter" elsewhere. Named keys become glyphs only where the platform
 * conventionally uses them.
 */
export function shortcut(key: string, options?: { shift?: boolean }): string {
  if (!isMac()) return `Ctrl+${options?.shift ? "Shift+" : ""}${key}`;
  const glyphs: Record<string, string> = {
    Enter: "↵",
    Backspace: "⌫",
    Escape: "⎋",
    Shift: "⇧",
    Alt: "⌥",
  };
  // ⇧ before ⌘: Apple orders modifiers Control, Option, Shift, Command, and
  // a label that writes them the other way round reads as a different
  // shortcut to anyone who knows the convention.
  return `${options?.shift ? "⇧" : ""}⌘${glyphs[key] ?? key}`;
}
