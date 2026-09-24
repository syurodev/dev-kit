import type { WorkspaceService } from "@/bridge/workspace";

/**
 * One row of the Create / Edit workspace dialog's "Source folders" list: a
 * registered service, or a folder just picked that Save will discover.
 */
export type FolderRow = {
  path: string;
  /** The service's name, or the folder's own name for a row not saved yet. */
  name: string;
};

/** The last path segment, whichever separator the OS used. */
export function folderName(path: string): string {
  const trimmed = path.replace(/[\\/]+$/, "");
  const cut = Math.max(trimmed.lastIndexOf("/"), trimmed.lastIndexOf("\\"));
  return cut >= 0 ? trimmed.slice(cut + 1) || trimmed : trimmed;
}

/**
 * The saved services as rows, primary first: the one at the workspace's
 * root, then the rest in the order Go listed them (by name).
 */
export function rowsFromServices(services: WorkspaceService[], root: string): FolderRow[] {
  const rows = services.map((svc) => ({ path: svc.path, name: svc.name }));
  const primary = rows.findIndex((row) => row.path === root);
  if (primary > 0) rows.unshift(...rows.splice(primary, 1));
  return rows;
}

/** A picked folder appended, unless it is already listed. */
export function addFolder(rows: FolderRow[], path: string): FolderRow[] {
  if (!path || rows.some((row) => row.path === path)) return rows;
  return [...rows, { path, name: folderName(path) }];
}

export function removeFolder(rows: FolderRow[], path: string): FolderRow[] {
  return rows.filter((row) => row.path !== path);
}

/** The picked row becomes the primary — moved to the top of the list. */
export function makePrimary(rows: FolderRow[], path: string): FolderRow[] {
  const index = rows.findIndex((row) => row.path === path);
  if (index <= 0) return rows;
  const next = [...rows];
  next.unshift(...next.splice(index, 1));
  return next;
}
