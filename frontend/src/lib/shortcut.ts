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

/**
 * A key chord as data, so one description can be drawn per platform: `mod` is
 * ⌘ on macOS and Ctrl elsewhere, the same either-modifier rule the handlers
 * follow; `alt` is ⌥ / Alt; `key` is the key itself ("1", "K", "Enter", ",").
 */
export type Chord = { mod?: boolean; alt?: boolean; shift?: boolean; key: string };

const MAC_KEYS: Record<string, string> = { Enter: "↵", Escape: "esc", Backspace: "⌫", Tab: "⇥" };

/**
 * The chord as it is written on this platform: "⌘⌥1" on macOS, "Ctrl+Alt+1"
 * elsewhere. Modifiers read ⌘ first, then ⌥, then ⇧ — the order Figma's
 * Keyboard settings draws them in — and the letter is always upper case.
 * `mac` is a parameter so the other platform can be drawn and tested.
 */
export function chordLabel(chord: Chord, mac: boolean = isMac()): string {
  const key = chord.key.length === 1 ? chord.key.toUpperCase() : chord.key;
  if (!mac) {
    return [chord.mod ? "Ctrl" : "", chord.alt ? "Alt" : "", chord.shift ? "Shift" : "", key]
      .filter(Boolean)
      .join("+");
  }
  return `${chord.mod ? "⌘" : ""}${chord.alt ? "⌥" : ""}${chord.shift ? "⇧" : ""}${MAC_KEYS[key] ?? key}`;
}

const GLYPH_KEYS: Record<string, string> = { "↵": "Enter", "⎋": "Escape", "⌫": "Backspace", "⇥": "Tab" };

/**
 * A shortcut written with macOS glyphs — "⇧⌘N", as module manifests declare
 * them for the menu bar and the palette — as a chord. Null when it names no key
 * or a modifier this app has no chord for (⌃).
 */
export function parseGlyphShortcut(label: string): Chord | null {
  const chord: Chord = { key: "" };
  for (const glyph of Array.from(label)) {
    if (glyph === "⌘") chord.mod = true;
    else if (glyph === "⌥") chord.alt = true;
    else if (glyph === "⇧") chord.shift = true;
    else if (glyph === "⌃") return null;
    else if (chord.key === "") chord.key = GLYPH_KEYS[glyph] ?? glyph;
    else return null;
  }
  return chord.key === "" ? null : chord;
}
