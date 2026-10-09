import type { FolderRef } from "./identity";

/**
 * The part of the workspace a surface looks at.
 *
 * `all` follows the workspace: a folder added later is in scope, one removed is
 * gone. `folders` is a named subset. A scope is never "the primary" — the
 * primary is a fact about where agents start, not about what is being read.
 */
export type Scope = { kind: "all" } | { kind: "folders"; ids: string[] };

export const WHOLE_WORKSPACE: Scope = { kind: "all" };

/** The folders a scope covers, in the workspace's own order. */
export function scopeFolders<T extends Pick<FolderRef, "id">>(scope: Scope, folders: readonly T[]): T[] {
  if (scope.kind === "all") return [...folders];
  const wanted = new Set(scope.ids);
  return folders.filter((f) => wanted.has(f.id));
}

export function scopeFolderIds(scope: Scope, folders: readonly Pick<FolderRef, "id">[]): string[] {
  return scopeFolders(scope, folders).map((f) => f.id);
}

/**
 * Drops folders that left the workspace. A subset that has lost every member
 * falls back to the whole workspace rather than to an empty view that looks like
 * a broken project; one that still names every folder is just the workspace.
 */
export function normalizeScope(scope: Scope, folders: readonly Pick<FolderRef, "id">[]): Scope {
  if (scope.kind === "all") return scope;
  const present = new Set(folders.map((f) => f.id));
  const ids = scope.ids.filter((id) => present.has(id));
  if (ids.length === 0 || ids.length === folders.length) return WHOLE_WORKSPACE;
  return ids.length === scope.ids.length ? scope : { kind: "folders", ids };
}

/** Toggles one folder in a scope, normalizing the result. */
export function toggleScopeFolder(scope: Scope, folderId: string, folders: readonly Pick<FolderRef, "id">[]): Scope {
  const current = new Set(scopeFolderIds(scope, folders));
  if (current.has(folderId)) current.delete(folderId);
  else current.add(folderId);
  return normalizeScope({ kind: "folders", ids: folders.filter((f) => current.has(f.id)).map((f) => f.id) }, folders);
}

export function scopesEqual(a: Scope, b: Scope): boolean {
  if (a.kind !== b.kind) return false;
  if (a.kind === "all" || b.kind === "all") return true;
  return a.ids.length === b.ids.length && a.ids.every((id, i) => id === b.ids[i]);
}

/**
 * How the scope reads under the workspace's name and in a search form:
 * "Whole workspace · 4 folders", "Viewing: api", "Viewing: api, web +1".
 */
export function scopeSummary(scope: Scope, folders: readonly FolderRef[], labels: Record<string, string>): string {
  const total = folders.length;
  const noun = (n: number) => `${n} folder${n === 1 ? "" : "s"}`;
  if (scope.kind === "all") return `Whole workspace · ${noun(total)}`;
  const covered = scopeFolders(scope, folders);
  if (covered.length === 0) return `Whole workspace · ${noun(total)}`;
  const names = covered.map((f) => labels[f.id] ?? f.name);
  if (names.length <= 2) return `Viewing: ${names.join(", ")}`;
  return `Viewing: ${names.slice(0, 2).join(", ")} +${names.length - 2}`;
}
