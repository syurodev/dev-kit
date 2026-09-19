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
  codeGraphIndexInfo,
  codeGraphGitStatus,
  readCodeGraphFile,
  writeCodeGraphFile,
  onCodeGraphIndexUpdated,
  onCodeGraphProjectChanged,
  type CodeGraphEndpoint,
  type CodeGraphSubgraph,
  type CodeGraphSymbolMatch,
} from "@/bridge/codegraph";
import {
  approveCodeGraphPlan,
  countCodeGraphPlanNodeComments,
  relinkCodeGraphPlanNode,
  getCodeGraphPlanEdges,
  getCodeGraphPlanProgress,
} from "@/bridge/codegraphPlan";
import { errorMessage, toAppError } from "@/bridge/errors";
import { codeGraphRequests, useCodeGraphStore, type FileBuffer, type FlowDoc } from "./codegraphStore";
import { isNoPlanError, isProjectChangedError } from "./codegraphErrors";
import { mergeSubgraph } from "./subgraph";
import { rememberProject } from "./recentProjects";
import { useAgentSessionsStore } from "@/stores/agentSessions";
import { moduleTabs, useWorkbenchTabsStore } from "@/stores/workbenchTabs";
import type { Placement } from "@/workbench/intents";
import {
  MODULE,
  closeProjectDocuments,
  describeDocument,
  keepTab,
  openEndpointFlow,
  openFileDoc,
  openPlanDoc,
  openNodeFlow,
} from "./documents";
import { symbolDisplay } from "./sidebar/sidebarModel";

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

/**
 * The sidebar's line under the project name and the Files tab's git marks.
 * Neither may break anything: a failed read leaves the last answer in place.
 */
export async function loadSidebarMeta() {
  const current = begin("meta");
  const [info, git] = await Promise.allSettled([codeGraphIndexInfo(), codeGraphGitStatus()]);
  if (!current()) return;
  set({
    ...(info.status === "fulfilled" ? { indexInfo: info.value } : {}),
    ...(git.status === "fulfilled" ? { gitStatus: git.value } : {}),
  });
}

