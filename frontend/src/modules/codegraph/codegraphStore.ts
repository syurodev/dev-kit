import { create } from "zustand";
import { createRequestGuard } from "./requestGuard";
import type {
  CodeGraphPlanEdge,
  CodeGraphPlanProgress,
} from "@/bridge/codegraphPlan";
import type {
  CodeGraphCodeSnippet,
  CodeGraphEntryPoint,
  CodeGraphFileEntry,
  CodeGraphGitState,
  CodeGraphIndexInfo,
  CodeGraphSubgraph,
  CodeGraphToolchainStatus,
} from "@/bridge/codegraph";

/**
 * One flow document: the graph drawn from a root node, as a tab of its own
 * (documents.ts). Keyed by root node id, so two tabs of entry points handled by
 * the same method share one fetch.
 */
export type FlowDoc = {
  subgraph: CodeGraphSubgraph | null;
  /**
   * How many pages are on screen. A background re-index re-fetches this many,
   * so an index update does not quietly undo every "Load more" clicked.
   */
  pages: number;
  loading: boolean;
  /** True while fetching the next page of a truncated subgraph. */
  loadingMore: boolean;
  error: string;
};

/**
 * One file document's buffer, keyed by project-relative path.
 *
 * It outlives its tab on purpose: closing the tab of a file with unsaved
 * edits keeps the edits, and opening the file again finds them. Only saving,
 * discarding or a clean project switch drops a buffer.
 */
export type FileBuffer = {
  content: string;
  /**
   * The project root it was read from. Saving sends it to Go, which refuses
   * the write if the active project has moved since; the view compares it
   * with projectPath to mark a buffer left over from a switch.
   */
  root: string;
  dirty: boolean;
  loading: boolean;
  error: string;
  /** The line to reveal, if it was opened at one. */
  line: number | null;
  /**
   * Bumped each time `line` is set by a navigation, even to the line it
   * already held. Going to the same definition twice is two requests, and the
   * editor's reveal is keyed on (line, token) so it sees both (RevealTarget in
   * CodeEditor.tsx).
   */
  revealToken: number;
};

/** One task on a plan node, narrowed to what a chip draws. */
export type PlanTaskChip = { id: string; key: string; title: string; status: string };

type CodeGraphState = {
  projectPath: string;
  entryPoints: CodeGraphEntryPoint[];
  status: string;
  /** Why the project could not be analyzed, or "". */
  error: string;
  /**
   * The bridge error code behind `error`, or "". "unsupported_project" is the
   * one the view branches on: picking the wrong folder needs a different offer
   * from an analysis that broke, and a retry button on a folder that will never
   * analyze is a button that cannot work.
   */
  errorCode: string;
  busy: boolean;

  /** Open flow documents, by root node id. */
  flows: Record<string, FlowDoc>;
  /** Open (or edited) file documents, by project-relative path. */
  buffers: Record<string, FileBuffer>;

  /**
   * The node whose inspector is open, and the flow it was picked in. The
   * inspector is a context pane of that flow: it shows while that flow is the
   * document in front, and goes when another document comes forward.
   */
  inspector: { flow: string; nodeId: string } | null;
  snippet: CodeGraphCodeSnippet | null;
  snippetLoading: boolean;
  snippetError: string;

  /** The sidebar's section. Documents are tabs now; this only picks the rail's list. */
  activeTab: "entries" | "files" | "plan";

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
  /**
   * The tasks implementing each plan node, for the chips on the canvas.
   *
   * Code Graph does not own this and does not store it: the link lives on the
   * task, and this is the answer to one bounded query, refreshed with the
   * plan. An empty record is the normal state — most projects have no board.
   */
  planTasks: Record<string, PlanTaskChip[]>;
  planError: string;
  files: CodeGraphFileEntry[];

  /** Filters the sidebar's visible list; applies to whichever tab is active. */
  query: string;

  /**
   * Per-language analyzer readiness, or null until the first probe answers.
   * Null and "all ready" both render the normal module — the setup panel only
   * takes over once we know an analyzer is genuinely missing, so a slow probe
   * never flashes it at someone whose analyzer is fine.
   */
  analyzerStatuses: CodeGraphToolchainStatus[] | null;

  /** When the index was built, whether a run is going, why the last failed. */
  indexInfo: CodeGraphIndexInfo | null;
  /** Changed files of the project, for the Files tab's marks and filter. */
  gitStatus: Record<string, CodeGraphGitState>;
  /** Verb chips on in the Entry points tab; empty means all. */
  verbFilter: string[];
  /** Kind, service and owner groups folded shut, by key. */
  collapsedGroups: string[];
  /** Folders open in the Files tab. */
  fileExpanded: string[];
  /** The Files tab's toggles. */
  fileFilter: { codeOnly: boolean; changed: boolean; inFlow: boolean };

  set: (patch: Partial<CodeGraphState>) => void;
};

/**
 * CodeGraph's view state lives outside the components on purpose.
 *
 * Its documents are tabs (documents.ts), and a tab can be closed, replaced as
 * the preview, or unmounted when the module is left — so useState in a view
 * would come back empty although Go still has the project indexed and
 * watched. And the sidebar, the documents and the inspector all read the same
 * project, which only a store outside all three can hold.
 *
 * The Go service restores the last selected project on startup. The view
 * reads that selection on mount and waits for its index before loading data;
 * graph results themselves are never restored from frontend storage.
 */
export const useCodeGraphStore = create<CodeGraphState>((set) => ({
  projectPath: "",
  entryPoints: [],
  status: "",
  error: "",
  errorCode: "",
  busy: false,

  flows: {},
  buffers: {},

  inspector: null,
  snippet: null,
  snippetLoading: false,
  snippetError: "",

  activeTab: "entries",
  plan: null,
  planEdges: [],
  planBusy: false,
  selectedPlanNodeId: null,
  commentsPlanNodeId: null,
  commentCounts: {},
  planTasks: {},
  planError: "",
  files: [],
  query: "",
  analyzerStatuses: null,
  indexInfo: null,
  gitStatus: {},
  verbFilter: [],
  collapsedGroups: [],
  fileExpanded: [],
  fileFilter: { codeOnly: true, changed: false, inFlow: false },

  set: (patch) => set(patch),
}));

/**
 * One guard for every CodeGraph request, module-level like the store itself:
 * the actions outlive the view (see above), and so do their responses.
 * Keys are per action; a project switch invalidates all of them.
 */
export const codeGraphRequests = createRequestGuard();
