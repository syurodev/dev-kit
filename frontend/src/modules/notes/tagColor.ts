import type { CSSProperties } from "react";

export function tagChipVar(name: string): string {
  const key = name.toLowerCase();
  let hash = 0;
  for (let i = 0; i < key.length; i++) {
    hash = (hash * 31 + key.charCodeAt(i)) >>> 0;
  }
  return `var(--note-tag-${hash % 8})`;
}

export function tagSoftBackground(name: string): CSSProperties {
  const accent = tagChipVar(name);
  return {
    backgroundColor: `color-mix(in oklch, ${accent} 15%, transparent)`,
    color: accent,
  };
}