async function loadFiles() {
  const current = begin("files");
  try {
    const files = await listCodeGraphFiles();
    if (current()) set({ files });
  } catch (e) {
    // The tree keeps what it had: an empty rail would read as an empty project.
    if (current()) console.warn("codegraph: could not list files", errorMessage(e));
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

const EMPTY_FLOW: FlowDoc = { subgraph: null, pages: 0, loading: false, loadingMore: false, error: "" };

function patchFlow(root: string, patch: Partial<FlowDoc>) {
  const flows = snapshot().flows;
  set({ flows: { ...flows, [root]: { ...(flows[root] ?? EMPTY_FLOW), ...patch } } });
}

function patchBuffer(path: string, patch: Partial<FileBuffer>) {
  const buffers = snapshot().buffers;
  const base: FileBuffer = buffers[path] ?? { content: "", root: snapshot().projectPath, dirty: false, loading: false, error: "", line: null, revealToken: 0 };
  set({ buffers: { ...buffers, [path]: { ...base, ...patch } } });
}

/** The documents open in Code Graph's tabs, by kind. */
function openDocuments() {
  const tabs = moduleTabs(useWorkbenchTabsStore.getState().modules, MODULE).openTabs.map(describeDocument);
  return {
    flows: new Set(tabs.flatMap((d) => (d.kind === "flow" && d.root ? [d.root] : []))),
    files: new Set(tabs.flatMap((d) => (d.kind === "file" && d.path ? [d.path] : []))),
    plan: tabs.some((d) => d.kind === "plan"),
  };
}

/**
 * Drops the state of documents whose tab is gone: a flow nobody is looking
 * at, a clean buffer. A buffer with unsaved edits is kept — closing a tab is
 * not "throw my edits away", and opening the file again finds them.
 */
function pruneClosedDocuments() {
  const open = openDocuments();
  const { flows, buffers, inspector } = snapshot();
  const keptFlows = Object.fromEntries(Object.entries(flows).filter(([root]) => open.flows.has(root)));
  const keptBuffers = Object.fromEntries(
    Object.entries(buffers).filter(([path, b]) => open.files.has(path) || b.dirty),
  );
  const patch: Record<string, unknown> = {};
  if (Object.keys(keptFlows).length !== Object.keys(flows).length) patch.flows = keptFlows;
  if (Object.keys(keptBuffers).length !== Object.keys(buffers).length) patch.buffers = keptBuffers;
  if (inspector && !open.flows.has(inspector.flow)) Object.assign(patch, { inspector: null, snippet: null });
  if (Object.keys(patch).length > 0) set(patch);
}

/**
 * Drops everything that belongs to the previous project and supersedes every
 * request still in flight for it — and closes its documents: a flow of the
 * old project drawn under the new one's name would be a lie.
 *
 * An unsaved buffer is the one exception: throwing away someone's edits
 * because an agent switched projects over MCP would be data loss. Its tab
 * stays with its old root, which the view shows as "from another project"
 * and which saving refuses (and Go refuses too), so it can be copied out but
 * never written into the new project at the same relative path.
 */
function switchProject(root: string) {
  codeGraphRequests.invalidateAll();
  const dirty = Object.fromEntries(Object.entries(snapshot().buffers).filter(([, b]) => b.dirty));
  closeProjectDocuments((path) => path in dirty);
  set({
    projectPath: root,
    endpoints: [],
    files: [],
    flows: {},
    buffers: dirty,
    busy: false,
    status: "",
    error: "",
    errorCode: "",
    inspector: null,
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
    indexInfo: null,
    gitStatus: {},
    collapsedGroups: [],
    fileExpanded: [],
  });
}

export async function reindex(path: string) {
  const sameProject = path === snapshot().projectPath;
  if (!sameProject) switchProject(path);
  const current = begin("reindex");
  set({
    busy: true,
    status: "Indexing…",
    error: "",
    errorCode: "",
    indexInfo: { ...(snapshot().indexInfo ?? { indexedAt: 0, error: "", modules: [], language: "", branch: "" }), indexing: true },
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
    rememberProject(path);
    // The flows still open were drawn from the index just replaced.
    if (sameProject) void refreshOpenFlows();
  } catch (e) {
    if (!current()) return;
    const failure = toAppError(e);
    set({ endpoints: [], files: [], status: "", error: failure.message, errorCode: failure.code });
  } finally {
    // A superseded reindex leaves busy to whoever superseded it: a project
    // switch resets it, and a newer reindex set it itself.
    if (current()) {
      set({ busy: false });
      void loadSidebarMeta();
    }
  }
}

export async function pickAndOpenProject() {
  set({ error: "" });
  let chosen: string;
  try {
    chosen = (await Dialogs.OpenFile({
      CanChooseDirectories: true,
      CanChooseFiles: false,
      Title: "Open project",
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
 * Re-indexes the active project from anywhere — the command palette lists
 * every module's commands, so this can run before any Code Graph surface has
 * mounted and the store has learned which project that is. Go is the
 * authority on it, so Go is asked.
 *
 * With no project at all it opens the picker rather than doing nothing: a
 * command that silently does nothing reads as broken.
 */
export async function reindexActiveProject() {
  ensureStarted();
  if (snapshot().busy) return;
  let root = snapshot().projectPath;
  if (!root) {
    try {
      root = await codeGraphProjectRoot();
    } catch {
      root = "";
    }
  }
  if (!root) {
    await pickAndOpenProject();
    return;
  }
  await reindex(root);
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

/** Adopt the project Go restored, if the store has none yet. */
async function restoreProject() {
  const root = await codeGraphProjectRoot();
  if (!root || snapshot().projectPath) return;
  set({ projectPath: root, status: "Indexing…" });
  await refreshAfterReindex();
  if (snapshot().projectPath === root) {
    set({ status: "" });
    rememberProject(root);
  }
}

let started = false;
let restored = false;

/**
 * Wires the module up once, from whichever of its surfaces mounts first — the
 * rail, the home view, a restored flow or file tab. None of them owns the
 * project, so none of them can be the one place this lives.
 *
 * The subscriptions are for the app's lifetime, like the store they write to.
 */
export function ensureStarted() {
  if (!started) {
    started = true;
    onCodeGraphIndexUpdated(() => void refreshAfterReindex());
    // The project can change without this module doing it — an agent
    // switching it over MCP. Everything project-scoped is reset when it does.
    onCodeGraphProjectChanged((root) => void handleProjectChanged(root));
    useWorkbenchTabsStore.subscribe((state, prev) => {
      if (state.modules[MODULE] !== prev.modules[MODULE]) pruneClosedDocuments();
    });
    // Probe once. Cheap enough, and the answer cannot change without someone
    // installing a JDK, which is not something to poll for.
    void loadAnalyzerStatus();
  }
  if (!restored) {
    restored = true;
    // A locked vault fails this; the next surface to mount tries again.
    restoreProject().catch(() => {
      restored = false;
    });
  }
}

/** Fetches one flow's graph, unless it is already on screen. */
export async function loadFlow(root: string, force = false) {
  const flow = snapshot().flows[root];
  if (!force && flow && (flow.subgraph || flow.loading)) return;
  const current = begin(`flow:${root}`);
  patchFlow(root, { loading: true, error: "", loadingMore: false });
  try {
    const sub = await getCodeGraphNeighbors(root, NEIGHBOR_DEPTH);
    if (current()) patchFlow(root, { subgraph: sub, pages: 1, loading: false });
  } catch (e) {
    if (current()) patchFlow(root, { subgraph: null, loading: false, error: errorMessage(e) });
  }
}

export type OpenDocOptions = { preview?: boolean; beside?: boolean };

/** An endpoint's call flow, in the preview tab unless told otherwise. */
export function viewFlow(endpoint: CodeGraphEndpoint, options?: OpenDocOptions) {
  openEndpointFlow(endpoint, options);
  void loadFlow(endpoint.entryNode);
}

/**
 * A symbol from the sidebar's @ search, drawn as the root of its own graph —
 * a class or method reached without an endpoint in front of it.
 */
export function viewSymbol(match: CodeGraphSymbolMatch, options?: OpenDocOptions) {
  openNodeFlow(match.nodeId, symbolDisplay(match).name, options);
  void loadFlow(match.nodeId);
}

/**
 * Fetches the next page of a truncated subgraph and merges it in. Without
 * this, a graph over the backend's ceiling silently rendered a partial
 * picture — GetNeighbors returned `truncated` and a continuation token that
 * nothing read.
 *
 * The page is merged only if the subgraph it continues is still the one in
 * that flow — checked by identity, so a refresh that replaced it wins.
 */
export async function loadMoreNeighbors(root: string) {
  const flow = snapshot().flows[root];
  if (!flow?.subgraph?.continuation || flow.loadingMore) return;
  const subgraph = flow.subgraph;
  const current = begin(`more:${root}`);
  patchFlow(root, { loadingMore: true });
  try {
    const next = await getCodeGraphNeighbors(root, NEIGHBOR_DEPTH, { continuation: subgraph.continuation });
    const now = snapshot().flows[root];
    if (!current() || now?.subgraph !== subgraph) return;
    patchFlow(root, { subgraph: mergeSubgraph(subgraph, next), pages: now.pages + 1 });
  } catch (e) {
    if (current() && snapshot().flows[root]?.subgraph === subgraph) patchFlow(root, { error: errorMessage(e) });
  } finally {
    if (current()) patchFlow(root, { loadingMore: false });
  }
}

/** Click a node → open its inspector, a context pane of the flow it is in. */
export async function inspectNode(flow: string, nodeId: string) {
  const node = snapshot().flows[flow]?.subgraph?.nodes.find((n) => n.id === nodeId);
  if (!node) return;
  const current = begin("inspect");
  set({ inspector: { flow, nodeId }, snippet: null, snippetError: "" });
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
  set({ inspector: null, snippet: null, snippetError: "", snippetLoading: false });
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

/** The plan's canvas as a tab, with `nodeId` picked on it. */
export function showPlan(nodeId?: string, options?: OpenDocOptions) {
  if (nodeId) set({ selectedPlanNodeId: nodeId });
  openPlanDoc(options);
  if (!snapshot().plan && !snapshot().planBusy) void loadPlan();
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

/** Re-draws every open flow from the current index, as many pages as it had. */
async function refreshOpenFlows() {
  const open = openDocuments().flows;
  await Promise.all(
    [...open].map(async (root) => {
      const pages = Math.max(1, snapshot().flows[root]?.pages ?? 1);
      const current = begin(`flow:${root}`);
      try {
        const { sub, loaded } = await fetchPages(root, pages, current);
        if (current()) patchFlow(root, { subgraph: sub, pages: loaded, loading: false, error: "" });
      } catch (e) {
        // A node the re-index removed: the tab says so rather than keeping
        // a graph of code that is gone.
        if (current()) patchFlow(root, { loading: false, error: errorMessage(e) });
      }
    }),
  );
}

/**
 * Re-reads whatever is open after a background re-index.
 *
 * Everything here is derived from the index, so it goes stale silently — plan
 * status especially, since Go recomputes the diff on every read and the UI
 * would otherwise keep showing the answer from before the agent's last commit.
 *
 * Open files are deliberately NOT re-read: a buffer may hold unsaved edits,
 * and refreshing it would throw them away to fix a staleness the user can see
 * and control.
 */
export async function refreshAfterReindex() {
  const current = begin("refresh");
  try {
    await waitForCodeGraphIndexReady();
    if (!current()) return;
    const endpoints = await listCodeGraphEndpoints();
    if (!current()) return;
    set({ endpoints, error: "", errorCode: "" });
    await loadFiles();
    if (!current()) return;
    await refreshOpenFlows();
  } catch (e) {
    if (current()) set({ error: errorMessage(e) });
  }
  if (!current()) return;
  void loadSidebarMeta();
  if (openDocuments().plan || snapshot().activeTab === "plan") await loadPlan();
}

/** Reads a file into its buffer — unless the buffer holds edits, which win. */
async function loadBuffer(path: string, line: number | null, navigating: boolean) {
  const existing = snapshot().buffers[path];
  // Only a navigation asks the caret to move. reloadFile passes the line the
  // buffer already holds, and re-reading a file is not a request to go there
  // again.
  const reveal = navigating && line !== null ? { line, revealToken: (existing?.revealToken ?? 0) + 1 } : { line };
  if (existing?.dirty) {
    if (line !== null) patchBuffer(path, reveal);
    return;
  }
  const current = begin(`file:${path}`);
  patchBuffer(path, { loading: !existing, error: "", ...reveal });
  try {
    const content = await readCodeGraphFile(path);
    // Typed into while the read was in flight: the typing wins.
    if (!current() || snapshot().buffers[path]?.dirty) return;
    patchBuffer(path, { content, root: snapshot().projectPath, dirty: false, loading: false });
  } catch (e) {
    if (current()) patchBuffer(path, { loading: false, error: errorMessage(e) });
  }
}

/**
 * Opens a file as a tab. Every way in — the tree, blast radius, the
 * inspector's Edit, go-to-definition, an agent — goes through here. A file with unsaved edits
 * opens on its edits: nothing is ever discarded to open something.
 */
export function openFile(path: string, line: number | null = null, options?: OpenDocOptions) {
  openFileDoc(path, options);
  void loadBuffer(path, line, true);
}

/** A buffer read again from disk: after a save elsewhere, or to drop a failed read. */
export function reloadFile(path: string) {
  if (snapshot().buffers[path]?.dirty) return;
  void loadBuffer(path, snapshot().buffers[path]?.line ?? null, false);
}

/**
 * Opens a file an agent pointed at, in the agent's project: switches to that
 * project the same way the picker does (so its state resets and an unsaved
 * buffer is kept as the old project's), then goes through openFile.
 * `path` may be absolute or project-relative.
 */
export async function openFileInProject(root: string, path: string, line?: number, placement: Placement = "replace") {
  const trimmedRoot = root.replace(/[\\/]+$/, "");
  const normalizedRoot = trimmedRoot.split("\\").join("/");
  const normalizedPath = path.split("\\").join("/");
  const relative = normalizedPath.startsWith(`${normalizedRoot}/`)
    ? normalizedPath.slice(normalizedRoot.length + 1)
    : normalizedPath;
  set({ activeTab: "files" });
  if (trimmedRoot && trimmedRoot !== snapshot().projectPath) {
    await reindex(trimmedRoot);
    // A failed open is already on screen as the home view's error; a
    // superseded one belongs to whoever superseded it.
    if (snapshot().projectPath !== trimmedRoot || snapshot().error) return;
  }
  openFile(relative, line ?? null, { preview: false, beside: placement === "beside" });
}

/** Throws a buffer's edits away and closes its tab. */
export function discardFile(path: string) {
  begin(`file:${path}`);
  const { [path]: _gone, ...rest } = snapshot().buffers;
  set({ buffers: rest });
  const tab = moduleTabs(useWorkbenchTabsStore.getState().modules, MODULE).openTabs.find(
    (t) => describeDocument(t).path === path,
  );
  if (tab) useWorkbenchTabsStore.getState().closeTab(tab.id);
}

export function editFile(path: string, content: string, tabId: string) {
  patchBuffer(path, { content, dirty: true });
  // An edited preview is not a preview any more: the next click must not
  // replace it.
  keepTab(tabId);
}

// Reads the live values off the store rather than through refs mirrored during
// render, so the CodeMirror keymap can call it without re-binding.
export async function saveFile(path: string) {
  const buffer = snapshot().buffers[path];
  const { projectPath } = snapshot();
  if (!buffer) return;
  if (buffer.root && buffer.root !== projectPath) {
    patchBuffer(path, {
      error: `This buffer belongs to ${buffer.root}, not the open project — copy your edits out; it cannot be saved here.`,
    });
    return;
  }
  const { content } = buffer;
  try {
    await writeCodeGraphFile(path, content, buffer.root || projectPath);
    // Edits typed while the write was in flight are still unsaved.
    if (snapshot().buffers[path]?.content === content) patchBuffer(path, { dirty: false, error: "" });
    void loadSidebarMeta();
  } catch (e) {
    patchBuffer(path, {
      error: isProjectChangedError(e)
        ? "Not saved: the active project changed since this file was opened. Your edits are still here — copy them before closing the file."
        : errorMessage(e),
    });
  }
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
  }
  agents.setChatOpen(true);
}
