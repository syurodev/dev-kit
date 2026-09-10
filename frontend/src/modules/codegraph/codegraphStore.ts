import { create } from "zustand";
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
 * Deliberately not persisted: the Go service holds the active project in
 * memory too, so this dying with the process keeps the two sides consistent.
 * Persisting it would resurrect a project path that Go no longer has open.
 */
export const useCodeGraphStore = create<CodeGraphState>((set) => ({
  projectPath: "",
  endpoints: [],
  subgraph: null,
  rootId: null,
  status: "",
  error: "",
  errorCode: "",
  busy: false,
  loadingMore: false,

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
  fileContent: "",
  fileDirty: false,
  fileError: "",
  query: "",
  analyzerStatuses: null,

  set: (patch) => set(patch),
}));
