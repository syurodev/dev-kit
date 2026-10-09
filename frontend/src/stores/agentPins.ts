import { create } from "zustand";
import { persist } from "zustand/middleware";

import { uiStateStorage } from "@/bridge/uistate";

type AgentPinsState = {
  /** Session ids the user pinned, in the order they were pinned. */
  pinned: string[];
  isPinned: (sessionId: string) => boolean;
  togglePin: (sessionId: string) => void;
  unpin: (sessionId: string) => void;
  /**
   * Tree nodes the user folded shut, by the rail's own keys ("project:<path>",
   * "session:<id>").
   *
   * Closed rather than open, so the default is an open tree: a project opened
   * for the first time, or a session that just spawned its first subagent, has
   * no entry here and shows. Storing the open ones would hide everything new.
   */
  collapsed: string[];
  toggleCollapsed: (key: string) => void;
  /**
   * Groups whose stack of older sessions is open, by the same keys as
   * `collapsed`. Open rather than closed, the opposite of `collapsed`: a stack
   * starts folded, and a group nobody has touched must not unfold everything.
   */
  olderOpen: string[];
  toggleOlderOpen: (key: string) => void;
  /**
   * Closed sessions left out of the tree. Off by default: a session you just
   * closed vanishing from under the pointer is a worse surprise than a long
   * list, and the list is what the toggle is for.
   */
  hideClosed: boolean;
  toggleHideClosed: () => void;
  /**
   * Folders sessions were started in, most recent first. A new draft opens in
   * the first one, and the draft's project picker lists them — so a folder
   * picked once is one click away even after its sessions are deleted.
   */
  recentProjects: string[];
  rememberProject: (path: string) => void;
  /**
   * Sessions the person follows: what the agent changes in DevKit shows up as
   * it happens — Agent Hub's activity panel, a toast anywhere else. Per
   * session and opt-in, remembered: following every session would put a
   * stream of other people's work in front of whatever is on screen.
   */
  following: string[];
  toggleFollowing: (sessionId: string) => void;
};

/** How many folders the project picker remembers. */
const RECENT_PROJECTS = 12;

/**
 * Which agents the user pinned to the top of the swarm tree.
 *
 * Not a column on agent_sessions: a pin is a per-person view preference, not a
 * fact about the agent, and in the session row it becomes something an agent
 * could read about itself and the bridge would have to authorize. ui_state is
 * where this app already keeps such state.
 *
 * Ids of deleted sessions are dropped as they are noticed rather than swept:
 * an id in this list that matches nothing simply never sorts anything.
 */
export const useAgentPinsStore = create<AgentPinsState>()(
  persist(
    (set, get) => ({
      pinned: [],
      isPinned: (sessionId) => get().pinned.includes(sessionId),
      togglePin: (sessionId) =>
        set((state) => ({
          pinned: state.pinned.includes(sessionId)
            ? state.pinned.filter((id) => id !== sessionId)
            : [...state.pinned, sessionId],
        })),
      unpin: (sessionId) =>
        set((state) => ({ pinned: state.pinned.filter((id) => id !== sessionId) })),
      collapsed: [],
      toggleCollapsed: (key) =>
        set((state) => ({
          collapsed: state.collapsed.includes(key)
            ? state.collapsed.filter((k) => k !== key)
            : [...state.collapsed, key],
        })),
      olderOpen: [],
      toggleOlderOpen: (key) =>
        set((state) => ({
          olderOpen: state.olderOpen.includes(key)
            ? state.olderOpen.filter((k) => k !== key)
            : [...state.olderOpen, key],
        })),
      hideClosed: false,
      toggleHideClosed: () => set((state) => ({ hideClosed: !state.hideClosed })),
      following: [],
      toggleFollowing: (sessionId) =>
        set((state) => ({
          following: state.following.includes(sessionId)
            ? state.following.filter((id) => id !== sessionId)
            : [...state.following, sessionId],
        })),
      recentProjects: [],
      rememberProject: (path) =>
        set((state) => ({
          recentProjects: [path, ...state.recentProjects.filter((p) => p !== path)].slice(0, RECENT_PROJECTS),
        })),
    }),
    {
      name: "agentPins",
      storage: uiStateStorage,
      partialize: (state) => ({
        pinned: state.pinned,
        collapsed: state.collapsed,
        olderOpen: state.olderOpen,
        hideClosed: state.hideClosed,
        recentProjects: state.recentProjects,
        following: state.following,
      }),
    },
  ),
);
