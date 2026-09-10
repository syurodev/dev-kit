import {
  Binary,
  Braces,
  Fingerprint,
  Hash,
  Link2,
  Link2Off,
  type LucideIcon,
} from "lucide-react";

/** The hash algorithms internal/tools.HashService accepts. */
export const HASH_ALGORITHMS = ["sha256", "sha512", "md5"] as const;
export type HashAlgorithm = (typeof HASH_ALGORITHMS)[number];
export const HASH_LABELS: Record<HashAlgorithm, string> = {
  sha256: "SHA-256",
  sha512: "SHA-512",
  md5: "MD5",
};

/** The UUID layouts internal/tools.UtilService accepts. */
export const UUID_VERSIONS = ["v4", "v7"] as const;
export type UUIDVersion = (typeof UUID_VERSIONS)[number];
export const UUID_LABELS: Record<UUIDVersion, string> = {
  v4: "v4 · random",
  v7: "v7 · time-ordered",
};

/**
 * What kind of controls a tool needs, which is the only thing the pane varies.
 *
 * Three shapes cover all seven: text in and text out; text in with a choice of
 * algorithm; and one with no input at all. Anything the pane needs to know
 * beyond this belongs to the tool, not to the pane.
 */
export type ToolShape = "text" | "text-with-algorithm" | "generator";

export type ToolDescriptor = {
  /** Matches the id in internal/tools/commands.go exactly. */
  id: string;
  name: string;
  icon: LucideIcon;
  shape: ToolShape;
  /** One line under the title: what it does, not how to use it. */
  blurb: string;
  inputPlaceholder?: string;
};

/**
 * The rail and the pane read the same list, in this order.
 *
 * Icons are not decoration here. The rail is a flat list of seven short labels
 * that read alike — "URL encode" and "URL decode" differ by one word — and a
 * glyph is what the eye finds before it has read anything. Encode and decode
 * pairs deliberately take mirrored icons so the direction is visible rather
 * than spelled.
 */
export const TOOLS: ToolDescriptor[] = [
  {
    id: "tools.hash",
    name: "Hash",
    icon: Hash,
    shape: "text-with-algorithm",
    blurb: "A fingerprint of the text. The same input always gives the same digest.",
    inputPlaceholder: "text to hash",
  },
  {
    id: "tools.base64-encode",
    name: "Base64 encode",
    icon: Binary,
    shape: "text",
    blurb: "Text to standard padded base64.",
    inputPlaceholder: "text to encode",
  },
  {
    id: "tools.base64-decode",
    name: "Base64 decode",
    icon: Binary,
    shape: "text",
    blurb: "Base64 back to text. Standard or URL-safe, padded or not.",
    inputPlaceholder: "base64 to decode",
  },
  {
    id: "tools.url-encode",
    name: "URL encode",
    icon: Link2,
    shape: "text",
    blurb: "Escape text for a query-string value. A space becomes “+”.",
    inputPlaceholder: "text to escape",
  },
  {
    id: "tools.url-decode",
    name: "URL decode",
    icon: Link2Off,
    shape: "text",
    blurb: "Percent-escapes back to text.",
    inputPlaceholder: "text to unescape",
  },
  {
    id: "tools.json-format",
    name: "Format JSON",
    icon: Braces,
    shape: "text",
    blurb: "Indent a document without reordering its keys or rewriting its numbers.",
    inputPlaceholder: '{"compact":true}',
  },
  {
    id: "tools.uuid-generate",
    name: "Generate UUID",
    icon: Fingerprint,
    shape: "generator",
    blurb: "One identifier. v7 leads with a timestamp, so a column of them sorts by creation order.",
  },
];

export function toolById(id: string): ToolDescriptor | undefined {
  return TOOLS.find((tool) => tool.id === id);
}
