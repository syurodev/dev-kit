import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  agentHubListContextFiles,
  agentHubListVendorTypes,
  agentHubPrepareSession,
  agentHubScratchRoot,
  type AgentContextFile,
} from "../../../bridge/agenthub";
import { useAgentSessionList, useAgentSessionsStore } from "../../../stores/agentSessions";
import { useAgentPinsStore } from "../../../stores/agentPins";
import { useCurrentWorkspaceServices } from "@/stores/workspaceSwitcher";
import { useModuleVisibility } from "@/components/ui/module-visibility";
import { EMPTY_ARRAY } from "@/lib/stableEmpty";
import { useAgentTranscript } from "../transcript";
import { pendingForSession, useResolvePermission } from "../permissions";
import { elicitationForSession, useRespondElicitation } from "../elicitations";
import { attentionBySession, failureDismissable } from "../sidebar/model";
import { useSidebarWorkspaces } from "../sidebarWorkspaces";
import { providerRetryFailure } from "../providerRetry";
import { rateLimitFailure } from "../surfaces/AgentContextMeter";
import { SHIPPED_VENDORS } from "../vendors";

// Until the backend answers: the agents DevKit ships. The mock adapter is a
// test double and is not offered.
const DEFAULT_VENDORS: string[] = [...SHIPPED_VENDORS];

/**
 * Everything a tab reads: the session list and store selectors, the live
 * transcript, the vendor list, the draft's project choices, the session's
 * context files, and the pending permission / question it is waiting on.
 *
 * Reads only — nothing here changes a session or starts a turn (that is
 * useAgentSessionActions), and nothing here opens a pane or binds a key (that
 * is useAgentHubEffects). The session on screen is the caller's: a tab knows
 * which id it was opened for, and reads never elect one themselves.
 */
