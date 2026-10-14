import {
  codeGraphAnalyzerStatus,
  codeGraphProjectRoot,
  getCodeGraphBlastRadius,
} from "@/bridge/codegraph";
import { useAgentSessionsStore } from "@/stores/agentSessions";
import { openNewAgentSession, showAgentSession } from "@/modules/agenthub/documents";
import type { CodeGraphEntryPoint } from "@/bridge/codegraph";
import type { Placement } from "@/workbench/intents";
import { set, snapshot, folderById } from "./storeAccess";
import { openSearch } from "./searchActions";
import { WHOLE_WORKSPACE } from "./workspace/scope";
import { MODULE, type ActiveDoc } from "./documents";
import { wireFileEvents, openFile } from "./fileActions";
import { wireWorkspaceEvents } from "./workspaceSync";
import { docKey } from "./workspace/identity";
import { Within } from "./workspace/paths";
import type { RevealFileArgs } from "./revealFile";

/**
 * The entry point of Code Graph's actions: what starts the module, and the
 * few things that cross its parts. The parts themselves live beside it —
 * workspaceSync (folders, scope, index), fileActions (buffers, navigation),
 * flowActions, planActions, searchActions — each over the one store
 * (codegraphStore.ts), none of them with a "current project".
 */
export * from "./fileActions";
export * from "./flowActions";
export * from "./planActions";
export * from "./searchActions";
export * from "./workspaceSync";

/**
 * Refreshes which analyzers are built.
 *
 * Called on mount and after a build, never on a timer: it probes the machine
 * for a JDK and Maven, which costs a subprocess apiece. A probe that fails
 * leaves the statuses null, which reads as "assume fine" — a broken status call
 * must not hide a working module behind a setup panel.
 */
export async function loadAnalyzerStatus() {
  try {
    set({ analyzerStatuses: await codeGraphAnalyzerStatus() });
  } catch {
    set({ analyzerStatuses: null });
  }
}

/**
 * The folder a tab saved before documents named their owner meant: the one
 * that was the open project when it was saved — which Go still remembers. It
 * is used only to read such a tab; nothing new is ever opened without an owner.
 */
let legacyRoot = "";

/** Who a document belongs to. Documents opened now name it; an old tab is resolved once. */
export function resolveOwner(doc: Pick<ActiveDoc, "workspaceId" | "folderId">): { workspaceId: string; folderId: string } | null {
  if (doc.workspaceId && doc.folderId) return { workspaceId: doc.workspaceId, folderId: doc.folderId };
  const s = snapshot();
  if (!s.workspaceId || s.folders.length === 0) return null;
  const legacy = s.folders.find((f) => legacyRoot !== "" && f.path === legacyRoot) ?? (s.folders.length === 1 ? s.folders[0] : undefined);
  return legacy ? { workspaceId: s.workspaceId, folderId: legacy.id } : null;
}

export const getLegacyRoot = () => legacyRoot;

let started = false;

/**
 * Wires the module up once, from whichever of its surfaces mounts first — the
 * rail, the home view, a restored flow or file tab. None of them owns the
 * workspace, so none of them can be the one place this lives.
 *
 * The subscriptions are for the app's lifetime, like the store they write to.
 */
export function ensureStarted() {
  if (started) return;
  started = true;
  wireWorkspaceEvents();
  wireFileEvents();
  // Probe once. Cheap enough, and the answer cannot change without someone
  // installing a JDK, which is not something to poll for.
  void loadAnalyzerStatus();
  codeGraphProjectRoot()
    .then((root) => {
      legacyRoot = root;
      // Tabs restored from before documents named their owner can resolve now.
      set({ foldersStatus: snapshot().foldersStatus });
    })
    .catch(() => {});
}

/**
 * Opens a file an agent (or another module) pointed at, by absolute path, in
 * the workspace folder that contains it. `root` is the project the pointer was
 * made in; the folder is whichever one of the workspace holds the file — the
 * deepest, when folders nest. A path in no folder of the workspace opens nothing
 * rather than guessing.
 */
