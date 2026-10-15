import { create } from "zustand";
import { Bot } from "lucide-react";
import type { AgentSession } from "@/bridge/agenthub";
import { useAgentSessionsStore } from "@/stores/agentSessions";
import { forgetClosed, moduleTabs, retitleClosedTabs, tabKey, useWorkbenchTabsStore, type CloseTabOptions, type WorkbenchTab, type WorkbenchTabsState } from "@/stores/workbenchTabs";

/**
 * Agent Hub's documents: one workbench tab per session, keyed by
 * `params.sessionId` (docs/design/module-shell-integration.md § 4), plus the
 * module's home tab — no session id — which is where a new session is drafted.
 *
 * Every way of showing a session goes through here: the sidebar, the palette,
 * the title-bar peek, a notification, the Agent panel. The module keeps no list
 * of open sessions of its own; the workbench's tabs are that list, and the
 * Agent panel's tabs read the same one.
 *
 * `activeSessionId` in the agent store is not a second selection: it is which
 * session the Agent panel follows. The view on screen in Agent Hub writes it,
 * so leaving the module carries the session you were looking at into the panel.
 */

export const AGENTHUB_MODULE = "agenthub";
export const AGENTHUB_VIEW = "agenthub.sessions";
export const NEW_SESSION_TITLE = "New session";

export function sessionTitle(session: Pick<AgentSession, "id" | "title">): string {
  return session.title?.trim() || `Session ${session.id.slice(0, 8)}`;
}

/** The session a tab shows, or "" for the home (new session) tab. */
export function tabSessionId(tab: Pick<WorkbenchTab, "params"> | undefined): string {
  const id = tab?.params?.sessionId;
  return typeof id === "string" ? id : "";
}

/** Agent Hub's open session tabs, in strip order. The home tab is not one. */
export function agentSessionTabs(state: Pick<WorkbenchTabsState, "modules">): WorkbenchTab[] {
  return moduleTabs(state.modules, AGENTHUB_MODULE).openTabs.filter((tab) => tabSessionId(tab) !== "");
}

/** The tab Agent Hub's focused group shows — not only the partition's last active one. */
function shownAgentHubTab(state: WorkbenchTabsState): WorkbenchTab | undefined {
  const tabs = moduleTabs(state.modules, AGENTHUB_MODULE);
  const group =
    state.groups.find((g) => g.id === state.focusedGroupId && g.moduleId === AGENTHUB_MODULE) ??
    state.groups.find((g) => g.moduleId === AGENTHUB_MODULE);
  return tabs.openTabs.find((t) => t.id === (group?.activeTabId ?? tabs.activeTabId));
}

/** The session Agent Hub has on screen, or "" (a new-session draft, or nothing). */
export function activeAgentHubSessionId(state: WorkbenchTabsState): string {
  return tabSessionId(shownAgentHubTab(state));
}

/** Opens (or focuses) a session's tab, which brings Agent Hub on screen. */
export function openAgentSession(session: Pick<AgentSession, "id" | "title">, options: { beside?: boolean } = {}): string {
  return useWorkbenchTabsStore.getState().openTab({
    moduleId: AGENTHUB_MODULE,
    viewId: AGENTHUB_VIEW,
    title: sessionTitle(session),
    icon: Bot,
    params: { sessionId: session.id },
    preview: false,
    beside: options.beside,
  });
}

/**
 * Adds a session's tab without bringing Agent Hub on screen — for the Agent
 * panel, which shows the session itself and must not pull the main area away
 * from what the person is working on. A tab already open is left as it is.
 */
export function addAgentSessionTabInBackground(session: Pick<AgentSession, "id" | "title">): void {
  useWorkbenchTabsStore.setState((state) => {
    const tabs = moduleTabs(state.modules, AGENTHUB_MODULE);
    if (tabs.openTabs.some((tab) => tabSessionId(tab) === session.id)) return state;
    const params = { sessionId: session.id };
    const tab: WorkbenchTab = {
      id: tabKey(AGENTHUB_MODULE, AGENTHUB_VIEW, params),
      moduleId: AGENTHUB_MODULE,
      viewId: AGENTHUB_VIEW,
      title: sessionTitle(session),
      icon: Bot,
      params,
      preview: false,
    };
    return {
      modules: {
        ...state.modules,
        [AGENTHUB_MODULE]: { ...tabs, openTabs: [...tabs.openTabs, tab], activeTabId: tabs.activeTabId || tab.id },
      },
      // Open again without openTab, so it forgets its closed record itself.
      recentlyClosed: forgetClosed(state.recentlyClosed, tab.id),
    };
  });
}

/**
 * Closes every tab showing these sessions. Closing a tab never closes a session.
 * `remember: false` when the sessions are gone, so ⌘⇧T does not offer them back.
 */
export function closeAgentSessionTabs(sessionIds: Iterable<string>, options?: CloseTabOptions): void {
  const ids = new Set(sessionIds);
  const state = useWorkbenchTabsStore.getState();
  for (const tab of moduleTabs(state.modules, AGENTHUB_MODULE).openTabs) {
    if (ids.has(tabSessionId(tab))) state.closeTab(tab.id, options);
  }
}

