import { errorMessage, toAppError } from "@/bridge/errors";
import {
  folderFileOutline,
  onWorkspaceFileChanged,
  readFolderFile,
  statFolderFile,
  watchDocuments,
  writeFolderFile,
  type FileStatus,
} from "@/bridge/workspaceCode";
import { useWorkbenchTabsStore } from "@/stores/workbenchTabs";
import {
  discardedLocal,
  edited,
  emptyBuffer,
  keptMine,
  loaded,
  orphaned,
  reconcile,
  saveConflicted,
  saveFailed,
  saveRevision,
  saveStarted,
  saveSucceeded,
  type DiskObservation,
  type FileBuffer,
  type SaveMode,
} from "./workspace/bufferModel";
import { docKey, folderLabels, normalizeRelPath, parseDocKey, type DocIdentity } from "./workspace/identity";
import type { NavPlace } from "./workspace/navigationHistory";
import { commitStep, captureDeparture, focusedGroupId, planStep, recordArrival } from "./navigation";
import { MODULE, closeFolderDocuments, documentsOf, focusedDocument, keepTab, openFileDoc, showFlow, syncFileTabTitles, type OpenOptions } from "./documents";
import { begin, folderById, set, snapshot } from "./storeAccess";
import type { OutlineEntry } from "./codegraphStore";
import { loadFolder } from "./workspaceSync";

/**
 * Every way a file is opened, edited, saved, kept true to the disk and
 * navigated back to.
 *
 * A buffer is keyed by `docKey(workspaceId, folderId, path)` and is saved into
 * exactly that folder (Go resolves it from the workspace's membership), so
 * nothing here consults a "current project" or the primary folder.
 */

export type OpenDocOptions = OpenOptions;

export type DocRef = DocIdentity;
export const keyOf = (ref: DocRef) => docKey(ref.workspaceId, ref.folderId, ref.path);

function patchBuffer(key: string, patch: Partial<FileBuffer>) {
  const { buffers } = snapshot();
  const base = buffers[key];
  if (!base) return;
  set({ buffers: { ...buffers, [key]: { ...base, ...patch } } });
}

function putBuffer(key: string, next: FileBuffer) {
  const { buffers } = snapshot();
  if (buffers[key] === next) return;
  set({ buffers: { ...buffers, [key]: next } });
}

// ── Opening ─────────────────────────────────────────────────────────────────

let navigationSeq = 0;

export type OpenFileOptions = OpenDocOptions & {
  /** Put the caret here once the file is on screen. */
  column?: number;
  /** Back/Forward replay: restore this exact place, and record nothing. */
  replay?: NavPlace;
};

/**
 * Opens a file of a folder as a tab and reveals `line`. Every way in — the
 * tree, search, blast radius, the inspector's Edit, go-to-definition, an agent —
 * goes through here, and a file with unsaved edits opens on its edits: nothing
 * is ever discarded to open something.
 *
 * A deliberate open is a navigation: the place being left is captured first and
 * the place arrived at is recorded once the file has actually opened. A newer
 * open supersedes an older one that is still reading (the generation check), so
 * a slow earlier jump cannot land on top of a later one.
 */
export async function openFile(ref: DocRef, line: number | null = null, options: OpenFileOptions = {}): Promise<boolean> {
  const path = normalizeRelPath(ref.path);
  if (!path) return false;
  const doc: DocRef = { ...ref, path };
  const key = keyOf(doc);
  const seq = ++navigationSeq;
  if (!options.replay) captureDeparture();
  openFileDoc(doc, options);
  const ok = await loadBuffer(doc, key, line, options);
  if (seq !== navigationSeq) return false;
  if (ok && !options.replay) {
    const groupId = focusedGroupId();
    if (groupId) recordArrival(groupId, { ...doc, line: line ?? 1, column: options.column ?? 1 });
  }
  return ok;
}

/** Names the buffer's workspace and folder while they are the ones in front. */
function originOf(ref: DocRef): FileBuffer["origin"] {
  const s = snapshot();
  if (s.workspaceId !== ref.workspaceId) return undefined;
  const folder = folderById(ref.folderId);
  if (!folder) return undefined;
  return { workspace: s.workspace?.name ?? "", folder: folderLabels(s.folders.map((f) => ({ id: f.id, name: f.name, path: f.path })))[folder.id] ?? folder.name };
}

let restoreToken = 0;

