import { create } from "zustand";
import { createRequestGuard } from "./requestGuard";
import type { CodeGraphPlanEdge, CodeGraphPlanProgress } from "@/bridge/codegraphPlan";
import type {
  CodeGraphCodeSnippet,
  CodeGraphFileEntry,
  CodeGraphSubgraph,
  CodeGraphToolchainStatus,
} from "@/bridge/codegraph";
import type { ServiceIndexStatus, Workspace, WorkspaceService } from "@/bridge/workspace";
import type { CodeGraphGitMark, FileOutline, FolderEntryPoints, FolderState } from "@/bridge/workspaceCode";
import type { FileBuffer } from "./workspace/bufferModel";
import { WHOLE_WORKSPACE, type Scope } from "./workspace/scope";
import { DEFAULT_SEARCH_OPTIONS, type SearchSession } from "./workspace/searchModel";

export type { FileBuffer };

/**
 * One flow document: the graph drawn from a root node of one folder, as a tab
 * of its own (documents.ts). Keyed by `flowKey(folderId, rootNodeId)` — two
 * folders can have a node with the same id, and each draws its own graph.
 */
export type FlowDoc = {
  /** The folder whose index this was drawn from. */
  folderId: string;
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

/** One task on a plan node, narrowed to what a chip draws. */
export type PlanTaskChip = { id: string; key: string; title: string; status: string };

/**
 * What the sidebar and documents know about one workspace folder. Everything a
 * folder owns is under its own entry: refreshing one folder replaces one entry
 * and leaves the others' arrays — and the components reading them — alone.
 */
export type FolderData = {
  /** What the machine says: reachable, branch, changed count. */
  state: FolderState | null;
  files: CodeGraphFileEntry[];
  filesTruncated: boolean;
  filesStatus: "idle" | "loading" | "ready" | "error";
  filesError: string;
  git: Record<string, CodeGraphGitMark>;
  entries: FolderEntryPoints | null;
  entriesStatus: "idle" | "loading" | "ready" | "error";
  entriesError: string;
  /** The queue's account of the folder (pending / indexing / failed…). */
  indexStatus: ServiceIndexStatus | null;
};

export const emptyFolderData = (): FolderData => ({
  state: null,
  files: [],
  filesTruncated: false,
  filesStatus: "idle",
  filesError: "",
  git: {},
  entries: null,
  entriesStatus: "idle",
  entriesError: "",
  indexStatus: null,
});

/** One folder's plan, with its comment counts and the tasks implementing it. */
export type PlanState = {
  plan: CodeGraphPlanProgress | null;
  edges: CodeGraphPlanEdge[];
  busy: boolean;
  error: string;
  commentCounts: Record<string, number>;
  tasks: Record<string, PlanTaskChip[]>;
};

export const emptyPlanState = (): PlanState => ({ plan: null, edges: [], busy: false, error: "", commentCounts: {}, tasks: {} });

/** An outline read for a file, valid for the index time it carries. */
export type OutlineEntry = FileOutline & { loading: boolean; error: string };

/** What the references / peek panel shows. */
export type NavigationPanelState = {
  mode: "peek" | "references";
  /** Where the lookup was made from. */
  source: { workspaceId: string; folderId: string; path: string; line: number };
  identifier: string;
  locations: PanelLocation[];
  selected: number;
};

export type PanelLocation = {
  folderId: string;
  path: string;
  line: number;
  label?: string;
  kind?: string;
  preview?: string;
};

export type SidebarTab = "entries" | "files" | "search" | "plan";

type CodeGraphState = {
  /** The workspace the discovery data belongs to; "" until the title-bar switcher picks one. */
  workspaceId: string;
  workspace: Workspace | null;
  /**
   * Bumped every time the workspace changes. Anything that finishes after the
   * bump belongs to the previous workspace and is dropped.
   */
  generation: number;
  /** Folders of the workspace in its own order (primary first). */
  folders: WorkspaceService[];
  /** Id of the primary folder. Derived from the workspace's root; never stored apart from it. */
  primaryId: string;
  foldersStatus: "idle" | "loading" | "ready" | "error";
  foldersError: string;
  /** What the sidebar's lists show. */
  scope: Scope;
  data: Record<string, FolderData>;

  /** Open flow documents, by flowKey. */
  flows: Record<string, FlowDoc>;
  /** Open (or edited) file documents, by docKey. Survives the tab closing. */
  buffers: Record<string, FileBuffer>;
  outlines: Record<string, OutlineEntry>;
  panel: NavigationPanelState | null;

  /** The node whose inspector is open, and the flow it was picked in. */
  inspector: { flow: string; nodeId: string } | null;
  snippet: CodeGraphCodeSnippet | null;
  snippetLoading: boolean;
  snippetError: string;

  activeTab: SidebarTab;
  /** Filters the sidebar's visible list; applies to whichever tab is active. */
  query: string;
  /** Verb chips on in the Entry points tab; empty means all. */
  verbFilter: string[];
  /** Kind, service and owner groups folded shut, by key. */
  collapsedGroups: string[];
  /** Directories open in the Files tab, by treeDirId. */
  fileExpanded: string[];
  /** Folder roots folded shut in the Files tab, by folderId. */
  collapsedRoots: string[];
  fileFilter: { codeOnly: boolean; changed: boolean; inFlow: boolean };

  search: SearchSession;

  /** The folder whose plan the sidebar's Plan tab shows. */
  planFolderId: string;
  plans: Record<string, PlanState>;
  selectedPlanNodeId: string | null;
  commentsPlanNodeId: string | null;

  /**
   * Per-language analyzer readiness, or null until the first probe answers.
   * Null and "all ready" both render the normal module — the setup panel only
   * takes over once we know an analyzer is genuinely missing.
   */
  analyzerStatuses: CodeGraphToolchainStatus[] | null;

  /** Documents the watcher could not watch: looked at again when the window regains focus. */
  unwatched: string[];

  set: (patch: Partial<CodeGraphState>) => void;
};

export const newSearchSession = (workspaceId: string, scope: Scope = WHOLE_WORKSPACE): SearchSession => ({
  id: `s${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`,
  workspaceId,
  scope,
  options: DEFAULT_SEARCH_OPTIONS,
  status: "idle",
  error: "",
  result: null,
  resultFor: "",
  collapsed: [],
});

/**
 * CodeGraph's view state lives outside the components on purpose.
 *
 * Its documents are tabs (documents.ts), and a tab can be closed, replaced as
 * the preview, or unmounted when the module is left — so useState in a view
 * would come back empty although Go still has the files and the indexes. And
 * the sidebar, the documents and the inspector all read the same workspace,
 * which only a store outside all of them can hold.
 *
 * What it holds is partitioned by owner (docs/design/codegraph-workspace.md
 * § 2): per-folder data under `data[folderId]`, buffers under their docKey,
 * flows under their flowKey. There is no "current project" in here. The
 * primary folder is read from the workspace, not copied, and the discovery
 * scope is a lens over the folders — changing either closes nothing.
 *
 * Graph results, file text and search results are never restored from frontend
 * storage; Go is the authority and the next mount asks again.
 */
export const useCodeGraphStore = create<CodeGraphState>((set) => ({
  workspaceId: "",
  workspace: null,
  generation: 0,
  folders: [],
  primaryId: "",
  foldersStatus: "idle",
  foldersError: "",
  scope: WHOLE_WORKSPACE,
  data: {},

  flows: {},
  buffers: {},
  outlines: {},
  panel: null,

  inspector: null,
  snippet: null,
  snippetLoading: false,
  snippetError: "",

  activeTab: "entries",
  query: "",
  verbFilter: [],
  collapsedGroups: [],
  fileExpanded: [],
  collapsedRoots: [],
  fileFilter: { codeOnly: true, changed: false, inFlow: false },

  search: newSearchSession(""),

  planFolderId: "",
  plans: {},
  selectedPlanNodeId: null,
  commentsPlanNodeId: null,

  analyzerStatuses: null,
  unwatched: [],

  set: (patch) => set(patch),
}));

/**
 * One guard for every CodeGraph request, module-level like the store itself:
 * the actions outlive the view (see above), and so do their responses.
 * Keys are per action and per owner; a workspace switch invalidates all of them.
 */
export const codeGraphRequests = createRequestGuard();
