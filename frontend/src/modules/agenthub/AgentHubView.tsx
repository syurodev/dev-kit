import { useEffect, useMemo, useState } from "react";
import {
  agentHubCancel,
  agentHubChooseDirectory,
  agentHubCloseSession,
  agentHubCreateSession,
  agentHubDeleteSession,
  agentHubGetStatus,
  agentHubInvoke,
  agentHubListVendorTypes,
  isSandboxUnavailableError,
  sandboxUnavailableMessage,
  type AgentSession,
  type AgentVendor,
} from "../../bridge/agenthub";
import { codeGraphProjectRoot } from "../../bridge/codegraph";
import { copyText } from "../../bridge/clipboard";
import { toAppError } from "../../bridge/errors";
import { useAgentHubSidebar } from "./useAgentHubSidebar";
import { useAgentTranscript } from "./transcript";
import {
  AgentHubContent,
  AgentNewSession,
} from "./surfaces";
import { TranscriptGallery } from "./transcript/TranscriptGallery";
import { useAgentSessionsStore } from "../../stores/agentSessions";
import { useAgentPinsStore } from "../../stores/agentPins";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";

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
  const removeSession = useAgentSessionsStore((state) => state.removeSession);
  const transcripts = useAgentSessionsStore((state) => state.transcripts);
  const setPrompt = useAgentSessionsStore((state) => state.setPrompt);
  const pendingQueue = useAgentSessionsStore((state) => state.pendingQueue);

  const [vendors, setVendors] = useState<string[]>(DEFAULT_VENDORS);
  const [newSessionOpen, setNewSessionOpen] = useState(false);
  // Figma 948:2204 writes the reason as "agenthub.processFailed — the agent
  // exited after …": the DevKit code AND what happened. errorMessage() gives
  // only the humanised half, so the card was saying "Agent process failed."
  // — true, and no more actionable than the red dot the note complains about.
  const [errorReason, setErrorReason] = useState("");
  // The folder Code Graph has open, offered as one click in the new-session
  // dialog. Read when the dialog opens rather than on mount: Code Graph may
  // have opened a project since, and a stale path is worse than no suggestion.
  const [codeGraphCwd, setCodeGraphCwd] = useState("");
  // The parent a new session will inherit from, set by the tree's "New child
  // session". Null means the dialog is starting a root session.
  const [newSessionParent, setNewSessionParent] = useState<AgentSession | null>(null);
  // The session awaiting a delete confirmation, with how many go with it.
  const [pendingDelete, setPendingDelete] = useState<{
    session: AgentSession;
    descendants: number;
  } | null>(null);
  const unpin = useAgentPinsStore((state) => state.unpin);

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
    setErrorReason("");
    setSandboxHint("");
    try {
      const session = await agentHubCreateSession({
        agentType: params.vendor,
        cwd: params.cwd,
        title: params.title,
        parentSessionId: newSessionParent?.id,
      });
      upsertSession(session);
      setActiveSessionId(session.id);
      setNewSessionOpen(false);
      setNewSessionParent(null);
    } catch (e) {
      reportError(e);
      if (params.vendor === "claude" && isSandboxUnavailableError(e)) {
        setSandboxHint(sandboxUnavailableMessage());
      }
    } finally {
      setBusy(false);
    }
  }

  function reportError(e: unknown) {
    const app = toAppError(e);
    setError(app.message);
    setErrorReason(app.code === "unknown" ? app.message : `${app.code} — ${app.message}`);
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
      reportError(e);
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
      reportError(e);
    }
  }

  // The header acts on the active session; the tree's context menu acts on
  // whichever row was right-clicked, which is often not the active one.
  async function handleCancelFor(session: AgentSession) {
    try {
      await agentHubCancel(session.id);
      upsertSession(await agentHubGetStatus(session.id));
    } catch (e) {
      reportError(e);
    }
  }

  async function handleCloseFor(session: AgentSession) {
    try {
      await agentHubCloseSession(session.id);
      upsertSession(await agentHubGetStatus(session.id));
      if (session.id === activeSession?.id) {
        const next = sessions.find((s) => s.id !== session.id && s.status !== "closed");
        setActiveSessionId(next ? next.id : "");
      }
    } catch (e) {
      reportError(e);
    }
  }

  async function handleDeleteConfirmed() {
    const target = pendingDelete?.session;
    setPendingDelete(null);
    if (!target) return;
    try {
      await agentHubDeleteSession(target.id);
      // The backend takes the subtree, so the store has to as well — a child
      // left in the list would render as a root pointing at a session that no
      // longer exists.
      const gone = new Set<string>([target.id]);
      let grew = true;
      while (grew) {
        grew = false;
        for (const s of sessions) {
          if (!gone.has(s.id) && s.parentSessionId && gone.has(s.parentSessionId)) {
            gone.add(s.id);
            grew = true;
          }
        }
      }
      for (const id of gone) {
        removeSession(id);
        unpin(id);
      }
      if (activeSession && gone.has(activeSession.id)) {
        const next = sessions.find((s) => !gone.has(s.id));
        setActiveSessionId(next ? next.id : "");
      }
    } catch (e) {
      reportError(e);
    }
  }

  async function handleCloseSession() {
    if (!activeSession) return;
    const closedId = activeSession.id;
    setBusy(true);
    try {
      await agentHubCloseSession(closedId);
      upsertSession(await agentHubGetStatus(closedId));
      // The session stays in the tree as history, but selecting it would show
      // a dead transcript under a live composer. Move to the next agent that
      // can still be talked to, or to the empty state if there is none.
      const next = sessions.find((s) => s.id !== closedId && s.status !== "closed");
      setActiveSessionId(next ? next.id : "");
    } catch (e) {
      reportError(e);
    } finally {
      setBusy(false);
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
    onNewSession: () => {
      setNewSessionParent(null);
      setNewSessionOpen(true);
    },
    onNewChildSession: (parent) => {
      setNewSessionParent(parent);
      setNewSessionOpen(true);
    },
    onCancelTurn: (session) => void handleCancelFor(session),
    onCloseSession: (session) => void handleCloseFor(session),
    onDeleteSession: (session, descendants) => setPendingDelete({ session, descendants }),
    onCopy: (text) => void copyText(text),
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
            onCloseSession={() => void handleCloseSession()}
            streaming={busy || activeSession?.status === "running"}
            errorMessage={errorReason || error}
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
            setErrorReason("");
            setSandboxHint("");
            setNewSessionParent(null);
          }
        }}
        vendors={vendors}
        busy={busy}
        error={error || sandboxHint}
        parentName={newSessionParent?.title ?? undefined}
        depth={newSessionParent ? newSessionParent.depth + 1 : undefined}
        defaultCwd={newSessionParent?.projectPath ?? ""}
        suggestedCwd={codeGraphCwd}
        onBrowse={() => agentHubChooseDirectory(codeGraphCwd)}
        onCreate={handleCreateSession}
      />

      {/* Delete is the one action here with nothing behind it: close keeps the
          row as history, this removes the transcript for good. */}
      <AlertDialog
        open={Boolean(pendingDelete)}
        onOpenChange={(next) => {
          if (!next) setPendingDelete(null);
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {`Delete ${pendingDelete?.session.title || "this session"}?`}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {pendingDelete?.descendants
                ? `Its transcript and ${pendingDelete.descendants} child session${
                    pendingDelete.descendants === 1 ? "" : "s"
                  } will be deleted for good. Any of them still running will be stopped. This cannot be undone.`
                : "Its transcript will be deleted for good, and the agent stopped if it is still running. This cannot be undone."}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction onClick={() => void handleDeleteConfirmed()}>
              Delete
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