export function useAgentSessionReads(tabSessionId: string, draftOpen: boolean) {
  const visible = useModuleVisibility();
  const sessions = useAgentSessionList();
  const sessionsLoaded = useAgentSessionsStore((state) => state.sessionsLoaded);
  const pendingQueue = useAgentSessionsStore((state) => state.pendingQueue);
  const elicitationQueue = useAgentSessionsStore((state) => state.elicitationQueue);

  const [vendors, setVendors] = useState<string[]>(DEFAULT_VENDORS);
  const recentProjects = useAgentPinsStore((state) => state.recentProjects);
  const dismissedFailures = useAgentPinsStore((state) => state.dismissedFailures);
  const { workspace: currentWorkspace, services: workspaceServices } = useCurrentWorkspaceServices();
  const sidebarWorkspaces = useSidebarWorkspaces();
  // Where "no project" sessions get their folders. Read once: it is a fixed
  // place under the config dir, and the sidebar groups everything under it.
  const scratchRoot = useQuery({
    queryKey: ["agenthub", "scratchRoot"],
    queryFn: agentHubScratchRoot,
    staleTime: Infinity,
  }).data ?? "";
  const [contextFiles, setContextFiles] = useState<Record<string, AgentContextFile[]>>({});

  // This tab's session, and only it: a tab whose session is gone says so
  // rather than quietly showing another one.
  const activeSession = useMemo(
    () => (tabSessionId ? sessions.find((s) => s.id === tabSessionId) ?? null : null),
    [sessions, tabSessionId],
  );

  const activePrompt = useAgentSessionsStore(
    (state) => state.promptBySession[activeSession?.id ?? ""] ?? "",
  );
  const queuedPrompts = useAgentSessionsStore(
    // EMPTY_ARRAY, not []: a fresh [] per read is a new snapshot every time,
    // and React renders forever for any session with nothing queued.
    (state) => state.queuedPromptsBySession[activeSession?.id ?? ""] ?? EMPTY_ARRAY,
  );
  const queuePaused = useAgentSessionsStore((state) =>
    Boolean(state.queuePausedBySession[activeSession?.id ?? ""]),
  );
  const queuePausedReason = useAgentSessionsStore(
    (state) => state.queuePauseReasonBySession[activeSession?.id ?? ""] ?? "",
  );

  // Live typed transcript model powered by useAgentTranscript
  const transcriptModel = useAgentTranscript(activeSession?.id ?? "", { enabled: visible });

  const sessionConfig = useQuery({
    queryKey: ["agenthub", "sessionConfig", activeSession?.id],
    queryFn: async () => { await agentHubPrepareSession(activeSession!.id); return true; },
    enabled: visible && !draftOpen && Boolean(activeSession && activeSession.status !== "closed" && activeSession.status !== "running" && activeSession.status !== "awaiting_permission"),
    staleTime: 60_000,
    retry: false,
  });

  useEffect(() => {
    const sessionId = activeSession?.id;
    if (!visible || !sessionId) return;
    let mounted = true;
    agentHubListContextFiles(sessionId)
      .then((files) => {
        if (mounted) setContextFiles((current) => ({ ...current, [sessionId]: files }));
      })
      .catch(() => {
        // The native picker remains available when a project cannot be walked.
      });
    return () => {
      mounted = false;
    };
  }, [activeSession?.id, visible]);

  // Load available vendor types
  useEffect(() => {
    let mounted = true;
    agentHubListVendorTypes()
      .then((list) => {
        if (!mounted || list.length === 0) return;
        setVendors(list);
      })
      .catch(() => {
        // Keep fallback
      });
    return () => {
      mounted = false;
    };
  }, []);

  // What the draft's picker offers: the current workspace's services first
  // (docs/dev-kit/workspace.mdx — registered on purpose, so they should sort
  // ahead of the guesses below), then folders used before, most recent first,
  // then any other project a session is open in. Never a scratch folder.
  const draftProjects = useMemo(() => {
    const isScratch = (path: string) => Boolean(scratchRoot) && path.startsWith(`${scratchRoot}/`);
    const seen = new Set<string>();
    const out: string[] = [];
    for (const path of [
      ...workspaceServices.map((s) => s.path),
      ...recentProjects,
      ...sessions.map((s) => s.projectPath),
    ]) {
      if (!path || isScratch(path) || seen.has(path)) continue;
      seen.add(path);
      out.push(path);
    }
    return out;
  }, [workspaceServices, recentProjects, sessions, scratchRoot]);

  // The vendor's own failure wins when there is one; Claude never reports a
  // rate-limit rejection through that channel (only through usage_update's
  // rateLimit), so without this a rejected turn had no banner at all — just
  // the composer's context-meter popover, which nobody opens unprompted.
  const activeFailure =
    transcriptModel.failure ?? providerRetryFailure(transcriptModel.retry) ?? rateLimitFailure(transcriptModel.usage);

  // The request this session is waiting on is asked inside the conversation,
  // not in the corner: see AgentHubContent's pendingPermission.
  const activePending = useMemo(
    () => pendingForSession(pendingQueue, activeSession?.id),
    [pendingQueue, activeSession?.id],
  );
  const { busy: permissionBusy, error: permissionError, resolve: resolvePermission, approveScoped: approveScopedPermission } = useResolvePermission(activePending);
  const activeQuestion = useMemo(
    () => elicitationForSession(elicitationQueue, activeSession?.id),
    [elicitationQueue, activeSession?.id],
  );
  const {
    busy: elicitationBusy,
    error: elicitationError,
    respond: respondElicitation,
  } = useRespondElicitation(activeQuestion);

  return {
    visible,
    sessions,
    sessionsLoaded,
    pendingQueue,
    elicitationQueue,
    vendors,
    recentProjects,
    dismissedFailures,
    currentWorkspace,
    workspaceServices,
    sidebarWorkspaces,
    scratchRoot,
    contextFiles,
    activeSession,
    activePrompt,
    queuedPrompts,
    queuePaused,
    queuePausedReason,
    transcriptModel,
    sessionConfig,
    draftProjects,
    activeFailure,
    activePending,
    permissionBusy,
    permissionError,
    resolvePermission,
    approveScopedPermission,
    activeQuestion,
    elicitationBusy,
    elicitationError,
    respondElicitation,
    canDismissError: Boolean(
      activeSession &&
        failureDismissable(
          activeSession,
          attentionBySession(pendingQueue, elicitationQueue)[activeSession.id],
          dismissedFailures,
        ),
    ),
  };
}

export type AgentSessionReads = ReturnType<typeof useAgentSessionReads>;
