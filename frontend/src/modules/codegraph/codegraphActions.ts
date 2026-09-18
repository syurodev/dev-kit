import { Dialogs } from "@wailsio/runtime";
import {
  codeGraphAnalyzerStatus,
  codeGraphProjectRoot,
  waitForCodeGraphIndexReady,
  openCodeGraphProject,
  listCodeGraphEndpoints,
  getCodeGraphNeighbors,
  getCodeGraphCodeSnippet,
  listCodeGraphFiles,
  readCodeGraphFile,
  writeCodeGraphFile,
  type CodeGraphEndpoint,
  type CodeGraphSubgraph,
} from "@/bridge/codegraph";
import {
  approveCodeGraphPlan,
  countCodeGraphPlanNodeComments,
  relinkCodeGraphPlanNode,
  getCodeGraphPlanEdges,
  getCodeGraphPlanProgress,
} from "@/bridge/codegraphPlan";
import { errorMessage, toAppError } from "@/bridge/errors";
import { codeGraphRequests, useCodeGraphStore } from "./codegraphStore";
import { isNoPlanError, isProjectChangedError } from "./codegraphErrors";
import { mergeSubgraph } from "./subgraph";

/**
 * Everything CodeGraph asks the bridge, as module-level functions over the
 * module store (codegraphStore.ts says why the view cannot own this state).
 *
 * Every action that awaits takes a token from codeGraphRequests first and
 * checks it after each await: a response to a request the user has since
 * superseded — a second endpoint clicked, the project switched — is dropped
 * rather than painted over the newer answer.
 */
const set = useCodeGraphStore.getState().set;
const snapshot = () => useCodeGraphStore.getState();
const begin = codeGraphRequests.begin;

const NEIGHBOR_DEPTH = 3;

/**
 * What the sidebar says after an index.
 *
 * A monorepo indexes as one graph, so without a module count four services
 * look exactly like one project that happens to have a lot of endpoints. Said
 * only when there is more than one — "1 module" is noise on the common case.
 */
export function indexedStatus(moduleCount: number, endpointCount: number): string {
  const modules = moduleCount > 1 ? `${moduleCount} modules · ` : "";
  return `Indexed. ${modules}${endpointCount} endpoint(s) found.`;
}

async function loadFiles() {
  const current = begin("files");
  try {
    const files = await listCodeGraphFiles();
    if (current()) set({ files });
  } catch (e) {
    if (current()) set({ fileError: errorMessage(e) });
  }
}

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
 * Drops everything that belongs to the previous project and supersedes every
 * request still in flight for it.
 *
 * An unsaved buffer is the one exception: throwing away someone's edits
 * because an agent switched projects over MCP would be data loss. It stays on
 * screen with its old fileRoot, which the view shows as "from another project"
 * and which saving refuses (and Go refuses too), so it can be copied out but
 * never written into the new project at the same relative path.
 */
function switchProject(root: string) {
  codeGraphRequests.invalidateAll();
  const { fileDirty } = snapshot();
  set({
    projectPath: root,
    endpoints: [],
    files: [],
    subgraph: null,
    rootId: null,
    rootEndpointId: null,
    subgraphPages: 0,
    loadingMore: false,
    busy: false,
    status: "",
    error: "",
    errorCode: "",
    inspectorNodeId: null,
    snippet: null,
    snippetLoading: false,
    snippetError: "",
    plan: null,
    planEdges: [],
    planBusy: false,
    planError: "",
    selectedPlanNodeId: null,
    commentsPlanNodeId: null,
    commentCounts: {},
    pendingOpenFile: null,
    pendingOpenLine: null,
    fileError: "",
    ...(fileDirty
      ? {}
      : { selectedFile: null, fileRoot: "", selectedLine: null, fileContent: "", fileDirty: false }),
  });
}

