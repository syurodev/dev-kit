import {
  getCodeGraphCodeSnippet,
  getCodeGraphNeighbors,
  type CodeGraphEntryPoint,
  type CodeGraphSubgraph,
  type CodeGraphSymbolMatch,
} from "@/bridge/codegraph";
import { errorMessage } from "@/bridge/errors";
import type { FlowDoc } from "./codegraphStore";
import { mergeSubgraph } from "./subgraph";
import { begin, folderById, set, snapshot } from "./storeAccess";
import { documentsOf, openEntryPointFlow, openNodeFlow, type DocOwner, type OpenOptions } from "./documents";
import { symbolDisplay } from "./sidebar/sidebarModel";
import { flowKey } from "./workspace/identity";
import { captureDeparture, focusedGroupId, recordArrival } from "./navigation";

/**
 * Flows: the call graph of one folder's index, drawn from a root node.
 *
 * A flow names its folder in its tab params and in its store key, and every
 * request goes to that folder's index by its registered path. Two folders can
 * have a node of the same id; each draws its own graph, and a re-index of one
 * redraws only its flows.
 */

const NEIGHBOR_DEPTH = 3;
const EMPTY_FLOW = (folderId: string): FlowDoc => ({ folderId, subgraph: null, pages: 0, loading: false, loadingMore: false, error: "" });

function patchFlow(key: string, folderId: string, patch: Partial<FlowDoc>) {
  const flows = snapshot().flows;
  set({ flows: { ...flows, [key]: { ...(flows[key] ?? EMPTY_FLOW(folderId)), ...patch } } });
}

/** The registered path of a folder, which names it to the graph queries. */
function servicePathOf(folderId: string): string | null {
  return folderById(folderId)?.path ?? null;
}

/** Fetches one flow's graph, unless it is already on screen. */
export async function loadFlow(folderId: string, root: string, force = false): Promise<void> {
  const key = flowKey(folderId, root);
  const flow = snapshot().flows[key];
  if (!force && flow && (flow.subgraph || flow.loading)) return;
  const servicePath = servicePathOf(folderId);
  if (!servicePath) {
    patchFlow(key, folderId, { subgraph: null, loading: false, error: "This flow's folder is not part of the open workspace." });
    return;
  }
  const current = begin(`flow:${key}`);
  patchFlow(key, folderId, { loading: true, error: "", loadingMore: false });
  try {
    const sub = await getCodeGraphNeighbors(root, NEIGHBOR_DEPTH, undefined, servicePath);
    if (current()) patchFlow(key, folderId, { subgraph: sub, pages: 1, loading: false });
  } catch (e) {
    if (current()) patchFlow(key, folderId, { subgraph: null, loading: false, error: errorMessage(e) });
  }
}

/** An entry point's call flow, in the preview tab unless told otherwise. */
export function viewFlow(owner: DocOwner, entry: CodeGraphEntryPoint, options?: OpenOptions) {
  captureDeparture();
  openEntryPointFlow(owner, entry, options);
  recordFlowArrival(owner, { root: entry.entryNode, entryPoint: entry.id, title: `${entry.verb} ${entry.target}` });
  void loadFlow(owner.folderId, entry.entryNode);
}

/** The flow just opened is a place in the focused group's history, so Back can leave it and Forward return. */
function recordFlowArrival(owner: DocOwner, flow: { root: string; entryPoint?: string; title: string }) {
  const groupId = focusedGroupId();
  if (groupId) recordArrival(groupId, { ...owner, path: "", line: 1, column: 1, flow });
}

/**
 * A symbol from the sidebar's @ search, drawn as the root of its own graph — a
 * class or method reached without an entry point in front of it.
 */
export function viewSymbol(owner: DocOwner, match: CodeGraphSymbolMatch, options?: OpenOptions) {
  captureDeparture();
  openNodeFlow(owner, match.nodeId, symbolDisplay(match).name, options);
  recordFlowArrival(owner, { root: match.nodeId, title: symbolDisplay(match).name });
  void loadFlow(owner.folderId, match.nodeId);
}