async function loadBuffer(ref: DocRef, key: string, line: number | null, options: OpenFileOptions): Promise<boolean> {
  const existing = snapshot().buffers[key];
  const reveal: Partial<FileBuffer> = options.replay
    ? { line: options.replay.line, restore: { line: options.replay.line, column: options.replay.column, anchor: options.replay.anchor, head: options.replay.head, scrollTop: options.replay.scrollTop, token: ++restoreToken } }
    : line !== null
      ? { line, restore: { line, column: options.column, token: ++restoreToken } }
      : {};
  if (existing?.dirty || (existing && !existing.loading && !existing.error)) {
    patchBuffer(key, reveal);
    // A buffer that is already open is checked against the disk instead of
    // read again: cheaper, and it never replaces text the person is holding.
    void reconcileFile(key);
    return true;
  }
  const guard = begin(`file:${key}`);
  const fresh = existing ?? { ...emptyBuffer(ref), origin: originOf(ref) };
  putBuffer(key, { ...fresh, ...reveal, loading: !existing?.content, error: "" });
  try {
    const read = await readFolderFile(ref.workspaceId, ref.folderId, ref.path);
    if (!guard()) return false;
    const cur = snapshot().buffers[key];
    // Typed into while the read was in flight: the typing wins.
    if (!cur || cur.dirty) return true;
    putBuffer(key, { ...loaded(cur, read) });
    void syncWatchedDocuments();
    return true;
  } catch (e) {
    if (!guard()) return false;
    const code = toAppError(e).code;
    const cur = snapshot().buffers[key];
    if (cur) {
      const next = { ...cur, loading: false, error: errorMessage(e) };
      putBuffer(key, code === "folder_not_in_workspace" ? { ...next, orphan: "folder_removed" } : next);
    }
    return false;
  }
}

/**
 * Makes sure a document that is already open in a tab has its buffer — what a
 * tab restored after a relaunch needs. Unlike openFile it opens no tab, moves no
 * focus and records no navigation: the tab is already there.
 */
export function ensureBuffer(ref: DocRef): void {
  const key = keyOf(ref);
  const existing = snapshot().buffers[key];
  if (existing && !existing.error) return;
  if (existing?.loading) return;
  void loadBuffer(ref, key, null, {});
}

/** The buffer read again from disk — after a failed read; never over unsaved edits. */
export function reloadFile(key: string): void {
  const b = snapshot().buffers[key];
  if (!b || b.dirty) return;
  void loadBuffer(b, key, null, {});
}

// ── Editing and saving ──────────────────────────────────────────────────────

export function editFile(key: string, content: string, tabId?: string) {
  const b = snapshot().buffers[key];
  if (!b) return;
  const next = edited(b, content);
  if (next === b) return;
  putBuffer(key, next);
  // An edited preview is not a preview any more: the next click must not
  // replace it.
  if (tabId) keepTab(tabId);
}

/**
 * Saves a buffer into its owner folder. A plain save asserts the revision the
 * buffer is based on; a file that has another one on disk is refused with
 * nothing written, and the buffer shows the conflict instead of retrying.
 * `overwrite` and `recreate` are the explicit answers to a conflict.
 */
export async function saveFile(key: string, mode: SaveMode = "normal"): Promise<void> {
  const b = snapshot().buffers[key];
  if (!b || b.saving) return;
  const expected = saveRevision(b, mode);
  if (expected === null) {
    if (b.orphan) {
      patchBuffer(key, { error: "This file's folder is no longer in the workspace. Copy your edits or save them elsewhere." });
    } else if (b.external) {
      patchBuffer(key, { error: "The file changed on disk. Compare, reload, or overwrite on purpose — Save will not do it for you." });
    }
    return;
  }
  const saved = b.content;
  // The editor keeps its own line endings; give the file back its own.
  const text = b.eol === "\r\n" ? saved.replace(/\r?\n/g, "\r\n") : saved;
  putBuffer(key, saveStarted(b));
  try {
    const result = await writeFolderFile(b.workspaceId, b.folderId, b.path, text, expected);
    const cur = snapshot().buffers[key];
    if (cur) putBuffer(key, saveSucceeded(cur, saved, result.revision));
    // Only ever git marks for the folder it was saved into.
    if (snapshot().data[b.folderId]) void loadFolder(b.folderId, ["git"]);
  } catch (e) {
    const cur = snapshot().buffers[key];
    if (!cur) return;
    const code = toAppError(e).code;
    if (code === "file_conflict") {
      let seen: DiskObservation = { exists: true, revision: cur.diskRevision };
      try {
        seen = toObservation(await statFolderFile(b.workspaceId, b.folderId, b.path));
      } catch {
        // Keep what is known; the conflict stands.
      }
      putBuffer(key, saveConflicted(cur, seen));
    } else if (code === "folder_not_in_workspace") {
      putBuffer(key, { ...orphaned(cur, "folder_removed"), error: "This file's folder left the workspace, so nothing was saved. Your edits are still here." });
    } else {
      putBuffer(key, saveFailed(cur, errorMessage(e)));
    }
  } finally {
    // Whatever the disk did while the write was in flight was ignored; look again.
    void reconcileFile(key);
  }
}

