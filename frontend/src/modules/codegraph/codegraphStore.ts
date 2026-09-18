import { create } from "zustand";
import { createRequestGuard } from "./requestGuard";
import type {
  CodeGraphPlanEdge,
  CodeGraphPlanProgress,
} from "@/bridge/codegraphPlan";
import type {
  CodeGraphCodeSnippet,
  CodeGraphEndpoint,
  CodeGraphFileEntry,
  CodeGraphSubgraph,
  CodeGraphToolchainStatus,
} from "@/bridge/codegraph";

type CodeGraphState = {
  projectPath: string;
  endpoints: CodeGraphEndpoint[];
  subgraph: CodeGraphSubgraph | null;
  rootId: string | null;
  /** The endpoint picked in the sidebar; several endpoints can share one entry node. */
  rootEndpointId: string | null;
  status: string;
  error: string;
  /**
   * The bridge error code behind `error`, or "". "unsupported_project" is the
   * one the view branches on: picking the wrong folder needs a different offer
   * from an analysis that broke, and a retry button on a folder that will never
   * analyze is a button that cannot work.
   */
  errorCode: string;
  busy: boolean;

  /** True while fetching the next page of a truncated subgraph. */
  loadingMore: boolean;
  /**
   * How many pages of the subgraph are on screen. A background re-index
   * re-fetches this many, so an index update does not quietly undo every
   * "Load more" the user clicked.
   */
  subgraphPages: number;

  /** The node whose inspector is open, or null. Replaces snippetOpen: the
   *  inspector is anchored to a specific node, so "which node" IS the
   *  open/closed state. */
  inspectorNodeId: string | null;
  snippet: CodeGraphCodeSnippet | null;
  snippetLoading: boolean;
  snippetError: string;

  activeTab: "endpoints" | "files" | "plan";

  /** Per-node status plus the aggregate, recomputed by Go on every read. */
  plan: CodeGraphPlanProgress | null;
  planEdges: CodeGraphPlanEdge[];
  planBusy: boolean;
  /** Which plan node the canvas should highlight, chosen from the sidebar. */
  selectedPlanNodeId: string | null;
  /** Which plan node has its comment thread open, or null. */
  commentsPlanNodeId: string | null;
  /** Thread length per plan node, for the node badge. */
  commentCounts: Record<string, number>;
  planError: string;
  files: CodeGraphFileEntry[];
  selectedFile: string | null;
  /**
   * The project root `selectedFile` was opened under. Saving sends it to Go,
   * which refuses the write if the active project has moved since; the view
   * compares it with `projectPath` to mark a buffer left over from a switch.
   * Filled in by set() whenever selectedFile changes without it — see below.
   */
  fileRoot: string;
  /** A file the user asked to open while the current one had unsaved edits. */
  pendingOpenFile: string | null;
  selectedLine: number | null;
  fileContent: string;
  fileDirty: boolean;
  fileError: string;

  /** Filters the sidebar's visible list; applies to whichever tab is active. */
  query: string;

  /**
   * Per-language analyzer readiness, or null until the first probe answers.
   * Null and "all ready" both render the normal module — the setup panel only
   * takes over once we know an analyzer is genuinely missing, so a slow probe
   * never flashes it at someone whose analyzer is fine.
   */
  analyzerStatuses: CodeGraphToolchainStatus[] | null;

  set: (patch: Partial<CodeGraphState>) => void;
};

/**
 * CodeGraph's view state lives outside the component on purpose.
 *
 * A Dock visit opens the module as the single shared *preview* tab, and openTab
 * replaces whichever preview tab is there (stores/workbenchTabs.ts). Switching
 * modules therefore drops CodeGraph's tab and unmounts its subtree, so useState
 * in the view would come back as an empty "Open Java project…" screen even
 * though Go still has the project indexed and watched.
 *
 * The Go service restores the last selected project on startup. The view
 * reads that selection on mount and waits for its index before loading data;
 * graph results themselves are never restored from frontend storage.
 */
export const useCodeGraphStore = create<CodeGraphState>((set, get) => ({
  projectPath: "",
  endpoints: [],
  subgraph: null,
  rootId: null,
  rootEndpointId: null,
  status: "",
  error: "",
  errorCode: "",
  busy: false,
  loadingMore: false,
  subgraphPages: 0,

  inspectorNodeId: null,
  snippet: null,
  snippetLoading: false,
  snippetError: "",

  activeTab: "endpoints",
  plan: null,
  planEdges: [],
  planBusy: false,
  selectedPlanNodeId: null,
  commentsPlanNodeId: null,
  commentCounts: {},
  planError: "",
  files: [],
  selectedFile: null,
  fileRoot: "",
  pendingOpenFile: null,
  selectedLine: null,
  fileContent: "",
  fileDirty: false,
  fileError: "",
  query: "",
  analyzerStatuses: null,

  // Opening a file from outside this module (the agent companion sets
  // selectedFile directly) must still record which project it belongs to, or a
  // later save would carry the previous file's root. So a patch that changes
  // the file without naming its root takes the project it lands in.
  set: (patch) => {
    if ("selectedFile" in patch && !("fileRoot" in patch)) {
      set({ ...patch, fileRoot: patch.selectedFile ? (patch.projectPath ?? get().projectPath) : "" });
      return;
    }
    set(patch);
  },
}));

/**
 * One guard for every CodeGraph request, module-level like the store itself:
 * the actions outlive the view (see above), and so do their responses.
 * Keys are per action; a project switch invalidates all of them.
 */
export const codeGraphRequests = createRequestGuard();
