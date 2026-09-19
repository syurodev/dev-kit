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
  /**
   * What the row says. The name, unless compactFiles folded a chain of
   * folders into this row: then the chain, "src/main/java", or a package,
   * "com.acme.refunds".
   */
  label?: string;
  /** A folder inside a source root — a Java package, drawn as one. */
  isPackage?: boolean;
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

/** `src/main/java`, `src/test/kotlin`, … — where folders stop and packages start. */
const SOURCE_ROOT = /(^|\/)src\/[^/]+\/(java|kotlin|groovy|scala)$/;

export function isSourceRoot(path: string): boolean {
  return SOURCE_ROOT.test(path);
}

/** Whether `path` is a folder below a source root: a package. */
export function isPackagePath(path: string): boolean {
  const parts = path.split("/");
  for (let i = parts.length - 1; i > 0; i -= 1) {
    if (SOURCE_ROOT.test(parts.slice(0, i).join("/"))) return true;
  }
  return false;
}

/**
 * Folds every chain of folders that hold nothing but one folder into one row,
 * as IntelliJ and VS Code's compact folders do: a Maven module is otherwise
 * eight clicks deep (gateway › src › main › java › com › synx › devkit ›
 * gateway) before the first class.
 *
 * The chain breaks at the source root, so the root keeps a row of its own —
 * "src/main/java" or "src/test/java" is worth knowing — and below it the
 * folders are packages, joined with dots. The folded row takes the deepest
 * folder's path, so expanding it and a search opening it act on the folder
 * whose contents it shows.
 */
export function compactFiles(tree: FileBranch[]): FileBranch[] {
  return tree.map((node) => {
    if (!node.isDir) return node;
    let cur = node;
    let label = node.name;
    while (cur.children.length === 1 && cur.children[0].isDir && !SOURCE_ROOT.test(cur.path)) {
      const next = cur.children[0];
      label += (isPackagePath(cur.path) ? "." : "/") + next.name;
      cur = next;
    }
    return { ...cur, label, isPackage: isPackagePath(cur.path), children: compactFiles(cur.children) };
  });
}
