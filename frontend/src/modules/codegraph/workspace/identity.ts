/**
 * Who owns a document, a flow or an entry point in a workspace.
 *
 * Three things are kept apart on purpose (docs/design/codegraph-workspace.md § 2):
 *
 *  - the stable identity — `workspaceId`, `folderId` (the registry row's id) and
 *    a normalized relative path — which is what keys buffers, tabs and caches;
 *  - the display name — a folder's label, which may repeat or change;
 *  - the absolute path — which only Go resolves, from membership, to do I/O.
 *
 * Nothing here knows which folder is the primary. A document is not "the file at
 * src/config.ts": it is that file in that folder of that workspace.
 */

export type FolderRef = {
  /** workspace_services.id: stable for as long as the folder is in the workspace. */
  id: string;
  /** Display only; not unique. */
  name: string;
  /** Absolute path, for tooltips and for naming the owner to read-only graph queries. */
  path: string;
};

/**
 * A relative path as the index and the tree spell it: "/"-separated, no leading
 * "./" or "/", no empty or "." segments. Case is kept exactly as written — the
 * filesystem decides whether `A.ts` and `a.ts` are one file, not this function.
 * A path that climbs out of its folder ("..") is not a path of the folder and
 * normalizes to "".
 */
export function normalizeRelPath(path: string): string {
  const out: string[] = [];
  for (const segment of path.split(/[\\/]+/)) {
    if (segment === "" || segment === ".") continue;
    if (segment === "..") return "";
    out.push(segment);
  }
  return out.join("/");
}

/** The key a document is known by in every store and in the watcher's events. */
export function docKey(workspaceId: string, folderId: string, path: string): string {
  return `${workspaceId}:${folderId}:${normalizeRelPath(path)}`;
}

export type DocIdentity = { workspaceId: string; folderId: string; path: string };

/**
 * Splits a docKey. The ids are 24 hex characters and never contain ":", so the
 * first two separators are the ids' and everything after is the path (which may
 * contain ":" itself).
 */
export function parseDocKey(key: string): DocIdentity | null {
  const first = key.indexOf(":");
  const second = first < 0 ? -1 : key.indexOf(":", first + 1);
  if (first <= 0 || second <= first + 1) return null;
  return { workspaceId: key.slice(0, first), folderId: key.slice(first + 1, second), path: key.slice(second + 1) };
}

/** A flow is keyed by its owner folder as well as its root node. */
export function flowKey(folderId: string, rootNodeId: string): string {
  return `${folderId}\u0000${rootNodeId}`;
}

/** An entry point is its folder's, whatever id the analyzer gave it. */
export function entryKey(folderId: string, entryPointId: string): string {
  return `${folderId}\u0000${entryPointId}`;
}

/** A tree node id for a file: unique across folders though paths repeat. */
export function treeFileId(folderId: string, path: string): string {
  return `${folderId}\u0000f\u0000${path}`;
}

/** A tree node id for a directory, for expanded state that is independent per root. */
export function treeDirId(folderId: string, path: string): string {
  return `${folderId}\u0000d\u0000${path}`;
}

export function baseName(path: string): string {
  return path.slice(path.lastIndexOf("/") + 1);
}

export function dirName(path: string): string {
  const at = path.lastIndexOf("/");
  return at < 0 ? "" : path.slice(0, at);
}

/** The last segment of an absolute path, in either separator style. */
export function folderBaseName(path: string): string {
  const clean = path.replace(/[\\/]+$/, "");
  return clean.slice(Math.max(clean.lastIndexOf("/"), clean.lastIndexOf("\\")) + 1) || clean;
}

/**
 * Labels for a workspace's folders, unique when they can be.
 *
 * Two folders called `api` (a fork, a v1 and a v2) must not read as one. Each
 * duplicate gains its parent directory — `api · services` — and a duplicate that
 * is still a duplicate keeps the whole path's tail until it is not. A folder
 * whose name is unique keeps it.
 */
export function folderLabels(folders: readonly FolderRef[]): Record<string, string> {
  const labels: Record<string, string> = {};
  const parts = new Map<string, string[]>();
  for (const f of folders) parts.set(f.id, f.path.split(/[\\/]+/).filter(Boolean));
  const depth = new Map<string, number>(folders.map((f) => [f.id, 1]));
  const label = (f: FolderRef) => {
    const segs = parts.get(f.id) ?? [];
    const d = Math.min(depth.get(f.id) ?? 1, Math.max(1, segs.length));
    const tail = segs.slice(-d);
    return d === 1 ? f.name || (tail[0] ?? f.path) : `${f.name || tail[tail.length - 1]} · ${tail.slice(0, -1).join("/")}`;
  };
  for (let round = 0; round < 8; round++) {
    const seen = new Map<string, string[]>();
    for (const f of folders) {
      const l = label(f);
      seen.set(l, [...(seen.get(l) ?? []), f.id]);
    }
    let changed = false;
    for (const ids of seen.values()) {
      if (ids.length < 2) continue;
      for (const id of ids) {
        const segs = parts.get(id) ?? [];
        if ((depth.get(id) ?? 1) < segs.length) {
          depth.set(id, (depth.get(id) ?? 1) + 1);
          changed = true;
        }
      }
    }
    if (!changed) break;
  }
  for (const f of folders) labels[f.id] = label(f);
  return labels;
}

/**
 * The order folders are listed in. It is by name and then path — never by
 * which one is primary, because the primary can change while someone is
 * working and the Explorer must not rearrange under them when it does. (The
 * workspace service lists the primary first; that order is for agents.)
 */
export function displayOrder<T extends { name: string; path: string }>(folders: readonly T[]): T[] {
  const key = (f: T) => (f.name || folderBaseName(f.path)).toLocaleLowerCase();
  return [...folders].sort((a, b) => key(a).localeCompare(key(b)) || (a.path < b.path ? -1 : a.path > b.path ? 1 : 0));
}
