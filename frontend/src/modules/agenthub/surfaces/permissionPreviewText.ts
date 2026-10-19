/**
 * How a permission request's text is shown, not what it says. The tool input a
 * person approves is the string the card was given; these functions only lay
 * it out for reading in the expanded view and never feed anything back.
 */

export type PermissionPreviewText = {
  /** The text as given: the folded box shows this, clipped by CSS only. */
  collapsed: string;
  /** What "Show full request" draws: indented JSON with real line breaks in strings. */
  expanded: string;
  /** What Copy puts on the clipboard: the whole text, valid JSON when it was JSON. */
  copy: string;
  json: boolean;
  /** More to read than the folded box shows, so the card offers the control. */
  long: boolean;
};

/** Longer than this (or with a line break) and the folded box cannot show it all. */
export const PREVIEW_FOLDED_CHARS = 120;

const INDENT = "  ";

/** A string value with its line breaks as real ones; every other character stays escaped as JSON does. */
function readableString(value: string): string {
  return `"${value
    .split("\n")
    .map((line) => JSON.stringify(line).slice(1, -1))
    .join("\n")}"`;
}

function render(value: unknown, depth: number): string {
  if (typeof value === "string") return readableString(value);
  if (value === null || typeof value !== "object") return JSON.stringify(value) ?? "null";
  const pad = INDENT.repeat(depth + 1);
  const close = INDENT.repeat(depth);
  if (Array.isArray(value)) {
    if (value.length === 0) return "[]";
    return `[\n${value.map((item) => pad + render(item, depth + 1)).join(",\n")}\n${close}]`;
  }
  const entries = Object.entries(value as Record<string, unknown>);
  if (entries.length === 0) return "{}";
  return `{\n${entries.map(([key, item]) => `${pad}${JSON.stringify(key)}: ${render(item, depth + 1)}`).join(",\n")}\n${close}}`;
}

export function previewPermissionText(raw: string): PermissionPreviewText {
  const trimmed = raw.trim();
  if (trimmed.startsWith("{") || trimmed.startsWith("[")) {
    try {
      const parsed: unknown = JSON.parse(trimmed);
      if (parsed !== null && typeof parsed === "object") {
        const breaks = /\\n/.test(trimmed);
        return {
          collapsed: raw,
          expanded: render(parsed, 0),
          copy: JSON.stringify(parsed, null, 2),
          json: true,
          long: raw.length > PREVIEW_FOLDED_CHARS || breaks,
        };
      }
    } catch {
      // Not JSON after all (a shell snippet that starts with a brace): plain text.
    }
  }
  return {
    collapsed: raw,
    expanded: raw,
    copy: raw,
    json: false,
    long: raw.length > PREVIEW_FOLDED_CHARS || raw.includes("\n"),
  };
}
