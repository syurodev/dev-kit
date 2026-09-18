import type { CodeGraphFileEntry } from "@/bridge/codegraph";

/**
 * The file list as a nested tree, for components/tree-view. The backend lists
 * files flat and path-sorted; nesting is by path. Pure, so the shape is
 * tested; the view dresses each node with icons, marks and handlers.
 */
export type FileBranch = {
  path: string;
  name: string;
  isDir: boolean;
  children: FileBranch[];
};

export function nestFiles(entries: CodeGraphFileEntry[]): FileBranch[] {
  const roots: FileBranch[] = [];
  const byPath = new Map<string, FileBranch>();
  for (const e of entries) {
    const cut = e.path.lastIndexOf("/");
    const node: FileBranch = { path: e.path, name: cut === -1 ? e.path : e.path.slice(cut + 1), isDir: e.isDir, children: [] };
    byPath.set(e.path, node);
    const parent = cut === -1 ? null : byPath.get(e.path.slice(0, cut));
    (parent ? parent.children : roots).push(node);
  }
  // Folders before files at every level, each by name — the order a file
  // tree is read in. The backend's path order interleaves them.
  const order = (list: FileBranch[]) => {
    list.sort((a, b) => Number(b.isDir) - Number(a.isDir) || a.name.localeCompare(b.name));
    for (const n of list) order(n.children);
  };
  order(roots);
  return roots;
}
