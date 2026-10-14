import type { FileRef } from "./fileRef";

export type MenuItem<Id extends string> = { id: Id; label: string };
export type FileMenuId = "open-beside" | "open-here" | "copy-path" | "copy-path-line" | "reveal";
export type UrlMenuId = "open" | "copy";
export type SymbolMenuId = "definition" | "copy";
export type CommitMenuId = "open" | "copy";

/** A file reference's context menu; Copy path:line only when the reference names a line. */
export function fileMenuItems(ref: FileRef): MenuItem<FileMenuId>[] {
  const items: MenuItem<FileMenuId>[] = [
    { id: "open-beside", label: "Open beside" },
    { id: "open-here", label: "Open here" },
    { id: "copy-path", label: "Copy path" },
  ];
  if (ref.line !== undefined) items.push({ id: "copy-path-line", label: "Copy path:line" });
  items.push({ id: "reveal", label: "Reveal in Files" });
  return items;
}

/** A web link's context menu. The same two rows for every URL. */
export function urlMenuItems(url: string): MenuItem<UrlMenuId>[] {
  void url;
  return [
    { id: "open", label: "Open in browser" },
    { id: "copy", label: "Copy link" },
  ];
}

/** A symbol's context menu. */
export function symbolMenuItems(): MenuItem<SymbolMenuId>[] {
  return [
    { id: "definition", label: "Go to definition" },
    { id: "copy", label: "Copy name" },
  ];
}

/** A commit's context menu. Nothing about the commit is known before it is opened. */
export function commitMenuItems(): MenuItem<CommitMenuId>[] {
  return [
    { id: "open", label: "Open in Git" },
    { id: "copy", label: "Copy SHA" },
  ];
}

/**
 * The path Copy puts on the clipboard: the folder-relative one when the
 * reference has been resolved, else the path as the agent wrote it.
 */
export function copiedPath(ref: FileRef, rel: string | undefined, withLine: boolean): string {
  const path = rel ?? ref.path;
  return withLine && ref.line !== undefined ? `${path}:${ref.line}` : path;
}
