import { useEffect, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { onAgentSignInCompleted } from "@/bridge/agentSignIn";
import { useAgentSessionsStore } from "../../../stores/agentSessions";
import { useSplitPanelActions } from "@/components/ui/split-panel";
import { detectPlatform } from "@/lib/platform";
import { appendChips, refChip } from "../composerDraft";
import { sessionTitle, useAgentDraftRequests } from "../documents";
import { AgentActivityPanel, useFollowing } from "../follow";
import { SubagentPanel, useSubagentPanel } from "../subagents";
import { isNewSessionShortcut } from "./sessionModel";
import type { AgentDraft } from "./useAgentDraft";
import type { AgentSessionActions } from "./useAgentSessionActions";
import type { AgentSessionReads } from "./useAgentSessionReads";

/**
 * What a tab does for the whole module while it is the one on screen: answer a
 * cross-module command or attach, remember the session on screen as the active
 * one and as read, show the followed session (or a subagent) in the split pane,
 * land a requested draft, bind ⌘N, and keep the catalog fresh after a sign-in.
 *
 * Mounted once per tab, and every effect stands aside unless `visible` — a tab
 * restored in the background must not move the app's active session.
 */
export function useAgentHubEffects({
  tabSessionId,
  reads,
  draft,
  actions,
}: {
  tabSessionId: string;
  reads: AgentSessionReads;
  draft: AgentDraft;
  actions: AgentSessionActions;
}) {
  const { visible, activeSession, pendingAgentAttach, pendingAgentHubCommand } = {
    visible: reads.visible,
    activeSession: reads.activeSession,
    pendingAgentAttach: useAgentSessionsStore((state) => state.pendingAgentAttach),
    pendingAgentHubCommand: useAgentSessionsStore((state) => state.pendingAgentHubCommand),
  };
  const queryClient = useQueryClient();
  const [focusComposerRequest, setFocusComposerRequest] = useState(0);

  useEffect(() => onAgentSignInCompleted((result) => {
    // An added account's sign-in says nothing about the login a draft is about to use.
    if (result.accountId) return;
    void queryClient.invalidateQueries({ queryKey: ["agenthub", "configCatalog"] });
    void queryClient.invalidateQueries({ queryKey: ["agenthub", "sessionConfig"] });
  }), [queryClient]);

  // A cross-module `agent.attach` (docs/dev-kit/cross-module.mdx § Primitive
  // 2) leaves its refs here rather than reaching into this component
  // directly, since the caller — a manifest handler — has no reference to
  // whichever composer is actually mounted (this view's, or the companion's;
  // AgentCompanion.tsx has the matching effect). Applied to whichever session
  // is active when it lands, same as askAgent does for plain text.
  useEffect(() => {
    if (!visible || !pendingAgentAttach || !activeSession) return;
    const { refs: attachedRefs, prompt } = pendingAgentAttach;
    const store = useAgentSessionsStore.getState();
    // The refs become chips at the end of the draft, then the prompt it came with.
    let next = appendChips(store.promptBySession[activeSession.id] ?? "", attachedRefs.map(refChip));
    if (prompt) next = next ? `${next}\n\n${prompt}` : prompt;
    if (next) store.setPrompt(activeSession.id, next);
    store.consumeAgentAttach();
  }, [visible, pendingAgentAttach, activeSession]);

  useEffect(() => {
    if (!visible || !pendingAgentHubCommand) return;
    useAgentSessionsStore.getState().consumeAgentHubCommand(pendingAgentHubCommand);
    if (pendingAgentHubCommand === "create") {
      draft.newSession();
    } else if (pendingAgentHubCommand === "cancel") {
      void actions.handleCancel();
    } else {
      setFocusComposerRequest((request) => request + 1);
    }
  }, [pendingAgentHubCommand, visible]);

  useEffect(() => {
    if (visible && activeSession?.id) useAgentSessionsStore.getState().markCompletedRead(activeSession.id);
  }, [activeSession?.id, visible]);

  // The tab on screen is the session the Agent panel follows once you leave
  // Agent Hub, and the one notifications and the permission toast treat as seen.
  useEffect(() => {
    if (visible && tabSessionId) useAgentSessionsStore.getState().setActiveSessionId(tabSessionId);
  }, [visible, tabSessionId]);

  // Following this session: the module's right-hand pane shows what the agent
  // changes in DevKit as it happens (follow.tsx). Only the tab on screen
  // drives it, so switching tabs moves the pane to that session — or closes
  // it, if that one is not followed. The panel reads the transcript itself,
  // so it is handed one element per session, not one per streamed token.
  const { openPanel, closePanel } = useSplitPanelActions();
  const [following, toggleFollowing] = useFollowing(activeSession?.id);
  const followedId = following ? activeSession?.id ?? "" : "";
  const followedTitle = activeSession ? sessionTitle(activeSession) : "";
  // A subagent opened from the sidebar goes in the same pane, ahead of the
  // follow feed while it is open; closing it gives the pane back.
  const openSubagent = useSubagentPanel((s) => s.open);
  const closeSubagent = useSubagentPanel((s) => s.close);
  const activeId = activeSession?.id ?? "";
  const subagentId = openSubagent && openSubagent.sessionId === activeId ? openSubagent.subagentId : "";
  useEffect(() => {
    if (!visible) return;
    if (subagentId && activeId) {
      openPanel(<SubagentPanel sessionId={activeId} subagentId={subagentId} onClose={closeSubagent} />, {
        width: 420,
        onClose: closeSubagent,
        key: `subagent:${activeId}:${subagentId}`,
      });
      return;
    }
    if (!followedId) {
      // Discarded, not kept hidden: both panels read the session's transcript
      // from the store and would re-render on every streamed token while hidden.
      closePanel({ discard: true });
      return;
    }
    openPanel(<AgentActivityPanel sessionId={followedId} title={followedTitle} onStop={toggleFollowing} />, {
      width: 400,
      onClose: toggleFollowing,
      key: `activity:${followedId}`,
    });
  }, [visible, subagentId, activeId, followedId, followedTitle, openPanel, closePanel, toggleFollowing, closeSubagent]);

  // A draft asked for from elsewhere — the sidebar's project or child "+" —
  // lands in the new-session tab, once it is on screen.
  const draftRequest = useAgentDraftRequests((state) => state.request);
  useEffect(() => {
    if (!visible || tabSessionId || !draftRequest) return;
    useAgentDraftRequests.getState().setRequest(null);
    if (draftRequest.defaultFolder) {
      void draft.startNewDraft();
      return;
    }
    draft.startDraft(draftRequest.cwd, draftRequest.parent ?? null, draftRequest.workspaceId);
    const paths = draftRequest.workspacePaths ?? [];
    if (draftRequest.workspaceId && paths.length > 1) draft.chooseWorkspaceForDraft(draftRequest.workspaceId, paths);
  }, [visible, tabSessionId, draftRequest]);

  useEffect(() => {
    if (!visible) return;
    function handleKeyDown(event: KeyboardEvent) {
      const overlayOpen = Boolean(document.querySelector('[role="dialog"], [role="alertdialog"], [role="menu"]'));
      if (!isNewSessionShortcut(event, detectPlatform() === "mac", overlayOpen)) return;
      event.preventDefault();
      draft.newSession();
    }
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [visible]);

  return { focusComposerRequest };
}

export type AgentHubEffects = ReturnType<typeof useAgentHubEffects>;
