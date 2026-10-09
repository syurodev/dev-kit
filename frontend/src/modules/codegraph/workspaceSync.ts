import { onCodeGraphIndexUpdated, onCodeGraphServiceClosed } from "@/bridge/codegraph";
import { listWorkspaces, listWorkspaceServices, onServiceIndexStatus, workspaceIndexStatus } from "@/bridge/workspace";
import {
  folderEntryPoints,
  folderGitStatus,
  listFolderFiles,
  listFolderStates,
  reindexWorkspaceFolders,
  setWorkspacePrimary,
} from "@/bridge/workspaceCode";
import { errorMessage } from "@/bridge/errors";
import { useWorkspaceSwitcherStore } from "@/stores/workspaceSwitcher";
import { codeGraphRequests, emptyFolderData, newSearchSession, type FolderData } from "./codegraphStore";
import { begin, folderById, folderByPath, patchFolder, set, snapshot } from "./storeAccess";
import { WHOLE_WORKSPACE, normalizeScope, scopeFolderIds, type Scope } from "./workspace/scope";
import { folderLabels } from "./workspace/identity";
import { setFolderLabelSource, syncFileTabTitles } from "./documents";

/**
 * Keeping the store's picture of the workspace's folders true.
 *
 * Three different things change what the sidebar shows, and they are kept
 * apart on purpose (docs/design/codegraph-workspace.md § 5):
 *
 *   - the workspace changes (the title-bar switcher)   → switchWorkspace
 *   - its folders or primary change (Edit workspace)   → reloadFolders
 *   - the discovery scope changes (the header picker)  → setScope
 *
 * None of them closes a tab or drops a buffer. A request that finishes after
 * its workspace was left finds the request guard invalidated and says nothing.
 */

/** How many folders are read at once. Reading is disk and git, not analysis. */
const FOLDER_CONCURRENCY = 3;

export async function runPool<T>(items: readonly T[], limit: number, fn: (item: T) => Promise<void>): Promise<void> {
  let next = 0;
  const workers = Array.from({ length: Math.min(limit, items.length) }, async () => {
    while (next < items.length) {
      const item = items[next++];
      try {
        await fn(item);
      } catch {
        // Each fn reports its own failure into the store; one folder's does not stop the rest.
      }
    }
  });
  await Promise.all(workers);
}

/** Labels for the current folders, unique where names repeat. */
export function currentFolderLabels(): Record<string, string> {
  return folderLabels(snapshot().folders.map((f) => ({ id: f.id, name: f.name, path: f.path })));
}

setFolderLabelSource(currentFolderLabels);

// ── Workspace ───────────────────────────────────────────────────────────────

/** Aligns the store with the title-bar switcher's workspace. */
export async function syncWorkspace(): Promise<void> {
  const id = useWorkspaceSwitcherStore.getState().currentId ?? "";
  const s = snapshot();
  if (id === s.workspaceId && s.foldersStatus !== "idle") return;
  await switchWorkspace(id);
}

async function switchWorkspace(id: string): Promise<void> {
  // Nothing asked of the old workspace is an answer about this one.
  codeGraphRequests.invalidateAll();
  const generation = snapshot().generation + 1;
  set({
    workspaceId: id,
    workspace: null,
    generation,
    folders: [],
    primaryId: "",
    foldersStatus: id ? "loading" : "idle",
    foldersError: "",
    scope: WHOLE_WORKSPACE,
    data: {},
    panel: null,
    inspector: null,
    snippet: null,
    snippetLoading: false,
    snippetError: "",
    plans: {},
    planFolderId: "",
    selectedPlanNodeId: null,
    commentsPlanNodeId: null,
    search: newSearchSession(id),
    query: "",
    fileExpanded: [],
    collapsedRoots: [],
    collapsedGroups: [],
    // Buffers, flows, outlines and tabs stay: they belong to their documents,
    // whose owners are named in their keys, and an unsaved edit must survive
    // the title bar being used.
  });
  if (!id) return;
  await loadFolders(generation, false);
}

/** The workspace's folders were edited (added, removed, new primary). */
export async function reloadFolders(): Promise<void> {
  const s = snapshot();
  if (!s.workspaceId) return;
  await loadFolders(s.generation, true);
}

