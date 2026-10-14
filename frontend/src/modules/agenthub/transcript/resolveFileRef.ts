import { useQuery } from "@tanstack/react-query";
import { agentHubListContextFiles } from "@/bridge/agenthub";
import { listWorkspaceServices } from "@/bridge/workspace";
import { statFolderFile } from "@/bridge/workspaceCode";
import type { FileRef } from "./fileRef";

export type RefContext = { sessionId: string; workspaceId: string; projectPath: string };
export type RefTarget = {
  workspaceId: string;
  folderId: string;
  folderName: string;
  rel: string;
  line?: number;
  unreadable?: "binary" | "too_large" | "not_regular";
};
export type RefResolution =
  | { kind: "found"; target: RefTarget }
  | { kind: "ambiguous"; targets: RefTarget[] }
  | { kind: "missing" }
  | { kind: "unsupported" };
export type ResolverDeps = {
  folders: (workspaceId: string) => Promise<{ id: string; name: string; path: string }[]>;
  stat: (workspaceId: string, folderId: string, rel: string) => Promise<{ exists: boolean; unreadable: string }>;
  byName: (sessionId: string, name: string) => Promise<{ path: string }[]>;
};

const MAX_AMBIGUOUS = 10;

/** Resolves `.` and `..` with `/` separators; `..` never climbs above the root. Browser code, so no node `path`. */
export function normalizePath(path: string): string {
  const absolute = path.startsWith("/");
  const out: string[] = [];
  for (const seg of path.split("/")) {
    if (seg === "" || seg === ".") continue;
    if (seg === "..") {
      if (out.length > 0 && out[out.length - 1] !== "..") out.pop();
      else if (!absolute) out.push("..");
      continue;
    }
    out.push(seg);
  }
  const joined = out.join("/");
  return absolute ? "/" + joined : joined;
}

export function joinPath(base: string, rel: string): string {
  return normalizePath(rel.startsWith("/") ? rel : `${base}/${rel}`);
}

type Folder = { id: string; name: string; path: string };

/** The folder holding `abs` (the deepest wins when folders nest) and the folder-relative path. */
function locate(folders: Folder[], abs: string): { folder: Folder; rel: string } | null {
  let best: { folder: Folder; rel: string } | null = null;
  let bestLen = -1;
  for (const folder of folders) {
    const root = normalizePath(folder.path);
    const prefix = root === "/" ? "/" : root + "/";
    if (!abs.startsWith(prefix) || abs.length === prefix.length) continue;
    if (root.length > bestLen) {
      best = { folder, rel: abs.slice(prefix.length) };
      bestLen = root.length;
    }
  }
  return best;
}

type Candidate = { folder: Folder; rel: string };

export async function resolveFileRef(ref: FileRef, ctx: RefContext, deps: ResolverDeps): Promise<RefResolution> {
  // A scratch session has no workspace to resolve against.
  if (!ctx.workspaceId) return { kind: "unsupported" };
  let folders: Folder[];
  try {
    folders = await deps.folders(ctx.workspaceId);
  } catch {
    return { kind: "missing" };
  }
  if (folders.length === 0) return { kind: "missing" };

  // A stat that rejects means the file is not there.
  const probe = async (c: Candidate): Promise<RefTarget | null> => {
    try {
      const st = await deps.stat(ctx.workspaceId, c.folder.id, c.rel);
      if (!st.exists) return null;
      const target: RefTarget = { workspaceId: ctx.workspaceId, folderId: c.folder.id, folderName: c.folder.name, rel: c.rel };
      if (ref.line !== undefined) target.line = ref.line;
      if (st.unreadable === "binary" || st.unreadable === "too_large" || st.unreadable === "not_regular") {
        target.unreadable = st.unreadable;
      }
      return target;
    } catch {
      return null;
    }
  };
  const probeAll = async (cands: Candidate[]): Promise<RefTarget[]> => {
    const seen = new Set<string>();
    const unique = cands.filter((c) => {
      const key = `${c.folder.id}\u0000${c.rel}`;
      if (seen.has(key)) return false;
      seen.add(key);
      return true;
    });
    const hits = await Promise.all(unique.map(probe));
    return hits.filter((h): h is RefTarget => h !== null);
  };
  const settle = (hits: RefTarget[]): RefResolution | null => {
    if (hits.length === 0) return null;
    if (hits.length === 1) return { kind: "found", target: hits[0] };
    const targets = [...hits]
      .sort((a, b) => a.folderName.localeCompare(b.folderName) || (a.rel < b.rel ? -1 : a.rel > b.rel ? 1 : 0))
      .slice(0, MAX_AMBIGUOUS);
    return { kind: "ambiguous", targets };
  };

  // 1. Absolute: the deepest folder containing it, or nothing.
  if (ref.path.startsWith("/")) {
    const at = locate(folders, normalizePath(ref.path));
    const hits = at ? await probeAll([at]) : [];
    return settle(hits) ?? { kind: "missing" };
  }

  // 2. Relative to the session's project folder (a leading ../ may land in a sibling folder).
  if (ctx.projectPath) {
    const at = locate(folders, joinPath(ctx.projectPath, ref.path));
    const hit = settle(at ? await probeAll([at]) : []);
    if (hit) return hit;
  }

  // 3. Relative to each folder root. A path that climbs out of its root names nothing here.
  const rootRel = normalizePath(ref.path);
  if (rootRel !== "" && !rootRel.startsWith("..")) {
    const hit = settle(await probeAll(folders.map((folder) => ({ folder, rel: rootRel }))));
    if (hit) return hit;
  }

  // 4. By name: the context-file search, kept to paths that end in the reference.
  const suffixPath = rootRel.replace(/^(\.\.\/)+/, "");
  if (suffixPath === "" || suffixPath.startsWith("..")) return { kind: "missing" };
  const base = suffixPath.slice(suffixPath.lastIndexOf("/") + 1);
  let files: { path: string }[];
  try {
    files = await deps.byName(ctx.sessionId, base);
  } catch {
    return { kind: "missing" };
  }
  const suffix = "/" + suffixPath;
  const cands: Candidate[] = [];
  for (const f of files) {
    const abs = normalizePath(f.path);
    if (!abs.endsWith(suffix)) continue;
    const at = locate(folders, abs);
    if (at) cands.push(at);
  }
  return settle(await probeAll(cands)) ?? { kind: "missing" };
}

export const liveResolverDeps: ResolverDeps = {
  folders: async (workspaceId) =>
    (await listWorkspaceServices(workspaceId)).map((s) => ({ id: s.id, name: s.name, path: s.path })),
  stat: statFolderFile,
  byName: (sessionId, name) => agentHubListContextFiles(sessionId, name),
};

export function useFileRefResolution(ref: FileRef | null, ctx: RefContext | null, enabled: boolean) {
  return useQuery({
    queryKey: ["agenthub", "fileRef", ctx?.sessionId ?? "", ref?.path ?? ""],
    queryFn: () => resolveFileRef(ref as FileRef, ctx as RefContext, liveResolverDeps),
    staleTime: 30_000,
    enabled: enabled && !!ref && !!ctx,
  });
}