export async function reindex(path: string) {
  if (path !== snapshot().projectPath) switchProject(path);
  const current = begin("reindex");
  set({
    busy: true,
    status: "Indexing…",
    error: "",
    errorCode: "",
    subgraph: null,
    rootId: null,
    rootEndpointId: null,
    subgraphPages: 0,
  });
  try {
    const detected = await openCodeGraphProject(path);
    if (!current()) return;
    const list = await listCodeGraphEndpoints();
    if (!current()) return;
    set({ endpoints: list });
    await loadFiles();
    if (!current()) return;
    set({ status: indexedStatus(detected.modules.length, list.length) });
  } catch (e) {
    if (!current()) return;
    const failure = toAppError(e);
    set({ endpoints: [], files: [], status: "", error: failure.message, errorCode: failure.code });
  } finally {
    // A superseded reindex leaves busy to whoever superseded it: a project
    // switch resets it, and a newer reindex set it itself.
    if (current()) set({ busy: false });
  }
}

export async function pickAndOpenProject() {
  set({ error: "" });
  let chosen: string;
  try {
    chosen = (await Dialogs.OpenFile({
      CanChooseDirectories: true,
      CanChooseFiles: false,
      Title: "Open Java project",
    })) as string;
  } catch (e) {
    set({ error: errorMessage(e) });
    return;
  }
  if (!chosen) {
    return; // user cancelled the picker
  }
  await reindex(chosen);
}

/**
 * The active project changed underneath the view — codegraph:projectChanged,
 * which also fires for this window's own picker. Our own switch has already
 * moved projectPath, so only a root we are not showing needs anything done.
 */
export async function handleProjectChanged(root: string) {
  if (!root || root === snapshot().projectPath) return;
  switchProject(root);
  set({ status: "Indexing…" });
  await refreshAfterReindex();
  if (snapshot().projectPath === root && snapshot().status === "Indexing…") set({ status: "" });
}

/** On mount: adopt the project Go restored, if the store has none yet. */
export async function restoreProject(isCancelled: () => boolean) {
  const root = await codeGraphProjectRoot();
  if (isCancelled() || !root || snapshot().projectPath) return;
  set({ projectPath: root, status: "Indexing…" });
  await refreshAfterReindex();
  if (!isCancelled() && snapshot().projectPath === root) set({ status: "" });
}

export async function viewFlow(endpoint: CodeGraphEndpoint) {
  const current = begin("subgraph");
  set({
    error: "",
    inspectorNodeId: null,
    snippet: null,
    snippetError: "",
    loadingMore: false,
  });
  try {
    const sub = await getCodeGraphNeighbors(endpoint.entryNode, NEIGHBOR_DEPTH);
    if (!current()) return;
    set({ subgraph: sub, rootId: endpoint.entryNode, rootEndpointId: endpoint.id, subgraphPages: 1 });
  } catch (e) {
    if (!current()) return;
    set({ subgraph: null, error: errorMessage(e) });
  }
}

/**
 * Fetches the next page of a truncated subgraph and merges it in. Without
 * this, a graph over the backend's ceiling silently rendered a partial
 * picture — GetNeighbors returned `truncated` and a continuation token that
 * nothing read.
 *
 * The page is merged only if the subgraph it continues is still the one on
 * screen — checked by identity rather than by taking the "subgraph" key, so
 * clicking Load more never cancels an endpoint the user just picked. A page of
 * endpoint A is never merged into B, nor into a refresh that replaced A.
 */
export async function loadMoreNeighbors() {
  const { rootId, subgraph, loadingMore } = snapshot();
  if (!rootId || !subgraph?.continuation || loadingMore) return;
  const current = begin("loadMore");
  set({ loadingMore: true });
  try {
    const next = await getCodeGraphNeighbors(rootId, NEIGHBOR_DEPTH, {
      continuation: subgraph.continuation,
    });
    const now = snapshot();
    if (!current() || now.subgraph !== subgraph || now.rootId !== rootId) return;
    set({ subgraph: mergeSubgraph(subgraph, next), subgraphPages: now.subgraphPages + 1 });
  } catch (e) {
    if (current() && snapshot().subgraph === subgraph) set({ error: errorMessage(e) });
  } finally {
    if (current()) set({ loadingMore: false });
  }
}

/** Click a node → open its inspector, anchored to that node on the canvas. */
export async function inspectNode(nodeId: string) {
  const { subgraph } = snapshot();
  const node = subgraph?.nodes.find((n) => n.id === nodeId);
  if (!node) return;
  const current = begin("inspect");
  set({ inspectorNodeId: nodeId, snippet: null, snippetError: "" });
  // A node with no source yet (a plan node still not_implemented) has an empty
  // filePath — let the Detail tab show its own empty state rather than asking
  // Go for a file that does not exist.
  if (!node.filePath) {
    set({ snippetLoading: false });
    return;
  }
  set({ snippetLoading: true });
  try {
    const snippet = await getCodeGraphCodeSnippet(node.filePath, node.line, node.endLine);
    if (current()) set({ snippet, snippetLoading: false });
  } catch (e) {
    if (current()) set({ snippetError: errorMessage(e), snippetLoading: false });
  }
}

