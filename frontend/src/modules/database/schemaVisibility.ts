import type { SchemaNamespace } from "@/bridge/database";

/** null/undefined follows all current and newly discovered schemas; [] hides all. */
export function visibleNamespace(namespace: SchemaNamespace, selected: readonly string[] | null | undefined): SchemaNamespace {
  if (selected == null) return namespace;
  const wanted = new Set(selected);
  return Object.fromEntries(Object.entries(namespace).filter(([name]) => wanted.has(name)));
}
export function toggleVisibleSchema(names: readonly string[], selected: readonly string[] | null | undefined, name: string, checked: boolean): string[] {
  const wanted = new Set(selected ?? names);
  if (checked) wanted.add(name); else wanted.delete(name);
  return names.filter((value) => wanted.has(value));
}