export async function openFileInProject(root: string, path: string, line?: number, placement: Placement = "replace"): Promise<boolean> {
  const folders = snapshot().folders;
  const normalized = path.split("\\").join("/");
  const absolute = normalized.startsWith("/") || /^[A-Za-z]:\//.test(normalized);
  const candidate = absolute ? normalized : `${root.replace(/[\\/]+$/, "").split("\\").join("/")}/${normalized}`;
  let best: { folder: (typeof folders)[number]; rel: string } | null = null;
  for (const folder of folders) {
    const base = folder.path.replace(/[\\/]+$/, "").split("\\").join("/");
    if (!Within(candidate, base)) continue;
    const rel = candidate.slice(base.length + 1);
    if (!rel) continue;
    if (!best || base.length > best.folder.path.length) best = { folder, rel };
  }
  // A relative path with no usable root: the one folder that is open, if only one.
  if (!best && !absolute && root === "" && folders.length === 1) best = { folder: folders[0], rel: normalized };
  if (!best) return false;
  set({ activeTab: "files" });
  return openFile({ workspaceId: snapshot().workspaceId, folderId: best.folder.id, path: best.rel }, line ?? null, {
    preview: false,
    beside: placement === "beside",
  });
}

/**
 * Opens a file the caller already resolved to a workspace folder (any
 * workspace, not only the title bar's), with the sidebar on Files the way
 * `openFileInProject` leaves it.
 */
export async function openResolvedFile(args: RevealFileArgs): Promise<boolean> {
  set({ activeTab: "files" });
  return openFile(args.location, args.line, args.options);
}

/** Brings up "Find in workspace" over the sidebar's scope. */
export function openWorkspaceSearch(): void {
  ensureStarted();
  openSearch(snapshot().scope ?? WHOLE_WORKSPACE);
}

/**
 * The folder a pointer from outside names: the one whose path is `project`,
 * else the only folder there is. With several and no match, nothing is guessed.
 */
export async function ownerOfNode(project?: string): Promise<{ workspaceId: string; folderId: string } | null> {
  ensureStarted();
  const { workspaceId, folders } = snapshot();
  if (!workspaceId) return null;
  const norm = (p: string) => p.replace(/[\\/]+$/, "").split("\\").join("/");
  const byPath = project ? folders.find((f) => norm(f.path) === norm(project)) : undefined;
  const folder = byPath ?? (folders.length === 1 ? folders[0] : undefined);
  return folder ? { workspaceId, folderId: folder.id } : null;
}

/** An entry point by id, from its folder's list: the named folder first, then any. */
export async function findEntryPoint(
  id: string,
  project?: string,
): Promise<{ owner: { workspaceId: string; folderId: string }; entry: CodeGraphEntryPoint } | null> {
  ensureStarted();
  const { workspaceId, folders, data } = snapshot();
  if (!workspaceId) return null;
  const preferred = project ? await ownerOfNode(project) : null;
  const order = preferred ? [preferred.folderId, ...folders.map((f) => f.id).filter((f) => f !== preferred.folderId)] : folders.map((f) => f.id);
  for (const folderId of order) {
    const entry = data[folderId]?.entries?.entries.find((e) => e.id === id);
    if (entry) return { owner: { workspaceId, folderId }, entry };
  }
  return null;
}

/** Opens a file of a folder in the current workspace by folder id. */
export function openFolderFile(folderId: string, path: string, line?: number, options: { beside?: boolean; preview?: boolean } = {}) {
  const { workspaceId } = snapshot();
  if (!workspaceId || !folderById(folderId)) return Promise.resolve(false);
  return openFile({ workspaceId, folderId, path }, line ?? null, { preview: options.preview ?? false, beside: options.beside });
}

/**
 * Hands a question about the graph to the agent companion: the text lands in
 * the composer of the session on screen, and the companion opens. Nothing is
 * sent — the person reads and presses send (docs/dev-kit/cross-module.mdx).
 */
export function askAgent(text: string) {
  const agents = useAgentSessionsStore.getState();
  const id = agents.activeSessionId;
  if (id) {
    const draft = agents.promptBySession[id] ?? "";
    agents.setPrompt(id, draft ? `${draft}\n\n${text}` : text);
    // The session the text went to, as one of the panel's tabs — not the
    // first tab, which is what the panel shows when this one has none.
    showAgentSession(id);
    return;
  }
  agents.setChatOpen(true);
}

/**
 * Opens a new-session draft whose working folder is `folderId`'s. It sends
 * nothing and changes neither the primary nor any running session's folder or
 * grants: the person reads the draft and starts it (Agent Hub decides what the
 * session may write).
 */
export function openAgentAtFolder(folderId: string): void {
  const { workspaceId } = snapshot();
  const folder = folderById(folderId);
  if (!workspaceId || !folder) return;
  openNewAgentSession({ cwd: folder.path, defaultFolder: false, workspaceId });
}

/** File impact for one file of a folder, read from that folder's index. */
export function blastRadiusOf(folderId: string, path: string) {
  const folder = folderById(folderId);
  if (!folder) return Promise.reject(new Error("This file's folder is not part of the workspace."));
  return getCodeGraphBlastRadius(path, folder.path);
}

export { MODULE, docKey };