async function loadFolders(generation: number, keep: boolean): Promise<void> {
  const workspaceId = snapshot().workspaceId;
  const current = () => snapshot().generation === generation && snapshot().workspaceId === workspaceId;
  try {
    const [workspaces, services] = await Promise.all([listWorkspaces(), listWorkspaceServices(workspaceId)]);
    if (!current()) return;
    const workspace = workspaces.find((w) => w.id === workspaceId) ?? null;
    if (!workspace) {
      set({ foldersStatus: "error", foldersError: "This workspace no longer exists." });
      return;
    }
    // The primary is whichever folder the workspace's root names; it is read
    // here, never remembered apart from it.
    const primaryId = services.find((f) => f.path === workspace.root)?.id ?? services[0]?.id ?? "";
    const before = snapshot();
    const data: Record<string, FolderData> = {};
    for (const f of services) data[f.id] = (keep && before.data[f.id]) || emptyFolderData();
    const scope = normalizeScope(before.scope, services);
    const plans = { ...before.plans };
    for (const id of Object.keys(plans)) if (!services.some((f) => f.id === id)) delete plans[id];
    set({
      workspace,
      folders: services,
      primaryId,
      data,
      scope,
      plans,
      planFolderId: services.some((f) => f.id === before.planFolderId) ? before.planFolderId : "",
      foldersStatus: "ready",
      foldersError: "",
    });
    syncFileTabTitles();
    // Files edited in a folder that has left the workspace are drafts now.
    const { markRemovedFolders } = await import("./fileActions");
    markRemovedFolders();
    void loadIndexStatuses(generation);
    void refreshScope(generation, !keep);
  } catch (e) {
    if (current()) set({ foldersStatus: "error", foldersError: errorMessage(e) });
  }
}

async function loadIndexStatuses(generation: number): Promise<void> {
  const workspaceId = snapshot().workspaceId;
  try {
    const list = await workspaceIndexStatus(workspaceId);
    if (snapshot().generation !== generation) return;
    const { data } = snapshot();
    const next = { ...data };
    for (const status of list) {
      const f = folderByPath(status.path);
      if (f && next[f.id]) next[f.id] = { ...next[f.id], indexStatus: status };
    }
    set({ data: next });
  } catch {
    // The header shows what it has; statuses arrive again by event.
  }
}

// ── Folder data ─────────────────────────────────────────────────────────────

export type FolderPart = "state" | "files" | "git" | "entries";
const ALL_PARTS: FolderPart[] = ["state", "entries", "files", "git"];

/** Reads some parts of one folder. Each part is guarded on its own, per folder. */
export async function loadFolder(folderId: string, parts: readonly FolderPart[] = ALL_PARTS): Promise<void> {
  const { workspaceId, generation } = snapshot();
  const folder = folderById(folderId);
  if (!folder) return;
  const live = (check: () => boolean) => check() && snapshot().generation === generation && Boolean(folderById(folderId));
  const tasks: Promise<void>[] = [];

  if (parts.includes("state")) {
    // One call covers the workspace, so ask once per burst rather than per folder.
    tasks.push(loadStatesOnce(workspaceId, generation));
  }
  if (parts.includes("entries")) {
    const ok = begin(`entries:${folderId}`);
    patchFolder(folderId, { entriesStatus: snapshot().data[folderId]?.entries ? "ready" : "loading" });
    tasks.push(
      folderEntryPoints(workspaceId, folderId).then(
        (entries) => live(ok) && patchFolder(folderId, { entries, entriesStatus: "ready", entriesError: "" }),
        (e) => live(ok) && patchFolder(folderId, { entriesStatus: "error", entriesError: errorMessage(e) }),
      ).then(() => undefined),
    );
  }
  if (parts.includes("files")) {
    const ok = begin(`files:${folderId}`);
    patchFolder(folderId, { filesStatus: snapshot().data[folderId]?.filesStatus === "ready" ? "ready" : "loading" });
    tasks.push(
      listFolderFiles(workspaceId, folderId).then(
        ({ entries, truncated }) => live(ok) && patchFolder(folderId, { files: entries, filesTruncated: truncated, filesStatus: "ready", filesError: "" }),
        // The tree keeps what it had: an empty rail would read as an empty folder.
        (e) => live(ok) && patchFolder(folderId, { filesStatus: "error", filesError: errorMessage(e) }),
      ).then(() => undefined),
    );
  }
  if (parts.includes("git")) {
    const ok = begin(`git:${folderId}`);
    tasks.push(
      folderGitStatus(workspaceId, folderId).then(
        (git) => live(ok) && patchFolder(folderId, { git }),
        () => undefined, // marks decorate the tree; never break it
      ).then(() => undefined),
    );
  }
  await Promise.all(tasks);
}

let statesInFlight: { key: string; promise: Promise<void> } | null = null;

/** Reads every folder's machine state (reachable, branch, changed) in one call. */
function loadStatesOnce(workspaceId: string, generation: number): Promise<void> {
  const key = `${workspaceId}:${generation}`;
  if (statesInFlight?.key === key) return statesInFlight.promise;
  const ok = begin(`states:${workspaceId}`);
  const promise = listFolderStates(workspaceId)
    .then((states) => {
      if (!ok() || snapshot().generation !== generation) return;
      const { data } = snapshot();
      const next = { ...data };
      for (const st of states) if (next[st.folderId]) next[st.folderId] = { ...next[st.folderId], state: st };
      set({ data: next });
    })
    .catch(() => undefined)
    .finally(() => {
      if (statesInFlight?.key === key) statesInFlight = null;
    });
  statesInFlight = { key, promise };
  return promise;
}

