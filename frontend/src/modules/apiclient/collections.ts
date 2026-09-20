import type { ApiCollection, ApiRequest } from "@/bridge/apiclient";

/**
 * Reading the folder tree into the labels a picker shows.
 *
 * The backend refuses a cycle, so one should never arrive — but this walks the
 * tree on every render and a walk that trusts its input is a hang rather than
 * a wrong label. Everything here terminates on its own.
 */

/** Deeper than this is not a folder structure, it is a loop. */
const MAX_DEPTH = 32;

/**
 * The path to a folder, outermost first: "GitHub / Repos".
 *
 * A parent that is missing — deleted on another device, arriving before its
 * child — stops the walk rather than dropping the folder: showing "Repos"
 * under no parent is right, and hiding it is not.
 */
export function folderPath(collections: readonly ApiCollection[], id: string): string {
  if (!id) return "";
  const byID = new Map(collections.map((collection) => [collection.id, collection]));
  const parts: string[] = [];
  const seen = new Set<string>();
  let at = id;
  for (let depth = 0; at && depth < MAX_DEPTH; depth++) {
    const collection = byID.get(at);
    if (!collection || seen.has(at)) break;
    seen.add(at);
    parts.unshift(collection.name);
    at = collection.parentId ?? "";
  }
  return parts.join(" / ");
}

/** A request's label in a flat picker: its folder path, then its name. */
export function requestLabel(
  collections: readonly ApiCollection[],
  request: Pick<ApiRequest, "name" | "collectionId">,
): string {
  const path = folderPath(collections, request.collectionId);
  return path ? `${path} / ${request.name}` : request.name;
}

/**
 * Requests in the order a picker should list them: by folder path, then by
 * name, so a folder's requests sit together and an unfiled one is not hidden
 * at the bottom under a blank heading.
 */
export function byFolderThenName<T extends Pick<ApiRequest, "name" | "collectionId">>(
  collections: readonly ApiCollection[],
  requests: readonly T[],
): T[] {
  return [...requests].sort((a, b) => {
    const pathA = folderPath(collections, a.collectionId);
    const pathB = folderPath(collections, b.collectionId);
    if (pathA !== pathB) return pathA.localeCompare(pathB);
    return a.name.localeCompare(b.name);
  });
}

/** Folders as a picker lists them: by full path, so children follow parents. */
export function foldersByPath(collections: readonly ApiCollection[]): ApiCollection[] {
  return [...collections].sort((a, b) =>
    folderPath(collections, a.id).localeCompare(folderPath(collections, b.id)),
  );
}