/** Throws a buffer's edits away and closes its tabs. */
export function discardFile(key: string): void {
  begin(`file:${key}`);
  const { [key]: _gone, ...rest } = snapshot().buffers;
  set({ buffers: rest });
  closeTabsOf(key);
  void syncWatchedDocuments();
}

function closeTabsOf(key: string) {
  const store = useWorkbenchTabsStore.getState();
  for (const { tab, doc } of documentsOf()) {
    if (doc.kind === "file" && doc.workspaceId && doc.folderId && doc.path && docKey(doc.workspaceId, doc.folderId, doc.path) === key) {
      store.closeTab(tab.id);
    }
  }
}

// ── Keeping buffers true to the disk ────────────────────────────────────────

const toObservation = (st: FileStatus): DiskObservation =>
  st.exists ? { exists: true, revision: st.revision, unreadable: st.unreadable || undefined } : { exists: false };

/**
 * Looks at the disk for one open buffer and applies what it finds
 * (bufferModel.reconcile): a clean buffer follows, a dirty one raises a
 * conflict, a deleted file is noted and kept. Fingerprints decide, so a save
 * echoed back by the watcher is not a change.
 */
export async function reconcileFile(key: string): Promise<void> {
  const b = snapshot().buffers[key];
  if (!b || b.saving || b.orphan || b.loading) return;
  const guard = begin(`reconcile:${key}`);
  let status: FileStatus;
  try {
    status = await statFolderFile(b.workspaceId, b.folderId, b.path);
  } catch (e) {
    if (guard() && toAppError(e).code === "folder_not_in_workspace") {
      const cur = snapshot().buffers[key];
      if (cur) putBuffer(key, orphaned(cur, "folder_removed"));
    }
    return;
  }
  if (!guard()) return;
  const cur = snapshot().buffers[key];
  if (!cur) return;
  const { buffer, reload } = reconcile(cur, toObservation(status));
  putBuffer(key, buffer);
  if (reload) await reloadClean(key, buffer.diskRevision);
}

async function reloadClean(key: string, expectRevision: string): Promise<void> {
  const b = snapshot().buffers[key];
  if (!b) return;
  const guard = begin(`reload:${key}`);
  try {
    const read = await readFolderFile(b.workspaceId, b.folderId, b.path);
    if (!guard()) return;
    const cur = snapshot().buffers[key];
    // Typed into while reading: the edits win and the conflict path takes over.
    if (!cur || cur.dirty || cur.saving) {
      void reconcileFile(key);
      return;
    }
    if (read.revision !== expectRevision && read.revision === cur.baseRevision) return;
    putBuffer(key, { ...loaded(cur, read), reloadedAt: Date.now() });
  } catch {
    // The next event or focus return tries again.
  }
}

/** "Keep mine": acknowledges the conflict for the disk's current revision. Saving stays guarded. */
export function keepMine(key: string): void {
  const b = snapshot().buffers[key];
  if (b) putBuffer(key, keptMine(b));
}

/** The disk's version of a conflicted buffer, to compare with or to take. */
export async function readDiskVersion(key: string): Promise<{ content: string; revision: string } | null> {
  const b = snapshot().buffers[key];
  if (!b) return null;
  try {
    return await readFolderFile(b.workspaceId, b.folderId, b.path);
  } catch {
    return null;
  }
}

/** The person chose the disk's version over their edits. */
export async function reloadFromDisk(key: string): Promise<void> {
  const b = snapshot().buffers[key];
  if (!b) return;
  const read = await readDiskVersion(key);
  if (!read) {
    patchBuffer(key, { error: "Could not read the file from disk." });
    return;
  }
  putBuffer(key, discardedLocal(b, read));
}

/** Looks at every open or dirty buffer — after the window was in the background, or events were lost. */
export function reconcileOpenFiles(): void {
  for (const key of watchedKeys()) void reconcileFile(key);
}

