import type { ApiCollection, ApiRequest } from "@/bridge/apiclient";

/**
 * The rail's shape, without React: collections → folders → requests, with the
 * requests filed nowhere at the bottom. Kept pure so the order and the search
 * can be tested.
 */

export type SidebarFolder = {
  kind: "folder";
  collection: ApiCollection;
  children: SidebarEntry[];
  /** Requests anywhere below, for the count. */
  total: number;
};

export type SidebarLeaf = { kind: "request"; request: ApiRequest };

export type SidebarEntry = SidebarFolder | SidebarLeaf;

const MAX_DEPTH = 32;

function byOrderThenName(a: ApiCollection, b: ApiCollection): number {
  if (a.order !== b.order) return a.order - b.order;
  return a.name.localeCompare(b.name);
}

function requestMatches(request: ApiRequest, needle: string): boolean {
  return (
    request.name.toLowerCase().includes(needle) ||
    request.url.toLowerCase().includes(needle) ||
    request.method.toLowerCase() === needle
  );
}

/**
 * Builds the tree. With a needle, a folder is kept when its name matches (with
 * everything in it) or when something below it does (with only that).
 *
 * A folder whose parent is missing — deleted on another device, arriving
 * before its parent — is shown at the top level rather than lost.
 */
export function buildSidebarTree(
  collections: readonly ApiCollection[],
  requests: readonly ApiRequest[],
  needle = "",
): SidebarEntry[] {
  const query = needle.trim().toLowerCase();
  const ids = new Set(collections.map((c) => c.id));
  const childrenOf = new Map<string, ApiCollection[]>();
  for (const collection of collections) {
    const parent = collection.parentId && ids.has(collection.parentId) && collection.parentId !== collection.id
      ? collection.parentId
      : "";
    childrenOf.set(parent, [...(childrenOf.get(parent) ?? []), collection]);
  }
  const requestsIn = new Map<string, ApiRequest[]>();
  for (const request of requests) {
    const at = request.collectionId && ids.has(request.collectionId) ? request.collectionId : "";
    requestsIn.set(at, [...(requestsIn.get(at) ?? []), request]);
  }

  const seen = new Set<string>();
  function build(parent: string, depth: number, forced: boolean): SidebarEntry[] {
    if (depth > MAX_DEPTH) return [];
    const out: SidebarEntry[] = [];
    for (const collection of [...(childrenOf.get(parent) ?? [])].sort(byOrderThenName)) {
      if (seen.has(collection.id)) continue;
      seen.add(collection.id);
      const nameMatches = !!query && collection.name.toLowerCase().includes(query);
      const children = build(collection.id, depth + 1, forced || nameMatches);
      const total = children.reduce((sum, child) => sum + (child.kind === "folder" ? child.total : 1), 0);
      if (query && !forced && !nameMatches && children.length === 0) continue;
      out.push({ kind: "folder", collection, children, total });
    }
    const leaves = [...(requestsIn.get(parent) ?? [])]
      .filter((request) => !query || forced || requestMatches(request, query))
      .sort((a, b) => a.name.localeCompare(b.name));
    for (const request of leaves) out.push({ kind: "request", request });
    return out;
  }
  return build("", 0, false);
}

/** Folder ids from the root down to `id`, for expanding the way to a request. */
export function ancestorsOf(collections: readonly ApiCollection[], id: string): string[] {
  const byId = new Map(collections.map((c) => [c.id, c]));
  const out: string[] = [];
  const seen = new Set<string>();
  for (let at = id, depth = 0; at && depth < MAX_DEPTH && !seen.has(at); depth++) {
    seen.add(at);
    const collection = byId.get(at);
    if (!collection) break;
    out.unshift(collection.id);
    at = collection.parentId ?? "";
  }
  return out;
}

/** Every folder id below `id`, itself included — what a tree delete takes. */
export function descendantsOf(collections: readonly ApiCollection[], id: string): Set<string> {
  const out = new Set<string>([id]);
  let grew = true;
  while (grew) {
    grew = false;
    for (const collection of collections) {
      if (collection.parentId && out.has(collection.parentId) && !out.has(collection.id)) {
        out.add(collection.id);
        grew = true;
      }
    }
  }
  return out;
}
