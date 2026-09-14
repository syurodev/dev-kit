import { useEffect, useMemo, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  CLAUDE_AGENT,
  agentRuntimeStatus,
  isAgentAuthRequiredError,
  isAgentNotInstalledError,
  isRuntimeRequirementError,
  runtimeBlocked,
} from "../../bridge/agentRuntime";
import { agentRuntimeQueryKey } from "../settings/AgentRuntimeSetting";
import { requestSettings } from "../../workbench/settingsRequest";
import { ClaudeAgentSetup } from "./ClaudeAgentSetup";
import { ClaudeSignIn, useClaudeSignIn } from "./ClaudeSignIn";
import type { SessionErrorCause } from "./surfaces";
import {
  agentHubCancel,
  agentHubChooseDirectory,
  agentHubChooseFiles,
  agentHubCloseSession,
  agentHubRenameSession,
  agentHubCreateSession,
  agentHubDeleteSession,
  agentHubGetStatus,
  agentHubListVendorTypes,
  agentHubListContextFiles,
  agentHubSetMode,
  agentHubSetConfigOption,
  CLAUDE_VENDOR,
  isSandboxUnavailableError,
  sandboxUnavailableMessage,
  type AgentSession,
  type AgentVendor,
  type AgentContextFile,
  type AgentImageAttachment,
} from "../../bridge/agenthub";
import { codeGraphProjectRoot, openCodeGraphProject, readCodeGraphFile } from "../../bridge/codegraph";
import { useCodeGraphStore } from "../codegraph/codegraphStore";
import { useWorkbenchTabsStore } from "../../stores/workbenchTabs";
import { copyText } from "../../bridge/clipboard";
import { toAppError } from "../../bridge/errors";
import { useAgentHubSidebar } from "./useAgentHubSidebar";
import { forgetAgentTranscript, updateTranscriptControls, useAgentTranscript } from "./transcript";
import { AgentHubContent } from "./surfaces";
import { useAgentSessionList, useAgentSessionsStore } from "../../stores/agentSessions";
import { useAgentPinsStore } from "../../stores/agentPins";
import { useModuleVisibility } from "@/components/ui/module-visibility";
import { imageAttachmentFromFile } from "./imageAttachments";
import type { QueuedAgentPrompt } from "../../stores/agentSessions";
import { queuedPrompt, resumeQueuedPrompts, sendAgentTurn } from "../../workbench/agentPromptQueue";
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

// Until the backend answers. Claude is the one agent DevKit ships; the mock
// adapter is a test double and is not offered.
const DEFAULT_VENDORS: AgentVendor[] = ["claude"];

/** A session to create, and the first message that is the reason for it. */
type CreateParams = {
  vendor: string;
  cwd: string;
  parentSessionId?: string;
  prompt?: string;
  attachments?: string[];
  images?: AgentImageAttachment[];
};
type TurnInput = { sessionId: string; text: string; attachments: string[]; images: AgentImageAttachment[] };
/** The action that failed on sign-in, retried once a login exits 0. */
type AuthRetry = { kind: "create"; params: CreateParams } | ({ kind: "turn" } & TurnInput);