/**
 * Fetches the next page of a truncated subgraph and merges it in. The page is
 * merged only if the subgraph it continues is still the one in that flow —
 * checked by identity, so a refresh that replaced it wins.
 */
export async function loadMoreNeighbors(folderId: string, root: string): Promise<void> {
  const key = flowKey(folderId, root);
  const flow = snapshot().flows[key];
  const servicePath = servicePathOf(folderId);
  if (!flow?.subgraph?.continuation || flow.loadingMore || !servicePath) return;
  const subgraph = flow.subgraph;
  const current = begin(`more:${key}`);
  patchFlow(key, folderId, { loadingMore: true });
  try {
    const next = await getCodeGraphNeighbors(root, NEIGHBOR_DEPTH, { continuation: subgraph.continuation }, servicePath);
    const now = snapshot().flows[key];
    if (!current() || now?.subgraph !== subgraph) return;
    patchFlow(key, folderId, { subgraph: mergeSubgraph(subgraph, next), pages: now.pages + 1 });
  } catch (e) {
    if (current() && snapshot().flows[key]?.subgraph === subgraph) patchFlow(key, folderId, { error: errorMessage(e) });
  } finally {
    if (current()) patchFlow(key, folderId, { loadingMore: false });
  }
}

/** Click a node → open its inspector, a context pane of the flow it is in. */
export async function inspectNode(folderId: string, root: string, nodeId: string): Promise<void> {
  const key = flowKey(folderId, root);
  const node = snapshot().flows[key]?.subgraph?.nodes.find((n) => n.id === nodeId);
  if (!node) return;
  const current = begin("inspect");
  set({ inspector: { flow: key, nodeId }, snippet: null, snippetError: "" });
  // A node with no source yet has an empty filePath — let the Detail tab show
  // its own empty state rather than asking Go for a file that does not exist.
  if (!node.filePath) {
    set({ snippetLoading: false });
    return;
  }
  const servicePath = servicePathOf(folderId);
  if (!servicePath) return;
  set({ snippetLoading: true });
  try {
    const snippet = await getCodeGraphCodeSnippet(node.filePath, node.line, node.endLine, servicePath);
    if (current()) set({ snippet, snippetLoading: false });
  } catch (e) {
    if (current()) set({ snippetError: errorMessage(e), snippetLoading: false });
  }
}

export function closeInspector() {
  begin("inspect"); // a snippet still on its way belongs to nothing now
  set({ inspector: null, snippet: null, snippetError: "", snippetLoading: false });
}

/** Re-fetches `pages` pages of a subgraph, the way the user had loaded them. */
async function fetchPages(servicePath: string, rootId: string, pages: number, current: () => boolean) {
  let sub: CodeGraphSubgraph = await getCodeGraphNeighbors(rootId, NEIGHBOR_DEPTH, undefined, servicePath);
  let loaded = 1;
  while (loaded < pages && sub.continuation && current()) {
    const next = await getCodeGraphNeighbors(rootId, NEIGHBOR_DEPTH, { continuation: sub.continuation }, servicePath);
    sub = mergeSubgraph(sub, next);
    loaded += 1;
  }
  return { sub, loaded };
}

/** Re-draws the open flows of one folder from its current index, as many pages as each had. */
export async function refreshFolderFlows(folderId: string): Promise<void> {
  const servicePath = servicePathOf(folderId);
  if (!servicePath) return;
  const open = documentsOf().flatMap(({ doc }) => (doc.kind === "flow" && doc.folderId === folderId && doc.root ? [doc.root] : []));
  await Promise.all(
    [...new Set(open)].map(async (root) => {
      const key = flowKey(folderId, root);
      if (!snapshot().flows[key]) return;
      const pages = Math.max(1, snapshot().flows[key]?.pages ?? 1);
      const current = begin(`flow:${key}`);
      try {
        const { sub, loaded } = await fetchPages(servicePath, root, pages, current);
        if (current()) patchFlow(key, folderId, { subgraph: sub, pages: loaded, loading: false, error: "" });
      } catch (e) {
        // A node the re-index removed: the tab says so rather than keeping
        // a graph of code that is gone.
        if (current()) patchFlow(key, folderId, { loading: false, error: errorMessage(e) });
      }
    }),
  );
}
