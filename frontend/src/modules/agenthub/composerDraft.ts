import type { AgentImageAttachment } from "@/bridge/agenthub";
import { decodeEntityRefUri, encodeEntityRef } from "@/workbench/entityRef";
import type { EntityRef } from "@/workbench/intents";

/**
 * What a composer draft holds besides words: files, images and references,
 * each a chip in the text (docs/superpowers/specs/2026-10-19-composer-
 * attachments-design.md). The draft stays one string — the Companion,
 * `agent.attach`, slash commands and the failed-send restore all read and write
 * it as text — and a chip is written into it as a token of private-use
 * characters:
 *
 *     U+E000  kind ":" enc(key) "|" enc(label)  U+E001
 *
 * `enc` is encodeURIComponent, so neither the delimiters nor the punctuation of
 * a path can occur inside. Nobody types or pastes private-use characters, and
 * `scrubText` replaces any that do appear in text, so a token can only have
 * come from a chip. The agent never sees a token: `parseDraft` turns each into
 * `@label`, exactly what an "@" mention put in the text before chips replaced
 * the strip.
 */

export type ChipKind = "file" | "image" | "ref";

/** `key` is what the chip stands for: a path, an image id, or a devkit:// ref URI. */
export type DraftChip = { kind: ChipKind; key: string; label: string };

export type DraftPart = { type: "text"; text: string } | { type: "chip"; chip: DraftChip };

const OPEN = "";
const CLOSE = "";
const TOKEN = /(file|image|ref):([^|]*)\|([^]*)/g;

export function chipToken(chip: DraftChip): string {
  return `${OPEN}${chip.kind}:${encodeURIComponent(chip.key)}|${encodeURIComponent(chip.label)}${CLOSE}`;
}

/** Typed or pasted text, with the token delimiters made harmless. */
export function scrubText(text: string): string {
  return text.replace(/[]/g, "�");
}

function decode(part: string): string {
  try {
    return decodeURIComponent(part);
  } catch {
    return part;
  }
}

/** The last path segment, whichever separator the platform used. */
export function baseName(path: string): string {
  return path.slice(Math.max(path.lastIndexOf("/"), path.lastIndexOf("\\")) + 1) || path;
}

export function fileChip(path: string, name?: string): DraftChip {
  return { kind: "file", key: path, label: name || baseName(path) };
}

export function imageChip(image: Pick<AgentImageAttachment, "id" | "name">): DraftChip {
  return { kind: "image", key: image.id, label: image.name };
}

export function refChip(ref: EntityRef): DraftChip {
  return { kind: "ref", key: encodeEntityRef(ref), label: ref.label || ref.id };
}

/** The draft as its text and its chips, in order. */
export function splitDraft(draft: string): DraftPart[] {
  const parts: DraftPart[] = [];
  let from = 0;
  for (const match of draft.matchAll(TOKEN)) {
    if (match.index > from) parts.push({ type: "text", text: draft.slice(from, match.index) });
    parts.push({ type: "chip", chip: { kind: match[1] as ChipKind, key: decode(match[2]), label: decode(match[3]) } });
    from = match.index + match[0].length;
  }
  if (from < draft.length) parts.push({ type: "text", text: draft.slice(from) });
  return parts;
}

/** `text` is what the agent reads; `chips` are what is attached, in order, repeats included. */
export function parseDraft(draft: string): { text: string; chips: DraftChip[] } {
  const chips: DraftChip[] = [];
  let text = "";
  for (const part of splitDraft(draft)) {
    if (part.type === "text") {
      text += part.text;
    } else {
      chips.push(part.chip);
      text += `@${part.chip.label}`;
    }
  }
  return { text, chips };
}

/** Words, or anything attached: "look at these" is a turn. */
export function draftHasContent(draft: string): boolean {
  const { text, chips } = parseDraft(draft);
  return chips.length > 0 || text.trim() !== "";
}

export type DraftTurn = {
  text: string;
  attachments: string[];
  images: AgentImageAttachment[];
  refs: EntityRef[];
};

/**
 * What a turn carries, read off the draft: the words, then each file, image
 * and reference once, in the order the chips stand. An image whose bytes are no
 * longer in the registry cannot be sent and is left out; the rest still go.
 */
export function turnFromDraft(draft: string, registry: Readonly<Record<string, AgentImageAttachment>>): DraftTurn {
  const { text, chips } = parseDraft(draft);
  const attachments: string[] = [];
  const images: AgentImageAttachment[] = [];
  const refs: EntityRef[] = [];
  const seen = new Set<string>();
  for (const chip of chips) {
    const id = `${chip.kind}:${chip.key}`;
    if (seen.has(id)) continue;
    seen.add(id);
    if (chip.kind === "file") {
      attachments.push(chip.key);
    } else if (chip.kind === "image") {
      const image = registry[chip.key];
      if (image) images.push(image);
    } else {
      const ref = decodeEntityRefUri(chip.key);
      if (ref) refs.push(ref);
    }
  }
  return { text: text.trim(), attachments, images, refs };
}

/**
 * The draft with `chips` after its text, for a caller that has no editor to
 * insert at a caret — a reference another module hands over while no composer
 * is mounted. A space before unless the text already ends in whitespace, and
 * one after, so the next word is not glued to the last name.
 */
export function appendChips(draft: string, chips: readonly DraftChip[]): string {
  if (chips.length === 0) return draft;
  const lead = draft && !/\s$/.test(draft) ? " " : "";
  return `${draft}${lead}${chips.map(chipToken).join(" ")} `;
}
