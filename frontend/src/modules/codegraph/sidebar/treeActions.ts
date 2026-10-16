import type { CodeGraphEntryPoint } from "@/bridge/codegraph";
import { treeDirId } from "../workspace/identity";
import { groupEntryPoints } from "./sidebarModel";

/** Include hidden levels too, so reopening a root does not reopen its children. */
export function entryCollapseKeys(folders: readonly { folderId: string; entries: CodeGraphEntryPoint[] }[]): string[] {
  return folders.flatMap(({ folderId, entries }) => [
    `folder:${folderId}`,
    ...groupEntryPoints(entries).flatMap((kind) => [
      `${folderId}:${kind.key}`,
      ...kind.services.flatMap((service) => [
        `${folderId}:${service.key}`,
        ...service.owners.map((owner) => `${folderId}:${owner.key}`),
      ]),
    ]),
  ]);
}

/** All prefixes include the endpoints of compact directory rows (src/main/java). */
export function fileAncestorKeys(folderId: string, path: string): string[] {
  const parts = path.split("/");
  return parts.slice(0, -1).map((_, i) => treeDirId(folderId, parts.slice(0, i + 1).join("/")));
}