/** Loads the folders the sidebar shows (and only those) that have not been read yet, or all of them again. */
export async function refreshScope(generation = snapshot().generation, force = false): Promise<void> {
  const s = snapshot();
  const ids = scopeFolderIds(s.scope, s.folders);
  const todo = ids.filter((id) => force || !s.data[id] || s.data[id].filesStatus === "idle" || s.data[id].entriesStatus === "idle");
  await runPool(todo, FOLDER_CONCURRENCY, async (id) => {
    if (snapshot().generation !== generation) return;
    await loadFolder(id);
  });
}

// ── Scope, primary, reindex ─────────────────────────────────────────────────

/**
 * Narrows or widens what the sidebar lists. Closes no tab, drops no buffer and
 * leaves the primary alone: a lens, not a navigation.
 */
export function setScope(scope: Scope): void {
  const s = snapshot();
  const next = normalizeScope(scope, s.folders);
  set({ scope: next });
  void refreshScope();
}

export async function makePrimary(folderId: string): Promise<void> {
  const { workspaceId } = snapshot();
  if (!workspaceId || folderById(folderId) === undefined) return;
  await setWorkspacePrimary(workspaceId, folderId);
  // The switcher's version bump is how every list of this workspace's folders
  // (Agent Hub's pickers, the title bar) reads them again; ours follows it.
  useWorkspaceSwitcherStore.getState().servicesChanged();
}

/** Reindex the folders in the sidebar's scope — the default meaning of "Reindex". */
export async function reindexScope(): Promise<void> {
  const s = snapshot();
  await reindexFolders(scopeFolderIds(s.scope, s.folders));
}

/** Reindex every folder of the workspace, whatever the scope. */
export async function reindexWorkspace(): Promise<void> {
  await reindexFolders(snapshot().folders.map((f) => f.id));
}

export async function reindexFolders(folderIds: string[]): Promise<void> {
  const { workspaceId } = snapshot();
  if (!workspaceId || folderIds.length === 0) return;
  try {
    await reindexWorkspaceFolders(workspaceId, folderIds, false);
  } catch (e) {
    set({ foldersError: errorMessage(e) });
  }
}

/** Withdraw folders (all of the workspace's when none are named) from the index queue. */
export async function cancelIndexing(folderIds: string[] = []): Promise<void> {
  const { workspaceId } = snapshot();
  if (!workspaceId) return;
  try {
    await reindexWorkspaceFolders(workspaceId, folderIds, true);
  } catch (e) {
    set({ foldersError: errorMessage(e) });
  }
}

/** Retry every folder whose last run failed. */
export async function retryFailed(): Promise<void> {
  const s = snapshot();
  const failed = s.folders.filter((f) => {
    const st = s.data[f.id]?.indexStatus?.state;
    return st === "failed";
  });
  await reindexFolders(failed.map((f) => f.id));
}

// ── Events ──────────────────────────────────────────────────────────────────

/** A folder's index changed: what was derived from it is read again, the rest is left. */
export async function onFolderIndexed(folderId: string): Promise<void> {
  await loadFolder(folderId, ["entries", "git"]);
  // Outlines are keyed by the index they were read from.
  const { outlines } = snapshot();
  const kept = Object.fromEntries(Object.entries(outlines).filter(([key]) => !key.includes(`:${folderId}:`)));
  if (Object.keys(kept).length !== Object.keys(outlines).length) set({ outlines: kept });
  const { refreshFolderFlows } = await import("./flowActions");
  void refreshFolderFlows(folderId);
  const { refreshFolderPlan } = await import("./planActions");
  void refreshFolderPlan(folderId);
  const { reloadOutlinesOf } = await import("./fileActions");
  void reloadOutlinesOf(folderId);
}

let wired = false;

/** Subscribes to the events that keep the folder data true. Once, for the app's lifetime. */
export function wireWorkspaceEvents(): void {
  if (wired) return;
  wired = true;

  useWorkspaceSwitcherStore.subscribe((state, prev) => {
    if (state.currentId !== prev.currentId) void syncWorkspace();
    else if (state.servicesVersion !== prev.servicesVersion) void reloadFolders();
  });
  void syncWorkspace();

  // Every re-index names its folder; only that one is read again.
  onCodeGraphIndexUpdated((_version, servicePath) => {
    const f = folderByPath(servicePath);
    if (f) void onFolderIndexed(f.id);
  });
  onCodeGraphServiceClosed(() => {
    // A resident service shut down to make room; its index is on disk, nothing shown is wrong.
  });
  onServiceIndexStatus((status) => {
    const f = folderByPath(status.path);
    if (!f) return;
    patchFolder(f.id, { indexStatus: status });
    if (status.state === "indexed" || status.state === "failed" || status.state === "unsupported") {
      void loadFolder(f.id, ["entries"]);
    }
  });
}