export function closeInspector() {
  begin("inspect"); // a snippet still on its way belongs to nothing now
  set({ inspectorNodeId: null, snippet: null, snippetError: "", snippetLoading: false });
}

/**
 * Loads the plan and its edges together.
 *
 * Status is recomputed by Go on every read, so this is also how the canvas
 * picks up an agent's progress — no diffing here, and nothing cached.
 */
export async function loadPlan() {
  const current = begin("plan");
  set({ planBusy: true, planError: "" });
  try {
    const [plan, planEdges, commentCounts] = await Promise.all([
      getCodeGraphPlanProgress(),
      getCodeGraphPlanEdges(),
      countCodeGraphPlanNodeComments(),
    ]);
    if (current()) set({ plan, planEdges, commentCounts });
  } catch (e) {
    if (!current()) return;
    // Having no plan is the normal state for most projects, not a failure —
    // the empty state says what creates one. Anything else is a real error
    // and is shown as one, with a retry.
    set({ plan: null, planEdges: [], planError: isNoPlanError(e) ? "" : errorMessage(e) });
  } finally {
    if (current()) set({ planBusy: false });
  }
}

export async function approvePlan() {
  const current = begin("plan");
  set({ planBusy: true, planError: "" });
  try {
    await approveCodeGraphPlan();
  } catch (e) {
    if (current()) set({ planError: errorMessage(e), planBusy: false });
    return;
  }
  if (current()) await loadPlan();
}

export async function relinkPlanNode(planNodeID: string, fqn: string) {
  const current = begin("plan");
  set({ planBusy: true, planError: "" });
  try {
    await relinkCodeGraphPlanNode(planNodeID, fqn);
  } catch (e) {
    if (current()) set({ planError: errorMessage(e), planBusy: false });
    return;
  }
  // Status is derived from the FQN, so the whole plan is re-read rather than
  // patched — the relinked node may now resolve, and its edges with it.
  if (current()) await loadPlan();
}

export function setPlanCommentCount(planNodeId: string, count: number) {
  set({ commentCounts: { ...snapshot().commentCounts, [planNodeId]: count } });
}

/** Re-fetches `pages` pages of a subgraph, the way the user had loaded them. */
async function fetchPages(rootId: string, pages: number, current: () => boolean) {
  let sub: CodeGraphSubgraph = await getCodeGraphNeighbors(rootId, NEIGHBOR_DEPTH);
  let loaded = 1;
  while (loaded < pages && sub.continuation && current()) {
    const next = await getCodeGraphNeighbors(rootId, NEIGHBOR_DEPTH, { continuation: sub.continuation });
    sub = mergeSubgraph(sub, next);
    loaded += 1;
  }
  return { sub, loaded };
}

/**
 * Re-reads whatever is currently on screen after a background re-index.
 *
 * Everything here is derived from the index, so it goes stale silently — plan
 * status especially, since Go recomputes the diff on every read and the UI
 * would otherwise keep showing the answer from before the agent's last commit.
 *
 * The open file is deliberately NOT re-read: that buffer may hold unsaved
 * edits, and refreshing it would throw them away to fix a staleness the user
 * can see and control.
 */
export async function refreshAfterReindex() {
  const current = begin("refresh");
  try {
    await waitForCodeGraphIndexReady();
    if (!current()) return;
    const endpoints = await listCodeGraphEndpoints();
    if (!current()) return;
    set({ endpoints });
    await loadFiles();
    if (!current()) return;
    // Read after the waits, not before: the user may have picked another
    // endpoint (or loaded more of this one) while the index settled.
    const { rootId, subgraphPages } = snapshot();
    if (rootId) {
      const flow = begin("subgraph");
      const { sub, loaded } = await fetchPages(rootId, Math.max(1, subgraphPages), flow);
      if (current() && flow()) set({ subgraph: sub, subgraphPages: loaded });
    }
  } catch (e) {
    if (current()) set({ error: errorMessage(e) });
  }
  if (current() && snapshot().activeTab === "plan") {
    await loadPlan();
  }
}

