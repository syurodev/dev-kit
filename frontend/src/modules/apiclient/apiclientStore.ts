import { create } from "zustand";
import { persist } from "zustand/middleware";
import { uiStateStorage } from "@/bridge/uistate";
import { errorMessage } from "@/bridge/errors";
import { listEnvTags, type EnvTag } from "@/bridge/environments";
import {
  getGlobals,
  listCollections,
  listEnvironments,
  listRequests,
  type ApiCollection,
  type ApiEnvironment,
  type ApiRequest,
  type ApiResponse,
  type RequestInput,
  type VarSet,
} from "@/bridge/apiclient";

/**
 * One editor tab's working state. It lives here, not in the component, so a
 * tab that is switched away from keeps what was typed into it and the
 * response it got — the same promise every other tab in the app makes.
 */
export type EditorState = {
  request: RequestInput;
  /** What is saved, for "unsaved changes"; null for a request never saved. */
  baseline: RequestInput | null;
  response: ApiResponse | null;
  sending: boolean;
  /** The id this tab's send in flight was given, for Cancel. */
  sendId: string;
  error: string;
  /** Why a draft from Code Graph has no body — shown once, above the body. */
  note: string;
  /** Bumped after each send, so the history below reloads. */
  sends: number;
};

type ApiClientState = {
  requests: ApiRequest[] | null;
  collections: ApiCollection[] | null;
  environments: ApiEnvironment[] | null;
  globals: VarSet | null;
  /** The env tags, for which environments ask before every send. */
  envTags: EnvTag[];
  error: string;
  /**
   * The environment every send goes against, until somebody picks another.
   * One for the module, as in Postman: it is the answer to "where am I
   * pointed", and a different answer per tab is how a request goes to prod
   * from the one tab nobody looked at.
   */
  activeEnvironmentId: string;
  expanded: Record<string, boolean>;
  editors: Record<string, EditorState>;
  /** Contents for a new tab to open with: an imported curl, a duplicate. */
  pending: Record<string, RequestInput>;
  /** The import dialog, open on one of its tabs; null when closed. */
  importKind: "curl" | "collection" | "environment" | null;
  setImportKind: (kind: ApiClientState["importKind"]) => void;

  refresh: () => Promise<void>;
  setActiveEnvironment: (id: string) => void;
  toggleExpanded: (id: string) => void;
  expand: (id: string) => void;
  setEditor: (key: string, editor: EditorState) => void;
  patchEditor: (key: string, patch: Partial<EditorState>) => void;
  dropEditor: (key: string) => void;
  setPending: (key: string, request: RequestInput) => void;
  takePending: (key: string) => RequestInput | undefined;
};

export const useApiClientStore = create<ApiClientState>()(
  persist(
    (set, get) => ({
      requests: null,
      collections: null,
      environments: null,
      globals: null,
      envTags: [],
      error: "",
      activeEnvironmentId: "",
      expanded: {},
      editors: {},
      pending: {},
      importKind: null,
      setImportKind: (kind) => set({ importKind: kind }),

      refresh: async () => {
        try {
          const [requests, collections, environments, globals, envTags] = await Promise.all([
            listRequests(),
            listCollections(),
            listEnvironments(),
            getGlobals(),
            listEnvTags().catch(() => [] as EnvTag[]),
          ]);
          set((state) => ({
            requests,
            collections,
            environments,
            globals,
            envTags,
            error: "",
            // An environment deleted elsewhere is not one to keep sending to.
            activeEnvironmentId: environments.some((env) => env.id === state.activeEnvironmentId)
              ? state.activeEnvironmentId
              : (environments[0]?.id ?? ""),
          }));
        } catch (e) {
          set({ error: errorMessage(e) });
        }
      },

      setActiveEnvironment: (id) => set({ activeEnvironmentId: id }),
      toggleExpanded: (id) => set((state) => ({ expanded: { ...state.expanded, [id]: !state.expanded[id] } })),
      expand: (id) => set((state) => (state.expanded[id] ? state : { expanded: { ...state.expanded, [id]: true } })),

      setEditor: (key, editor) => set((state) => ({ editors: { ...state.editors, [key]: editor } })),
      patchEditor: (key, patch) =>
        set((state) => {
          const current = state.editors[key];
          if (!current) return state;
          return { editors: { ...state.editors, [key]: { ...current, ...patch } } };
        }),
      dropEditor: (key) =>
        set((state) => {
          if (!(key in state.editors)) return state;
          const editors = { ...state.editors };
          delete editors[key];
          return { editors };
        }),
      setPending: (key, request) => set((state) => ({ pending: { ...state.pending, [key]: request } })),
      takePending: (key) => {
        const request = get().pending[key];
        if (request) {
          set((state) => {
            const pending = { ...state.pending };
            delete pending[key];
            return { pending };
          });
        }
        return request;
      },
    }),
    {
      name: "apiclient",
      storage: uiStateStorage,
      // The environment picked and the folders open. Requests, collections and
      // variables come from the bridge every time — a cached copy would
      // outlive the vault item it describes.
      partialize: (state) => ({
        activeEnvironmentId: state.activeEnvironmentId,
        expanded: state.expanded,
      }),
    },
  ),
);

/** The environment sends go against, or null when there is none. */
export function activeEnvironment(state: Pick<ApiClientState, "environments" | "activeEnvironmentId">) {
  return state.environments?.find((env) => env.id === state.activeEnvironmentId) ?? null;
}

/** Whether sends under this tag ask first. An unknown tag is treated as one that does. */
export function tagAsks(envTags: readonly EnvTag[], tag: string): boolean {
  return envTags.find((t) => t.name === tag)?.requiresConfirmation ?? true;
}
