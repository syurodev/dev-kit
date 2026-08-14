export function tagChipVar(name: string): string {
  const key = name.toLowerCase();
  let hash = 0;
  for (let i = 0; i < key.length; i++) {
    hash = (hash * 31 + key.charCodeAt(i)) >>> 0;
  }
  return `var(--note-tag-${hash % 8})`;
}