async function readFileIntoEditor(path: string, line: number | null = null) {
  const current = begin("file");
  set({ fileError: "", pendingOpenFile: null, pendingOpenLine: null });
  try {
    const content = await readCodeGraphFile(path);
    if (!current()) return;
    set({
      selectedFile: path,
      fileRoot: snapshot().projectPath,
      selectedLine: line,
      fileContent: content,
      fileDirty: false,
    });
  } catch (e) {
    if (current()) set({ fileError: errorMessage(e) });
  }
}

/**
 * Opens a file in the editor — unless that would discard unsaved edits, in
 * which case it parks the request for the view's confirm dialog. Every way in
 * (tree, blast radius, the inspector's Edit) goes through here.
 */
export async function openFile(path: string, line: number | null = null) {
  const { fileDirty, selectedFile, fileRoot, projectPath } = snapshot();
  if (fileDirty) {
    // Re-opening the file already being edited is a no-op, not a revert —
    // unless the buffer is left over from another project.
    const sameBuffer = path === selectedFile && (!fileRoot || fileRoot === projectPath);
    if (sameBuffer) {
      if (line !== null) set({ selectedLine: line });
    } else {
      set({ pendingOpenFile: path, pendingOpenLine: line });
    }
    return;
  }
  await readFileIntoEditor(path, line);
}

/**
 * Opens a file an agent pointed at, in the agent's project: switches to that
 * project the same way the picker does (so its state resets and an unsaved
 * buffer is kept as the old project's), then goes through openFile, so the
 * discard prompt applies here too. `path` may be absolute or project-relative.
 */
export async function openFileInProject(root: string, path: string, line?: number) {
  const trimmedRoot = root.replace(/[\\/]+$/, "");
  const normalizedRoot = trimmedRoot.split("\\").join("/");
  const normalizedPath = path.split("\\").join("/");
  const relative = normalizedPath.startsWith(`${normalizedRoot}/`)
    ? normalizedPath.slice(normalizedRoot.length + 1)
    : normalizedPath;
  set({ activeTab: "files" });
  if (trimmedRoot !== snapshot().projectPath) {
    await reindex(trimmedRoot);
    // A failed open is already on screen as the view's error; a superseded
    // one belongs to whoever superseded it.
    if (snapshot().projectPath !== trimmedRoot || snapshot().error) return;
  }
  await openFile(relative, line ?? null);
}

export async function confirmDiscardAndOpen() {
  const { pendingOpenFile: path, pendingOpenLine: line } = snapshot();
  if (!path) return;
  set({ fileDirty: false });
  await readFileIntoEditor(path, line);
}

export function cancelPendingOpen() {
  set({ pendingOpenFile: null, pendingOpenLine: null });
}

/** Closes a buffer left over from a project switch, edits and all. */
export function closeFile() {
  begin("file");
  set({ selectedFile: null, fileRoot: "", selectedLine: null, fileContent: "", fileDirty: false, fileError: "" });
}

export function editFileContent(content: string) {
  set({ fileContent: content, fileDirty: true });
}

// Reads the live values off the store rather than through refs mirrored during
// render, so the CodeMirror keymap can call it without re-binding.
export async function saveCurrentFile() {
  const { selectedFile, fileContent, fileRoot, projectPath } = snapshot();
  if (!selectedFile) return;
  if (fileRoot && fileRoot !== projectPath) {
    set({
      fileError: `This buffer belongs to ${fileRoot}, not the open project — copy your edits out; it cannot be saved here.`,
    });
    return;
  }
  try {
    await writeCodeGraphFile(selectedFile, fileContent, fileRoot || projectPath);
    // Edits typed while the write was in flight are still unsaved.
    if (snapshot().selectedFile === selectedFile && snapshot().fileContent === fileContent) {
      set({ fileDirty: false, fileError: "" });
    }
  } catch (e) {
    set({
      fileError: isProjectChangedError(e)
        ? "Not saved: the active project changed since this file was opened. Your edits are still here — copy them before closing the file."
        : errorMessage(e),
    });
  }
}