// ── Folder removed ──────────────────────────────────────────────────────────

/**
 * After the workspace's folders changed: a buffer whose folder is gone becomes
 * an orphan — its edits stay, Save is refused, and the person can copy them or
 * save them elsewhere. A clean one just has its tabs closed. A folder added back
 * at the same path has a new id, so it never adopts an old buffer.
 */
export function markRemovedFolders(): void {
  const { workspaceId, folders, buffers } = snapshot();
  const present = new Set(folders.map((f) => f.id));
  const removed = new Set<string>();
  for (const b of Object.values(buffers)) {
    if (b.workspaceId === workspaceId && !present.has(b.folderId)) removed.add(b.folderId);
  }
  if (removed.size === 0) return;
  const next = { ...buffers };
  for (const [key, b] of Object.entries(buffers)) {
    if (!removed.has(b.folderId) || b.workspaceId !== workspaceId) continue;
    if (b.dirty) next[key] = orphaned(b, "folder_removed");
    else delete next[key];
  }
  set({ buffers: next });
  for (const folderId of removed) {
    closeFolderDocuments(folderId, (path) => Boolean(next[docKey(workspaceId, folderId, path)]?.dirty));
  }
  void syncWatchedDocuments();
}

// ── Watching ────────────────────────────────────────────────────────────────

function watchedKeys(): string[] {
  const keys = new Set<string>();
  for (const { doc } of documentsOf()) {
    if (doc.kind === "file" && doc.workspaceId && doc.folderId && doc.path) keys.add(docKey(doc.workspaceId, doc.folderId, doc.path));
  }
  for (const [key, b] of Object.entries(snapshot().buffers)) if (b.dirty) keys.add(key);
  return [...keys].filter((k) => snapshot().buffers[k] && !snapshot().buffers[k].orphan);
}

let lastWatched = "";
let watchTimer: ReturnType<typeof setTimeout> | undefined;

/**
 * Tells Go which documents are open (open tabs and unsaved buffers), so changes
 * to them arrive as events. Debounced: opening a few files at once is one call.
 */
export function syncWatchedDocuments(): void {
  clearTimeout(watchTimer);
  watchTimer = setTimeout(() => {
    const keys = watchedKeys().sort();
    const signature = keys.join("\n");
    if (signature === lastWatched) return;
    lastWatched = signature;
    const docs = keys.flatMap((k) => {
      const id = parseDocKey(k);
      return id ? [{ workspaceId: id.workspaceId, folderId: id.folderId, path: id.path }] : [];
    });
    watchDocuments(docs).then(
      (unwatched) => set({ unwatched }),
      () => set({ unwatched: keys }),
    );
  }, 80);
}

let wired = false;

/** Subscribes to file-change events, the window's focus and the tab set. Once. */
export function wireFileEvents(): void {
  if (wired) return;
  wired = true;
  onWorkspaceFileChanged(({ keys, resync }) => {
    if (resync) reconcileOpenFiles();
    else for (const key of keys) void reconcileFile(key);
  });
  // The fallback for everything the watcher cannot promise: documents it could
  // not watch, events lost while the app slept, a network mount.
  const onReturn = () => reconcileOpenFiles();
  window.addEventListener("focus", onReturn);
  document.addEventListener("visibilitychange", () => {
    if (document.visibilityState === "visible") onReturn();
  });
  useWorkbenchTabsStore.subscribe((state, prev) => {
    if (state.modules[MODULE] !== prev.modules[MODULE]) {
      pruneClosedDocuments();
      syncFileTabTitles();
      syncWatchedDocuments();
    }
  });
}

/**
 * Drops the state of documents whose tab is gone: a flow nobody is looking at,
 * a clean buffer, an outline. A buffer with unsaved edits is kept — closing a
 * tab is not "throw my edits away", and opening the file again finds them.
 */
