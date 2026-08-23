import { create } from "zustand";
import type {
  CodeGraphCodeSnippet,
  CodeGraphEndpoint,
  CodeGraphFileEntry,
  CodeGraphSubgraph,
} from "@/bridge/codegraph";

type CodeGraphState = {
  projectPath: string;
  endpoints: CodeGraphEndpoint[];
  subgraph: CodeGraphSubgraph | null;
  rootId: string | null;
  status: string;
  error: string;
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

  activeTab: "endpoints" | "files";
  files: CodeGraphFileEntry[];
  selectedFile: string | null;
  fileContent: string;
  fileDirty: boolean;
  fileError: string;

  /** Filters the sidebar's visible list; applies to whichever tab is active. */
  query: string;

  set: (patch: Partial<CodeGraphState>) => void;
};

/**
 * CodeGraph's view state lives outside the component on purpose.
 *
 * Visiting a module from the Dock opens it as the single shared *preview* tab,
 * and openTab replaces whatever preview tab is already there (see
 * stores/workbenchTabs.ts). So switching to another module drops CodeGraph's
 * tab entirely and unmounts its subtree — any useState in the view is gone,
 * and coming back showed an empty "Open Java project…" screen even though Go
 * still had the project indexed and watched.
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
  busy: false,
  loadingMore: false,

  inspectorNodeId: null,
  snippet: null,
  snippetLoading: false,
  snippetError: "",

  activeTab: "endpoints",
  files: [],
  selectedFile: null,
  fileContent: "",
  fileDirty: false,
  fileError: "",
  query: "",

  set: (patch) => set(patch),
}));