/** The home tab, where a new session is drafted. Not remembered: the draft became the session. */
export function closeNewSessionTab(): void {
  const state = useWorkbenchTabsStore.getState();
  for (const tab of moduleTabs(state.modules, AGENTHUB_MODULE).openTabs) {
    if (tab.viewId === AGENTHUB_VIEW && tabSessionId(tab) === "") state.closeTab(tab.id, { remember: false });
  }
}

/**
 * Keeps tab titles in step with their sessions — a rename, or the title an
 * agent gives its session after the first turn. Id, group and pin stay.
 */
export function syncAgentSessionTabTitles(sessionsById: Record<string, AgentSession>): void {
  useWorkbenchTabsStore.setState((state) => {
    const tabs = moduleTabs(state.modules, AGENTHUB_MODULE);
    let changed = false;
    const openTabs = tabs.openTabs.map((tab) => {
      const session = sessionsById[tabSessionId(tab)];
      if (!session || tab.title === sessionTitle(session)) return tab;
      changed = true;
      return { ...tab, title: sessionTitle(session) };
    });
    const recentlyClosed = retitleClosedTabs(state.recentlyClosed, (entry) => {
      if (entry.moduleId !== AGENTHUB_MODULE) return undefined;
      const session = sessionsById[tabSessionId(entry)];
      return session ? sessionTitle(session) : undefined;
    });
    if (!changed && recentlyClosed === state.recentlyClosed) return state;
    return {
      modules: changed ? { ...state.modules, [AGENTHUB_MODULE]: { ...tabs, openTabs } } : state.modules,
      recentlyClosed,
    };
  });
}

/** What a new-session draft starts with when something other than "+ New" asked for it. */
export type AgentDraftRequest = {
  /** "" for the default folder (the one the app is working in, else the last used). */
  cwd: string;
  /** False: start in `cwd` exactly, even when it is "" (a scratch session). */
  defaultFolder: boolean;
  workspaceId?: string;
  /** Every service of a workspace, when the whole workspace was asked for. */
  workspacePaths?: string[];
  parent?: AgentSession | null;
};

/**
 * A draft asked for from outside the new-session tab (the sidebar's project
 * and child "+"). The tab picks it up when it is on screen, so the request
 * survives the tab being opened, mounted and loaded first.
 */
export const useAgentDraftRequests = create<{
  request: AgentDraftRequest | null;
  setRequest: (request: AgentDraftRequest | null) => void;
}>((set) => ({
  request: null,
  setRequest: (request) => set({ request }),
}));

/** Opens the new-session tab with a fresh draft: the strip's +, ⌘N, "New session". */
export function openNewAgentSession(request: Partial<AgentDraftRequest> = {}): void {
  useAgentDraftRequests.getState().setRequest({ cwd: "", defaultFolder: true, ...request });
  const state = useWorkbenchTabsStore.getState();
  state.openTab({ moduleId: AGENTHUB_MODULE, viewId: AGENTHUB_VIEW, title: NEW_SESSION_TITLE, icon: Bot });
}

/**
 * Shows a session wherever it can be seen: its tab while Agent Hub is the
 * module on screen (the Agent panel is not shown there), else the Agent panel
 * — the session is added to the tabs so the panel can show it as one.
 */
export function showAgentSession(sessionId: string): void {
  const agents = useAgentSessionsStore.getState();
  const session = agents.sessionsById[sessionId] ?? { id: sessionId, title: null };
  agents.setActiveSessionId(sessionId);
  if (useWorkbenchTabsStore.getState().activeModuleId === AGENTHUB_MODULE) {
    openAgentSession(session);
    return;
  }
  addAgentSessionTabInBackground(session);
  agents.setChatOpen(true);
}

/**
 * The session the Agent panel shows: the one it follows while that one has a
 * tab, else the first tab. A session whose tab was closed is not shown behind
 * a strip that no longer lists it.
 */
export function agentPanelSessionId(tabSessionIds: readonly string[], activeSessionId: string): string {
  return tabSessionIds.includes(activeSessionId) ? activeSessionId : (tabSessionIds[0] ?? "");
}

/**
 * Whether a session is in front of the person right now: its tab on screen in
 * Agent Hub, or — anywhere else — the Agent panel open on it.
 */
export function agentSessionOnScreen(
  sessionId: string,
  workbench: WorkbenchTabsState = useWorkbenchTabsStore.getState(),
  agents: { activeSessionId: string; chatOpen: boolean } = useAgentSessionsStore.getState(),
): boolean {
  if (!sessionId) return false;
  if (workbench.activeModuleId === AGENTHUB_MODULE) return activeAgentHubSessionId(workbench) === sessionId;
  const panelSession = agentPanelSessionId(agentSessionTabs(workbench).map(tabSessionId), agents.activeSessionId);
  return agents.chatOpen && panelSession === sessionId;
}