export function AgentHubView() {
  const visible = useModuleVisibility();
  const sessions = useAgentSessionList();
  const activeSessionId = useAgentSessionsStore((state) => state.activeSessionId);
  const setActiveSessionId = useAgentSessionsStore((state) => state.setActiveSessionId);
  const pendingAgentHubCommand = useAgentSessionsStore((state) => state.pendingAgentHubCommand);
  const consumeAgentHubCommand = useAgentSessionsStore((state) => state.consumeAgentHubCommand);
  const markCompletedRead = useAgentSessionsStore((state) => state.markCompletedRead);
  const openTab = useWorkbenchTabsStore((state) => state.openTab);
  const upsertSession = useAgentSessionsStore((state) => state.upsertSession);
  const removeSession = useAgentSessionsStore((state) => state.removeSession);
  const setPrompt = useAgentSessionsStore((state) => state.setPrompt);
  const enqueuePrompt = useAgentSessionsStore((state) => state.enqueuePrompt);
  const removeQueuedPrompt = useAgentSessionsStore((state) => state.removeQueuedPrompt);
  const pendingQueue = useAgentSessionsStore((state) => state.pendingQueue);

  const [vendors, setVendors] = useState<string[]>(DEFAULT_VENDORS);
  // A session being started. Its folder and parent are known when it begins;
  // its agent is picked in the composer; nothing exists on the backend until
  // the first message is sent, because an agent is a process bound to its
  // session and there is nothing to bind yet. This replaced a dialog that asked
  // for the agent up front, before anyone had said what they wanted from it.
  const [draft, setDraft] = useState<{ cwd: string; parent: AgentSession | null } | null>(null);
  const [draftAgent, setDraftAgent] = useState<string>(CLAUDE_VENDOR);
  const [draftPrompt, setDraftPrompt] = useState("");
  const [draftImages, setDraftImages] = useState<AgentImageAttachment[]>([]);
  // Figma 948:2204 writes the reason as "agenthub.processFailed — the agent
  // exited after …": the DevKit code AND what happened. errorMessage() gives
  // only the humanised half, so the card was saying "Agent process failed."
  // — true, and no more actionable than the red dot the note complains about.
  const [errorReason, setErrorReason] = useState("");
  // Not installed / missing Node are not crashes; the card offers Settings.
  const [errorCause, setErrorCause] = useState<SessionErrorCause | undefined>(undefined);
  // What to do again once Claude is signed in. A failed create keeps its
  // params here while the dialog steps aside for the terminal.
  const [authRetry, setAuthRetry] = useState<AuthRetry | null>(null);
  // The last turn sent, so Retry resends it: the composer is cleared on send.
  const lastTurn = useRef<TurnInput | null>(null);
  const signIn = useClaudeSignIn((outcome) => {
    if (authRetry?.kind === "create") {
      // The draft is still on screen with its message, so a successful login
      // carries straight on: the session is created and the message sent.
      if (outcome.signedIn) void handleCreateSession(authRetry.params);
    } else if (authRetry?.kind === "turn" && outcome.signedIn) {
      void handleSend(authRetry);
    }
  });
  const queryClient = useQueryClient();
  // Asked while the draft has Claude picked, not after a send fails: the draft
  // shows the install panel as soon as Claude is chosen and cannot start.
  const claudeRuntime = useQuery({
    queryKey: agentRuntimeQueryKey(CLAUDE_AGENT),
    queryFn: () => agentRuntimeStatus(CLAUDE_AGENT),
    enabled: draft !== null && draftAgent === CLAUDE_VENDOR,
  });
  // The session awaiting a delete confirmation, with how many go with it.
  const [pendingDelete, setPendingDelete] = useState<{
    session: AgentSession;
    descendants: number;
  } | null>(null);
  const unpin = useAgentPinsStore((state) => state.unpin);
  // Per session: attachments belong to the turn you are composing, and
  // switching tabs must not carry someone else's files into your prompt.
  const [attachments, setAttachments] = useState<Record<string, string[]>>({});
  const [images, setImages] = useState<Record<string, AgentImageAttachment[]>>({});
  const [imageError, setImageError] = useState("");
  const [contextFiles, setContextFiles] = useState<Record<string, AgentContextFile[]>>({});

  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [sandboxHint, setSandboxHint] = useState("");
  const [focusComposerRequest, setFocusComposerRequest] = useState(0);

  const activeSession = useMemo(
    () => sessions.find((s) => s.id === activeSessionId) ?? sessions[0] ?? null,
    [sessions, activeSessionId],
  );

  const parentSession = useMemo(() => {
    if (!activeSession?.parentSessionId) return null;
    return sessions.find((s) => s.id === activeSession.parentSessionId) ?? null;
  }, [sessions, activeSession]);

  const activePrompt = useAgentSessionsStore(
    (state) => state.promptBySession[activeSession?.id ?? ""] ?? "",
  );
  const queuedPrompts = useAgentSessionsStore(
    (state) => state.queuedPromptsBySession[activeSession?.id ?? ""] ?? [],
  );
  const queuePaused = useAgentSessionsStore((state) =>
    Boolean(state.queuePausedBySession[activeSession?.id ?? ""]),
  );

  // Live typed transcript model powered by useAgentTranscript
  const transcriptModel = useAgentTranscript(activeSession?.id ?? "", { enabled: visible });

  useEffect(() => {
    if (!visible || !pendingAgentHubCommand) return;
    consumeAgentHubCommand(pendingAgentHubCommand);
    if (pendingAgentHubCommand === "create") {
      void chooseFolderForDraft();
    } else if (pendingAgentHubCommand === "cancel") {
      void handleCancel();
    } else {
      setFocusComposerRequest((request) => request + 1);
    }
  }, [consumeAgentHubCommand, pendingAgentHubCommand, visible]);

  useEffect(() => {
    if (visible && activeSession?.id) markCompletedRead(activeSession.id);
  }, [activeSession?.id, markCompletedRead, visible]);

  useEffect(() => {
    if (!visible) return;
    function handleKeyDown(event: KeyboardEvent) {
      if (
        event.isComposing ||
        event.repeat ||
        event.altKey ||
        event.shiftKey ||
        !(event.metaKey || event.ctrlKey) ||
        event.key.toLowerCase() !== "n"
      ) {
        return;
      }
      event.preventDefault();
      void chooseFolderForDraft();
    }
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [visible]);

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

  /** Starts a draft in a folder that is already known — a project row, a parent. */
  function startDraft(cwd: string, parent: AgentSession | null) {
    setDraft({ cwd, parent });
    setDraftPrompt("");
    setDraftImages([]);
    setError("");
    setErrorReason("");
    setErrorCause(undefined);
    setSandboxHint("");
    setAuthRetry(null);
  }

  /** Starts a draft in a folder the user picks, from "+ New" and the empty state. */
  async function chooseFolderForDraft() {
    // Opens where Code Graph is looking when it has a project open — the folder
    // you are most likely about to point an agent at. Nothing open is the
    // ordinary case, not an error.
    const start = await codeGraphProjectRoot().catch(() => "");
    try {
      const picked = await agentHubChooseDirectory(start);
      if (picked) startDraft(picked, null); // "" is a cancelled picker
    } catch (e) {
      reportError(e);
    }
  }

  async function handleCreateSession(params: CreateParams) {
    setBusy(true);
    setError("");
    setErrorReason("");
    setErrorCause(undefined);
    setSandboxHint("");
    setAuthRetry(null);
    try {
      const session = await agentHubCreateSession({
        agentType: params.vendor,
        cwd: params.cwd,
        parentSessionId: params.parentSessionId,
      });
      upsertSession(session);
      setActiveSessionId(session.id);
      setDraft(null);
      setDraftPrompt("");
      setDraftImages([]);
      if (params.prompt || params.attachments?.length || params.images?.length) {
        // The message was written before the session existed and is the reason
        // it was created. Passed the session itself: the list this closure
        // sees was read before the session was added to it.
        await handleSend(
          { sessionId: session.id, text: params.prompt ?? "", attachments: params.attachments ?? [], images: params.images ?? [] },
          session,
        );
      }
    } catch (e) {
      if (isAgentNotInstalledError(e) || isRuntimeRequirementError(e)) {
        // The status changed under the dialog (removed, Node uninstalled):
        // re-read it and the install panel takes the form's place.
        void queryClient.invalidateQueries({ queryKey: agentRuntimeQueryKey(CLAUDE_AGENT) });
        // The panel already says "not installed"; a requirement's reason stays.
        if (isAgentNotInstalledError(e)) return;
      }
      // The sign-in panel takes the form's place and says why.
      if (params.vendor === "claude" && isAgentAuthRequiredError(e)) {
        setAuthRetry({ kind: "create", params });
        return;
      }
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
    setErrorCause(
      isAgentNotInstalledError(app)
        ? "notInstalled"
        : isRuntimeRequirementError(app)
          ? "requirement"
          : isAgentAuthRequiredError(app)
            ? "authRequired"
            : undefined,
    );
  }

  async function handleBrowseContext() {
    if (!activeSession) return;
    try {
      const picked = await agentHubChooseFiles(activeSession.projectPath);
      if (picked.length === 0) return; // cancelled
      setAttachments((prev) => {
        const current = prev[activeSession.id] ?? [];
        // Picking the same file twice is a slip, not an instruction to send it
        // twice.
        const merged = [...current, ...picked.filter((p) => !current.includes(p))];
        return { ...prev, [activeSession.id]: merged };
      });
    } catch (e) {
      reportError(e);
    }
  }

  function handleRemoveAttachment(path: string) {
    if (!activeSession) return;
    setAttachments((prev) => ({
      ...prev,
      [activeSession.id]: (prev[activeSession.id] ?? []).filter((p) => p !== path),
    }));
  }

  async function handleAddImage(file: File) {
    try {
      const image = await imageAttachmentFromFile(file);
      setImageError("");
      if (activeSession) setImages((prev) => ({ ...prev, [activeSession.id]: [...(prev[activeSession.id] ?? []), image] }));
      else if (draft) setDraftImages((current) => [...current, image]);
    } catch (e) {
      setImageError(e instanceof Error ? e.message : "Could not attach image");
    }
  }

  function handleRemoveImage(id: string) {
    if (activeSession) {
      setImages((prev) => ({ ...prev, [activeSession.id]: (prev[activeSession.id] ?? []).filter((image) => image.id !== id) }));
    } else {
      setDraftImages((current) => current.filter((image) => image.id !== id));
    }
  }

  function queuePrompt(session: AgentSession, text: string, attached: string[], attachedImages: AgentImageAttachment[]) {
    enqueuePrompt(session.id, queuedPrompt(text, attached, attachedImages));
    setPrompt(session.id, "");
    setAttachments((prev) => ({ ...prev, [session.id]: [] }));
    setImages((prev) => ({ ...prev, [session.id]: [] }));
  }

  function editQueuedPrompt(queued: QueuedAgentPrompt) {
    if (!activeSession) return;
    setPrompt(activeSession.id, queued.text);
    setAttachments((prev) => ({ ...prev, [activeSession.id]: queued.attachments }));
    setImages((prev) => ({ ...prev, [activeSession.id]: queued.images }));
    removeQueuedPrompt(activeSession.id, queued.id);
  }

  function handleSelectContext(path: string) {
    if (!activeSession) return;
    setAttachments((prev) => {
      const current = prev[activeSession.id] ?? [];
      return current.includes(path)
        ? prev
        : { ...prev, [activeSession.id]: [...current, path] };
    });
  }

  async function handleOpenFile(path: string, line?: number) {
    if (!activeSession) return;
    const root = activeSession.projectPath.replace(/[\\/]+$/, "");
    const normalizedPath = path.split("\\").join("/");
    const normalizedRoot = root.split("\\").join("/");
    const relative = normalizedPath.startsWith(`${normalizedRoot}/`)
      ? normalizedPath.slice(normalizedRoot.length + 1)
      : normalizedPath;
    try {
      await openCodeGraphProject(root);
      const content = await readCodeGraphFile(relative);
      useCodeGraphStore.getState().set({
        projectPath: root,
        activeTab: "files",
        selectedFile: relative,
        selectedLine: line ?? null,
        fileContent: content,
        fileDirty: false,
        fileError: "",
      });
      openTab({ moduleId: "codegraph", viewId: "codegraph.explorer", title: "Code Graph" });
    } catch (e) {
      reportError(e);
    }
  }

  function handleSelectCommand(name: string) {
    if (!activeSession) return;
    const command = transcriptModel.commands.find((item) => item.name.replace(/^\//, "") === name.replace(/^\//, ""));
    const commandName = name.startsWith("/") ? name : `/${name}`;
    if (command?.input) {
      const current = useAgentSessionsStore.getState().promptBySession[activeSession.id] ?? "";
      setPrompt(activeSession.id, `${current}${current && !/\s$/.test(current) ? " " : ""}${commandName} `);
      return;
    }
    void handleSend({ sessionId: activeSession.id, text: commandName, attachments: [], images: [] });
  }

  async function handleSetMode(nextMode: string) {
    if (!activeSession) return;
    const previous = transcriptModel;
    updateTranscriptControls(activeSession.id, (model) => ({ ...model, mode: nextMode }));
    try {
      await agentHubSetMode(activeSession.id, nextMode);
    } catch (e) {
      updateTranscriptControls(activeSession.id, () => previous);
      reportError(e);
    }
  }

  async function handleSetConfigOption(optionId: string, value: string) {
    if (!activeSession) return;
    const previous = transcriptModel;
    updateTranscriptControls(activeSession.id, (model) => ({
      ...model,
      configOptions: model.configOptions.map((option) =>
        option.id === optionId ? { ...option, currentValue: value } : option,
      ),
    }));
    try {
      await agentHubSetConfigOption(activeSession.id, optionId, value);
    } catch (e) {
      updateTranscriptControls(activeSession.id, () => previous);
      reportError(e);
    }
  }

  /** Sends the composer's prompt, or `retry` — a turn that already failed once. */
  async function handleSend(retry?: TurnInput, known?: AgentSession) {
    const session = known ?? (retry ? sessions.find((s) => s.id === retry.sessionId) : activeSession);
    if (!session) return;
    if (session.status === "closed") return;
    const attached = retry ? retry.attachments : attachments[session.id] ?? [];
    const attachedImages = retry ? retry.images : images[session.id] ?? [];
    const text = retry ? retry.text : activePrompt.trim();
    // A prompt with attachments and no words is a turn: "look at these".
    if (!text && attached.length === 0 && attachedImages.length === 0) return;
    if (!known && (busy || session.status === "running" || session.status === "awaiting_permission")) {
      queuePrompt(session, text, attached, attachedImages);
      return;
    }
    if (busy) return;
    if (!retry) setPrompt(session.id, "");
    const turn = { sessionId: session.id, text, attachments: attached, images: attachedImages };
    lastTurn.current = turn;
    setBusy(true);
    setError("");
    setAuthRetry(null);
    try {
      // Resolves without a result when the backend was busy and the prompt was
      // queued instead; its files went into the queue with it.
      await sendAgentTurn(session.id, queuedPrompt(text, attached, attachedImages));
      // Cleared only once the turn was accepted: a failed send should leave the
      // files where the user put them rather than make them pick again.
      setAttachments((prev) => ({ ...prev, [session.id]: [] }));
      setImages((prev) => ({ ...prev, [session.id]: [] }));
    } catch (e) {
      reportError(e);
      if (isAgentAuthRequiredError(e)) setAuthRetry({ kind: "turn", ...turn });
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
        forgetAgentTranscript(id);
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

  async function handleRenameSession(title: string) {
    if (!activeSession) return;
    try {
      await agentHubRenameSession(activeSession.id, title);
      upsertSession({ ...activeSession, title });
    } catch (e) {
      reportError(e);
    }
  }



  // What stands between a draft and its first message, in the draft's own view.
  const draftBlocked =
    draftAgent === CLAUDE_VENDOR && Boolean(claudeRuntime.data && runtimeBlocked(claudeRuntime.data));
  const draftNotice = !draft ? null : draftBlocked ? (
    <ClaudeAgentSetup onOpenSettings={() => requestSettings("agents")} />
  ) : authRetry?.kind === "create" ? (
    <div data-testid="claude-sign-in-panel" className="flex w-full flex-col gap-3 rounded-sm bg-(--state-soft) p-4">
      <p className="text-body-13-medium text-text-primary">Claude isn&apos;t signed in</p>
      <p className="text-caption-11 text-text-tertiary">
        Sign in in the terminal that opens. The session starts and your message is sent as soon as
        the login finishes.
      </p>
      <ClaudeSignIn signIn={signIn} />
    </div>
  ) : null;

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
    onSelectSession: (id) => {
      setDraft(null);
      setActiveSessionId(id);
    },
    onNewSession: () => void chooseFolderForDraft(),
    onNewSessionIn: (projectPath) => startDraft(projectPath, null),
    onNewChildSession: (parent) => startDraft(parent.projectPath, parent),
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
        <div className="flex size-full min-h-0 flex-1 overflow-hidden">
          {/* The swarm tree is NOT drawn here — useAgentHubSidebar publishes it
              into the shell rail, which is where Figma's _Slot / Sidebar Content
              puts a module's rail. Drawing it here as well gave two vertical
              rails side by side whenever the shell sidebar was open. */}

          {/* Surface: Agent Hub / Content (1136×804) */}
          <AgentHubContent
            session={activeSession}
            sessionStatus={activeSession?.status}
            parentSession={parentSession}
            items={transcriptModel.items}
            usage={transcriptModel.usage}
            commands={transcriptModel.commands}
            contextFiles={contextFiles[activeSession?.id ?? ""] ?? []}
            onSelectCommand={handleSelectCommand}
            onSelectContext={handleSelectContext}
            onOpenFile={(path, line) => void handleOpenFile(path, line)}
            configOptions={transcriptModel.configOptions}
            mode={transcriptModel.mode}
            onSetMode={(mode) => void handleSetMode(mode)}
            onSetConfigOption={(optionId, value) => void handleSetConfigOption(optionId, value)}
            focusComposerRequest={focusComposerRequest}
            hasMore={transcriptModel.hasMore}
            loadingOlder={transcriptModel.loadingOlder}
            onLoadOlder={transcriptModel.loadOlder}
            draft={draft ? { projectPath: draft.cwd, parentName: draft.parent?.title || undefined } : null}
            agents={vendors}
            agent={draftAgent}
            onAgentChange={setDraftAgent}
            draftNotice={draftNotice}
            sendDisabled={draft ? busy || draftBlocked : activeSession?.status === "closed"}
            promptValue={draft ? draftPrompt : activePrompt}
            onPromptChange={(val) => {
              if (draft) setDraftPrompt(val);
              else if (activeSession) setPrompt(activeSession.id, val);
            }}
            onSend={() => {
              if (!draft) return void handleSend();
              if (!draftPrompt.trim() && draftImages.length === 0) return;
              void handleCreateSession({
                vendor: draftAgent,
                cwd: draft.cwd,
                parentSessionId: draft.parent?.id,
                prompt: draftPrompt.trim(),
                images: draftImages,
              });
            }}
            onCancel={() => void handleCancel()}
            onCloseSession={() => void handleCloseSession()}
            onRenameSession={(title) => handleRenameSession(title)}
            onCopy={(text) => void copyText(text)}
            onCompact={() => {
              // Through the same path as a typed prompt, so a compact is a turn
              // like any other: it shows, it can fail, it can be retried. Not
              // through the draft, which may hold something half-written.
              if (activeSession) {
                void handleSend({ sessionId: activeSession.id, text: "/compact", attachments: [], images: [] });
              }
            }}
            streaming={busy || activeSession?.status === "running"}
            errorMessage={errorReason || error || sandboxHint}
            errorCause={errorCause}
            onRetrySession={() =>
              void handleSend(
                lastTurn.current?.sessionId === activeSession?.id
                  ? (lastTurn.current ?? undefined)
                  : undefined,
              )
            }
            onNewSession={() => void chooseFolderForDraft()}
            onOpenSettings={() => requestSettings("agents")}
            errorDetail={
              errorCause === "authRequired" && activeSession?.agentType === "claude" ? (
                <ClaudeSignIn signIn={signIn} />
              ) : undefined
            }
            attachments={activeSession ? attachments[activeSession.id] ?? [] : []}
            imageAttachments={draft ? draftImages : activeSession ? images[activeSession.id] ?? [] : []}
            onRemoveImage={handleRemoveImage}
            onPasteImage={(file) => void handleAddImage(file)}
            onDropImage={(file) => void handleAddImage(file)}
            imageError={imageError}
            queuedPrompts={queuedPrompts}
            queuePaused={queuePaused}
            onResumeQueued={() => activeSession && resumeQueuedPrompts(activeSession.id)}
            onCancelQueued={(id) => activeSession && removeQueuedPrompt(activeSession.id, id)}
            onEditQueued={editQueuedPrompt}
            onBrowseContext={() => void handleBrowseContext()}
            onRemoveAttachment={handleRemoveAttachment}
          />
        </div>

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
