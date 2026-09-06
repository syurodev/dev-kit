import { useEffect, useMemo, useState } from "react";
import {
  agentHubCancel,
  agentHubChooseDirectory,
  agentHubCreateSession,
  agentHubGetStatus,
  agentHubInvoke,
  agentHubListVendorTypes,
  isSandboxUnavailableError,
  sandboxUnavailableMessage,
  type AgentVendor,
} from "../../bridge/agenthub";
import { codeGraphProjectRoot } from "../../bridge/codegraph";
import { errorMessage } from "../../bridge/errors";
import { useAgentHubSidebar } from "./useAgentHubSidebar";
import { useAgentTranscript } from "./transcript";
import {
  AgentHubContent,
  AgentNewSession,
} from "./surfaces";
import { TranscriptGallery } from "./transcript/TranscriptGallery";
import { useAgentSessionsStore } from "../../stores/agentSessions";

const DEFAULT_VENDORS: AgentVendor[] = ["mock", "claude"];

export function AgentHubView() {
  const [showGallery, setShowGallery] = useState(() => {
    if (typeof window !== "undefined") {
      return new URLSearchParams(window.location.search).get("gallery") === "1";
    }
    return false;
  });

  const sessions = useAgentSessionsStore((state) => state.sessions);
  const activeSessionId = useAgentSessionsStore((state) => state.activeSessionId);
  const setActiveSessionId = useAgentSessionsStore((state) => state.setActiveSessionId);
  const upsertSession = useAgentSessionsStore((state) => state.upsertSession);
  const transcripts = useAgentSessionsStore((state) => state.transcripts);
  const setPrompt = useAgentSessionsStore((state) => state.setPrompt);
  const pendingQueue = useAgentSessionsStore((state) => state.pendingQueue);

  const [vendors, setVendors] = useState<string[]>(DEFAULT_VENDORS);
  const [newSessionOpen, setNewSessionOpen] = useState(false);
  // The folder Code Graph has open, offered as one click in the new-session
  // dialog. Read when the dialog opens rather than on mount: Code Graph may
  // have opened a project since, and a stale path is worse than no suggestion.
  const [codeGraphCwd, setCodeGraphCwd] = useState("");

  useEffect(() => {
    if (!newSessionOpen) return;
    let cancelled = false;
    // A failure here means no suggestion, not a broken dialog — Code Graph
    // simply has nothing open, which is the ordinary case.
    codeGraphProjectRoot()
      .then((root) => {
        if (!cancelled) setCodeGraphCwd(root);
      })
      .catch(() => {
        if (!cancelled) setCodeGraphCwd("");
      });
    return () => {
      cancelled = true;
    };
  }, [newSessionOpen]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [sandboxHint, setSandboxHint] = useState("");

  const activeSession = useMemo(
    () => sessions.find((s) => s.id === activeSessionId) ?? sessions[0] ?? null,
    [sessions, activeSessionId],
  );

  const parentSession = useMemo(() => {
    if (!activeSession?.parentSessionId) return null;
    return sessions.find((s) => s.id === activeSession.parentSessionId) ?? null;
  }, [sessions, activeSession]);

  const activePrompt = activeSession
    ? transcripts[activeSession.id]?.prompt ?? ""
    : "";

  // Live typed transcript model powered by useAgentTranscript
  const transcriptModel = useAgentTranscript(activeSession?.id ?? "");

  // Auto-select first session if none active
  useEffect(() => {
    if (!activeSessionId && sessions.length > 0) {
      setActiveSessionId(sessions[0].id);
    }
  }, [sessions, activeSessionId, setActiveSessionId]);

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

  async function handleCreateSession(params: {
    vendor: string;
    cwd: string;
    title?: string;
  }) {
    setBusy(true);
    setError("");
    setSandboxHint("");
    try {
      const session = await agentHubCreateSession({
        agentType: params.vendor,
        cwd: params.cwd,
        title: params.title,
      });
      upsertSession(session);
      setActiveSessionId(session.id);
      setNewSessionOpen(false);
    } catch (e) {
      setError(errorMessage(e));
      if (params.vendor === "claude" && isSandboxUnavailableError(e)) {
        setSandboxHint(sandboxUnavailableMessage());
      }
    } finally {
      setBusy(false);
    }
  }

  async function handleSend() {
    if (!activeSession || !activePrompt.trim() || busy) return;
    const text = activePrompt.trim();
    setPrompt(activeSession.id, "");
    setBusy(true);
    setError("");
    try {
      upsertSession({ ...activeSession, status: "running" });
      await agentHubInvoke({
        sessionId: activeSession.id,
        prompt: text,
      });
      upsertSession(await agentHubGetStatus(activeSession.id));
    } catch (e) {
      setError(errorMessage(e));
      try {
        upsertSession(await agentHubGetStatus(activeSession.id));
      } catch {}
    } finally {
      setBusy(false);
    }
  }

  async function handleCancel() {
    if (!activeSession) return;
    try {
      await agentHubCancel(activeSession.id);
      upsertSession(await agentHubGetStatus(activeSession.id));
    } catch (e) {
      setError(errorMessage(e));
    }
  }



  // Group pending permission counts per session
  const pendingCounts = useMemo(() => {
    const map: Record<string, number> = {};
    for (const p of pendingQueue) {
      map[p.sessionId] = (map[p.sessionId] ?? 0) + 1;
    }
    return map;
  }, [pendingQueue]);

  useAgentHubSidebar({
    sessions,
    activeSessionId: activeSession?.id,
    pendingCounts,
    onSelectSession: setActiveSessionId,
    onNewSession: () => setNewSessionOpen(true),
  });

  return (
    <div
      data-testid="agenthub-view"
      className="relative flex size-full min-h-0 flex-1 overflow-hidden bg-surface-base"
    >
      {/* Dev toggle button in top right for leaf showcase gallery */}
      <button
        type="button"
        onClick={() => setShowGallery((prev) => !prev)}
        data-testid="toggle-transcript-gallery"
        className="absolute top-2 right-4 z-40 rounded-sm border border-border-subtle bg-surface-base/90 px-2 py-0.5 text-caption-11 text-text-tertiary shadow-sm hover:text-text-primary hover:bg-surface-tinted cursor-pointer"
      >
        {showGallery ? "Show Swarm" : "Leaf Gallery"}
      </button>

      {showGallery ? (
        <div className="size-full overflow-y-auto pt-8">
          <TranscriptGallery />
        </div>
      ) : (
        <div className="flex size-full min-h-0 flex-1 overflow-hidden">
          {/* The swarm tree is NOT drawn here — useAgentHubSidebar publishes it
              into the shell rail, which is where Figma's _Slot / Sidebar Content
              puts a module's rail. Drawing it here as well gave two vertical
              rails side by side whenever the shell sidebar was open. */}

          {/* Surface: Agent Hub / Content (1136×804) */}
          <AgentHubContent
            session={activeSession}
            parentSession={parentSession}
            items={transcriptModel.items}
            usage={transcriptModel.usage}
            commands={transcriptModel.commands}
            promptValue={activePrompt}
            onPromptChange={(val) => activeSession && setPrompt(activeSession.id, val)}
            onSend={() => void handleSend()}
            onCancel={() => void handleCancel()}
            streaming={busy || activeSession?.status === "running"}
            errorMessage={error}
            onRetrySession={() => void handleSend()}
            onNewSession={() => setNewSessionOpen(true)}
          />
        </div>
      )}

      {/* Surface: Agent / New Session. The hand-rolled backdrop this replaced
          had role="dialog" and aria-modal but none of what those promise — no
          focus trap, no escape key, no scroll lock. Dialog brings all three. */}
      <AgentNewSession
        open={newSessionOpen}
        onOpenChange={(next) => {
          setNewSessionOpen(next);
          if (!next) {
            setError("");
            setSandboxHint("");
          }
        }}
        vendors={vendors}
        busy={busy}
        error={error || sandboxHint}
        suggestedCwd={codeGraphCwd}
        onBrowse={() => agentHubChooseDirectory(codeGraphCwd)}
        onCreate={handleCreateSession}
      />
    </div>
  );
}