export function pruneClosedDocuments(): void {
  const docs = documentsOf().map((d) => d.doc);
  const openFiles = new Set(
    docs.flatMap((d) => (d.kind === "file" && d.workspaceId && d.folderId && d.path ? [docKey(d.workspaceId, d.folderId, d.path)] : [])),
  );
  const openFlows = new Set(docs.flatMap((d) => (d.kind === "flow" && d.folderId && d.root ? [`${d.folderId}\u0000${d.root}`] : [])));
  const { buffers, flows, outlines, inspector } = snapshot();
  const keptBuffers = Object.fromEntries(Object.entries(buffers).filter(([key, b]) => openFiles.has(key) || b.dirty || b.saving));
  const keptFlows = Object.fromEntries(Object.entries(flows).filter(([key]) => openFlows.has(key)));
  const keptOutlines = Object.fromEntries(Object.entries(outlines).filter(([key]) => openFiles.has(key)));
  const patch: Record<string, unknown> = {};
  if (Object.keys(keptBuffers).length !== Object.keys(buffers).length) patch.buffers = keptBuffers;
  if (Object.keys(keptFlows).length !== Object.keys(flows).length) patch.flows = keptFlows;
  if (Object.keys(keptOutlines).length !== Object.keys(outlines).length) patch.outlines = keptOutlines;
  if (inspector && !openFlows.has(inspector.flow)) Object.assign(patch, { inspector: null, snippet: null });
  if (Object.keys(patch).length > 0) set(patch);
}

// ── Outline ─────────────────────────────────────────────────────────────────

/**
 * Reads a file's outline from the index — the saved file as of the index time,
 * cached under the document until the folder's index changes (workspaceSync
 * drops the cache then). Moving the caret never calls this.
 */
export async function loadOutline(key: string, force = false): Promise<void> {
  const id = parseDocKey(key);
  if (!id) return;
  const have = snapshot().outlines[key];
  if (!force && have && (have.loading || !have.error)) return;
  const guard = begin(`outline:${key}`);
  const blank: OutlineEntry = { path: id.path, state: "indexing", symbols: [], indexedAt: 0, loading: true, error: "" };
  set({ outlines: { ...snapshot().outlines, [key]: have ? { ...have, loading: true } : blank } });
  try {
    const outline = await folderFileOutline(id.workspaceId, id.folderId, id.path);
    if (guard()) set({ outlines: { ...snapshot().outlines, [key]: { ...outline, loading: false, error: "" } } });
  } catch (e) {
    if (guard()) set({ outlines: { ...snapshot().outlines, [key]: { ...(have ?? blank), loading: false, error: errorMessage(e) } } });
  }
}

/** After a folder's index changed: the outline of the file in front, if it is that folder's, is read again. */
export async function reloadOutlinesOf(folderId: string): Promise<void> {
  const front = focusedDocument()?.doc;
  if (front?.kind === "file" && front.folderId === folderId && front.workspaceId && front.path) {
    await loadOutline(docKey(front.workspaceId, folderId, front.path), true);
  }
}

// ── Back / Forward ──────────────────────────────────────────────────────────

/** Is this place still somewhere a document can be opened? Cheap, synchronous checks only. */
function placeUsable(p: NavPlace): boolean {
  const s = snapshot();
  if (p.workspaceId === s.workspaceId && !folderById(p.folderId)) return false;
  const b = s.buffers[docKey(p.workspaceId, p.folderId, p.path)];
  return !(b && b.disk === "deleted" && !b.dirty);
}

async function navigate(dir: "back" | "forward"): Promise<boolean> {
  const seq = ++navigationSeq;
  // A few attempts: a place whose file is gone from the disk is skipped, not shown as an error.
  const skipped = new Set<string>();
  for (let attempt = 0; attempt < 6; attempt++) {
    const plan = planStep(dir, (p) => placeUsable(p) && !skipped.has(`${p.folderId}\u0000${p.path}`));
    if (!plan) return false;
    const target = plan.step.target;
    if (target.flow) {
      const owner = { workspaceId: target.workspaceId, folderId: target.folderId };
      showFlow(owner, target.flow);
      void import("./flowActions").then((a) => a.loadFlow(target.folderId, target.flow!.root));
      commitStep(plan.groupId, plan.step);
      return true;
    }
    let gone = false;
    try {
      const st = await statFolderFile(target.workspaceId, target.folderId, target.path);
      gone = !st.exists && !snapshot().buffers[docKey(target.workspaceId, target.folderId, target.path)]?.dirty;
    } catch {
      gone = true;
    }
    if (seq !== navigationSeq) return false;
    if (gone) {
      skipped.add(`${target.folderId}\u0000${target.path}`);
      continue;
    }
    // openFile takes its own sequence number and returns false if a newer
    // navigation superseded it, so only a replay that really opened is committed.
    const ok = await openFile(target, target.line, { replay: target, preview: false });
    if (ok) commitStep(plan.groupId, plan.step);
    return ok;
  }
  return false;
}

export const goBack = () => navigate("back");
export const goForward = () => navigate("forward");
