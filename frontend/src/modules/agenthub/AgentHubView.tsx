import { useEffect, useMemo, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  agentRuntimeStatus,
  isAgentApiKeyError,
  isAgentAuthRequiredError,
  isAgentNotInstalledError,
  isRuntimeRequirementError,
  runtimeBlocked,
} from "../../bridge/agentRuntime";
import { agentRuntimeQueryKey } from "../settings/AgentRuntimeSetting";
import { requestSettings } from "../../workbench/settingsRequest";
import { AgentRuntimeSetup } from "./AgentRuntimeSetup";
import { ClaudeSignIn, useClaudeSignIn } from "./ClaudeSignIn";
import { CodexSignIn, useCodexSignIn } from "./CodexSignIn";
import { SHIPPED_VENDORS, vendorInfo } from "./vendors";
import type { SessionErrorCause } from "./surfaces";
import {
  agentHubCancel,
  agentHubChooseDirectory,
  agentHubSetGoal,
  agentHubSteer,
  agentHubNewScratchDirectory,
  agentHubScratchRoot,
  agentHubChooseFiles,
  agentHubCloseSession,
  agentHubRenameSession,
  agentHubCreateSession,
  agentHubDeleteSession,
  agentHubGetStatus,
  agentHubListVendorTypes,
  agentHubListContextFiles,
  CLAUDE_VENDOR,
  isSandboxUnavailableError,
  sandboxUnavailableMessage,
  type AgentSession,
  type AgentContextFile,
  type AgentImageAttachment,
} from "../../bridge/agenthub";
import { codeGraphProjectRoot } from "../../bridge/codegraph";
import { dispatchIntent, type EntityRef } from "@/workbench/intents";
import { copyText } from "../../bridge/clipboard";
import { toAppError } from "../../bridge/errors";
import { useAgentHubSidebar } from "./useAgentHubSidebar";
import { forgetAgentTranscript, loadWholeTranscript, useAgentTranscript } from "./transcript";
import { transcriptToMarkdown } from "./exportMarkdown";
import { groupTurns } from "./turns";
import { toast } from "sonner";
import { pendingForSession, useResolvePermission } from "./permissions";
import { elicitationForSession, useRespondElicitation } from "./elicitations";
import { setSessionConfigOption, setSessionMode } from "./transcriptControls";
import { commandName, promptWithCommand, promptWithoutCommand } from "./slashCommands";
import { AgentHubContent } from "./surfaces";
import { RenameSessionDialog } from "./surfaces/RenameSessionDialog";
import { rateLimitFailure } from "./surfaces/AgentContextMeter";
import { useAgentSessionList, useAgentSessionsStore } from "../../stores/agentSessions";
import { useAgentPinsStore } from "../../stores/agentPins";
import { useModuleFolder } from "../../stores/workFolder";
import { useCurrentWorkspaceServices } from "@/stores/workspaceSwitcher";
import { useSidebarWorkspaces } from "./sidebarWorkspaces";
import { useModuleVisibility } from "@/components/ui/module-visibility";
import { detectPlatform } from "@/lib/platform";
import { EMPTY_ARRAY } from "@/lib/stableEmpty";
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

// Until the backend answers: the agents DevKit ships. The mock adapter is a
// test double and is not offered.
const DEFAULT_VENDORS: string[] = [...SHIPPED_VENDORS];

/** A session to create, and the first message that is the reason for it. */
type CreateParams = {
  vendor: string;
  cwd: string;
  parentSessionId?: string;
  prompt?: string;
  attachments?: string[];
  images?: AgentImageAttachment[];
  /** The rest of a workspace's services — see chooseWorkspaceForDraft. */
  extraReadWritePaths?: string[];
};
type TurnInput = {
  sessionId: string;
  text: string;
  attachments: string[];
  images: AgentImageAttachment[];
  refs: EntityRef[];
};
/** The action that failed on sign-in, retried once the sign-in succeeds. */
type AuthRetry = { kind: "create"; params: CreateParams } | ({ kind: "turn" } & TurnInput);

type ShortcutKey = Pick<
  KeyboardEvent,
  "key" | "metaKey" | "ctrlKey" | "altKey" | "shiftKey" | "repeat" | "isComposing" | "defaultPrevented"
>;

/**
 * ⌘N on macOS, Ctrl+N elsewhere — not either on both: on a Mac, Ctrl+N is the
 * text field's own next-line. Stands aside for a key something else already
 * handled, an IME composition, and an open dialog or menu, where the key
 * belongs to what is open.
 */
export function isNewSessionShortcut(event: ShortcutKey, mac: boolean, overlayOpen: boolean): boolean {
  if (event.defaultPrevented || event.isComposing || event.repeat || event.altKey || event.shiftKey || overlayOpen) {
    return false;
  }
  const modifier = mac ? event.metaKey && !event.ctrlKey : event.ctrlKey && !event.metaKey;
  return modifier && event.key.toLowerCase() === "n";
}

/**
 * Whether a message should go into the turn that is running rather than behind
 * it. Text only: steering carries content blocks, and files belong to a turn of
 * their own. Not while the agent waits on a permission — that turn is parked on
 * the user, and the card asking is already on screen.
 */
export function shouldSteer(input: {
  status: string;
  text: string;
  attachments: number;
  images: number;
  refs?: number;
  busy: boolean;
  retrying: boolean;
}): boolean {
  return (
    !input.retrying &&
    !input.busy &&
    input.text.length > 0 &&
    input.attachments === 0 &&
    input.images === 0 &&
    (input.refs ?? 0) === 0 &&
    input.status === "running"
  );
}

/** A failure and its revision: the agent republishes one when it changes. */
export function failureKey(failure: { id: string; revision: number }): string {
  return `${failure.id}:${failure.revision}`;
}

/** Renames a session. Throws, so the rename dialog stays open and says why. */
export async function renameAgentSession(
  sessionId: string,
  title: string,
  rename: (sessionId: string, title: string) => Promise<void> = agentHubRenameSession,
): Promise<void> {
  await rename(sessionId, title);
  const { sessionsById, upsertSession } = useAgentSessionsStore.getState();
  const session = sessionsById[sessionId];
  // The store's row, not the one the view rendered: its status may have moved on.
  if (session) upsertSession({ ...session, title });
}

/**
 * What a failed create turns into, for the vendor that failed — read from the
 * vendor table, so the install panel, the sign-in and the sandbox hint are
 * Codex's as much as Claude's, and an agent with none of them (the mock) gets
 * the plain error card.
 */
export function createFailure(vendor: string, error: unknown) {
  const info = vendorInfo(vendor);
  const runtime = info.runtimeId !== "" && (isAgentNotInstalledError(error) || isRuntimeRequirementError(error));
  return {
    /** The runtime whose status to re-read, or "". */
    refreshRuntime: runtime ? info.runtimeId : "",
    /** Nothing to add: the install panel already says it. */
    shownByInstallPanel: runtime && isAgentNotInstalledError(error),
    /** Show the vendor's own sign-in, and retry the create once it lands. */
    signIn: info.signIn !== "none" && isAgentAuthRequiredError(error),
    /** The chosen API key failed: the error card sends the person to Settings. */
    apiKey: isAgentApiKeyError(error),
    sandboxHint: info.runtimeId !== "" && isSandboxUnavailableError(error),
  };
}

export function AgentHubView() {
  // Where the app is working, for a new draft to start in.
  const { folder: workFolder } = useModuleFolder("agenthub");
  const visible = useModuleVisibility();
  const sessions = useAgentSessionList();
  const activeSessionId = useAgentSessionsStore((state) => state.activeSessionId);
  const setActiveSessionId = useAgentSessionsStore((state) => state.setActiveSessionId);
  const pendingAgentHubCommand = useAgentSessionsStore((state) => state.pendingAgentHubCommand);
  const consumeAgentHubCommand = useAgentSessionsStore((state) => state.consumeAgentHubCommand);
  const markCompletedRead = useAgentSessionsStore((state) => state.markCompletedRead);
  const upsertSession = useAgentSessionsStore((state) => state.upsertSession);
  const removeSession = useAgentSessionsStore((state) => state.removeSession);
  const setPrompt = useAgentSessionsStore((state) => state.setPrompt);
  const enqueuePrompt = useAgentSessionsStore((state) => state.enqueuePrompt);
  const removeQueuedPrompt = useAgentSessionsStore((state) => state.removeQueuedPrompt);
  const pendingQueue = useAgentSessionsStore((state) => state.pendingQueue);
  const dequeuePendingForSession = useAgentSessionsStore((state) => state.dequeuePendingForSession);
  const elicitationQueue = useAgentSessionsStore((state) => state.elicitationQueue);
  const dequeueElicitationForSession = useAgentSessionsStore((state) => state.dequeueElicitationForSession);

  const [vendors, setVendors] = useState<string[]>(DEFAULT_VENDORS);
  // A session being started. Its folder and parent are known when it begins;
  // its agent is picked in the composer; nothing exists on the backend until
  // the first message is sent, because an agent is a process bound to its
  // session and there is nothing to bind yet. This replaced a dialog that asked
  // for the agent up front, before anyone had said what they wanted from it.
  // Images pasted into the draft belong to it: they go when it is sent or left.
  const [draft, setDraft] = useState<{
    cwd: string;
    parent: AgentSession | null;
    images: AgentImageAttachment[];
    // The rest of a workspace's services, when "grant every service" was
    // picked instead of one folder — see chooseWorkspaceForDraft.
    extraReadWritePaths: string[];
  } | null>(null);
  const [draftAgent, setDraftAgent] = useState<string>(CLAUDE_VENDOR);
  const [draftPrompt, setDraftPrompt] = useState("");
  // Figma 948:2204 writes the reason as "agenthub.processFailed — the agent
  // exited after …": the DevKit code AND what happened. errorMessage() gives
  // only the humanised half, so the card was saying "Agent process failed."
  // — true, and no more actionable than the red dot the note complains about.
  const [errorReason, setErrorReason] = useState("");
  // Not installed / missing Node are not crashes; the card offers Settings.
  const [errorCause, setErrorCause] = useState<SessionErrorCause | undefined>(undefined);
  // What to do again once the agent is signed in. A failed create keeps its
  // params here while the draft shows the sign-in instead of sending.
  const [authRetry, setAuthRetry] = useState<AuthRetry | null>(null);
  // The last turn sent, so Retry resends it: the composer is cleared on send.
  const lastTurn = useRef<TurnInput | null>(null);
  const retryAfterSignIn = (outcome: { signedIn: boolean }) => {
    if (authRetry?.kind === "create") {
      // The draft is still on screen with its message, so a successful login
      // carries straight on: the session is created and the message sent.
      if (outcome.signedIn) void handleCreateSession(authRetry.params);
    } else if (authRetry?.kind === "turn" && outcome.signedIn) {
      void handleSend(authRetry);
    }
  };
  // One per way of signing in (vendors.ts): Claude's runs its login in a
  // terminal, Codex's inside DevKit. Both retry the same way.
  const claudeSignIn = useClaudeSignIn(retryAfterSignIn);
  const codexSignIn = useCodexSignIn(retryAfterSignIn);
  const queryClient = useQueryClient();
  // Asked while the draft has an agent picked, not after a send fails: the
  // draft shows the install panel as soon as an agent that cannot start is
  // chosen.
  const draftVendor = vendorInfo(draftAgent);
  const draftRuntime = useQuery({
    queryKey: agentRuntimeQueryKey(draftVendor.runtimeId),
    queryFn: () => agentRuntimeStatus(draftVendor.runtimeId),
    enabled: draft !== null && draftVendor.runtimeId !== "",
  });
  // A vendor failure the user waved off. Keyed by id and revision: the agent
  // republishes a failure with a new revision when something about it changed,
  // and that is news again.
  const [dismissedFailure, setDismissedFailure] = useState("");
  // The row the sidebar asked to rename — not always the one on screen.
  const [pendingRename, setPendingRename] = useState<AgentSession | null>(null);
  // The session awaiting a delete confirmation, with how many go with it.
  const [pendingDelete, setPendingDelete] = useState<{
    session: AgentSession;
    descendants: number;
  } | null>(null);
  const unpin = useAgentPinsStore((state) => state.unpin);
  const recentProjects = useAgentPinsStore((state) => state.recentProjects);
  const rememberProject = useAgentPinsStore((state) => state.rememberProject);
  const { workspace: currentWorkspace, services: workspaceServices } = useCurrentWorkspaceServices();
  const sidebarWorkspaces = useSidebarWorkspaces();
  // Where "no project" sessions get their folders. Read once: it is a fixed
  // place under the config dir, and the sidebar groups everything under it.
  const scratchRoot = useQuery({
    queryKey: ["agenthub", "scratchRoot"],
    queryFn: agentHubScratchRoot,
    staleTime: Infinity,
  }).data ?? "";
  // Per session: attachments belong to the turn you are composing, and
  // switching tabs must not carry someone else's files into your prompt.
  const [attachments, setAttachments] = useState<Record<string, string[]>>({});
  const [images, setImages] = useState<Record<string, AgentImageAttachment[]>>({});
  // Cross-module refs an `agent.attach` intent handed this session — see the
  // pendingAgentAttach effect below. Per session for the same reason as
  // attachments: switching tabs must not carry someone else's refs along.
  const [refs, setRefs] = useState<Record<string, EntityRef[]>>({});
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

  // A cross-module `agent.attach` (docs/dev-kit/cross-module.mdx § Primitive
  // 2) leaves its refs here rather than reaching into this component
  // directly, since the caller — a manifest handler — has no reference to
  // whichever composer is actually mounted (this view's, or the companion's;
  // AgentCompanion.tsx has the matching effect). Applied to whichever session
  // is active when it lands, same as askAgent does for plain text.
  const pendingAgentAttach = useAgentSessionsStore((state) => state.pendingAgentAttach);
  useEffect(() => {
    if (!pendingAgentAttach || !activeSession) return;
    const { refs: attachedRefs, prompt } = pendingAgentAttach;
    setRefs((prev) => ({
      ...prev,
      [activeSession.id]: [...(prev[activeSession.id] ?? []), ...attachedRefs],
    }));
    if (prompt) {
      const draft = useAgentSessionsStore.getState().promptBySession[activeSession.id] ?? "";
      setPrompt(activeSession.id, draft ? `${draft}\n\n${prompt}` : prompt);
    }
    useAgentSessionsStore.getState().consumeAgentAttach();
  }, [pendingAgentAttach, activeSession, setPrompt]);

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

  useEffect(() => {
    if (!visible || !pendingAgentHubCommand) return;
    consumeAgentHubCommand(pendingAgentHubCommand);
    if (pendingAgentHubCommand === "create") {
      void startNewDraft();
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
      const overlayOpen = Boolean(document.querySelector('[role="dialog"], [role="alertdialog"], [role="menu"]'));
      if (!isNewSessionShortcut(event, detectPlatform() === "mac", overlayOpen)) return;
      event.preventDefault();
      void startNewDraft();
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
    setDraft({ cwd, parent, images: [], extraReadWritePaths: [] });
    setDraftPrompt("");
    setError("");
    setErrorReason("");
    setErrorCause(undefined);
    setSandboxHint("");
    setAuthRetry(null);
  }

  /**
   * "Grant every service" in the draft's picker: cwd anchors on the first
   * one (a concrete WorkDir the sandbox can bind), the rest become
   * ExtraReadWritePaths — a workspace's services do not necessarily share a
   * root cwd alone could cover (docs/dev-kit/workspace.mdx).
   */
  function chooseWorkspaceForDraft(paths: string[]) {
    if (paths.length === 0) return;
    const [cwd, ...rest] = paths;
    setDraft((current) => current && { ...current, cwd, extraReadWritePaths: rest });
  }

  /**
   * Starts a draft straight away, from "+ New", ⌘N and the empty state. The
   * folder is the one the app is working in, else the last one used, else where
   * Code Graph is looking, else none; the draft's project picker changes it
   * before the first message.
   */
  async function startNewDraft() {
    const recent = useAgentPinsStore.getState().recentProjects[0];
    const cwd = workFolder || recent || (await codeGraphProjectRoot().catch(() => ""));
    startDraft(cwd, null);
  }

  /** "New project…" in the draft's picker: the native folder dialog. */
  async function chooseFolderForDraft() {
    // Opens where the draft already points, else the folder the app is working in.
    const start = draft?.cwd || workFolder || (await codeGraphProjectRoot().catch(() => ""));
    try {
      const picked = await agentHubChooseDirectory(start);
      // "" is a cancelled picker: the draft keeps the folder it had. Picking
      // one folder narrows back from "whole workspace" if that was chosen.
      if (picked) setDraft((current) => current && { ...current, cwd: picked, extraReadWritePaths: [] });
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
      const scratch = !params.cwd;
      if (scratch) {
        // No project: a scratch folder of its own, made now rather than when
        // the draft opened so an abandoned draft leaves nothing behind. Kept
        // in params, so a sign-in retry reuses it instead of making another.
        params = { ...params, cwd: await agentHubNewScratchDirectory() };
      }
      const session = await agentHubCreateSession({
        agentType: params.vendor,
        cwd: params.cwd,
        parentSessionId: params.parentSessionId,
        // A scratch session gets nothing beyond its own scratch folder — the
        // "whole workspace" pick only applies to a real cwd.
        extraReadWritePaths: scratch ? [] : params.extraReadWritePaths,
      });
      upsertSession(session);
      if (!scratch) rememberProject(params.cwd);
      setActiveSessionId(session.id);
      setDraft(null);
      setDraftPrompt("");
      if (params.prompt || params.attachments?.length || params.images?.length) {
        // The message was written before the session existed and is the reason
        // it was created. Passed the session itself: the list this closure
        // sees was read before the session was added to it.
        await handleSend(
          {
            sessionId: session.id,
            text: params.prompt ?? "",
            attachments: params.attachments ?? [],
            images: params.images ?? [],
            refs: [],
          },
          session,
        );
      }
    } catch (e) {
      const failure = createFailure(params.vendor, e);
      if (failure.refreshRuntime) {
        // The status changed under the dialog (removed, Node uninstalled):
        // re-read it and the install panel takes the form's place.
        void queryClient.invalidateQueries({ queryKey: agentRuntimeQueryKey(failure.refreshRuntime) });
      }
      // The panel already says "not installed"; a requirement's reason stays.
      if (failure.shownByInstallPanel) return;
      // The sign-in panel takes the form's place and says why.
      if (failure.signIn) {
        setAuthRetry({ kind: "create", params });
        return;
      }
      reportError(e);
      if (failure.sandboxHint) {
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
            : isAgentApiKeyError(app)
              ? "apiKey"
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
      else if (draft) setDraft((current) => current && { ...current, images: [...current.images, image] });
    } catch (e) {
      setImageError(e instanceof Error ? e.message : "Could not attach image");
    }
  }

  function handleRemoveImage(id: string) {
    if (activeSession) {
      setImages((prev) => ({ ...prev, [activeSession.id]: (prev[activeSession.id] ?? []).filter((image) => image.id !== id) }));
    } else {
      setDraft((current) => current && { ...current, images: current.images.filter((image) => image.id !== id) });
    }
  }

  function handleRemoveRef(ref: EntityRef) {
    if (!activeSession) return;
    setRefs((prev) => ({
      ...prev,
      [activeSession.id]: (prev[activeSession.id] ?? []).filter(
        (r) => !(r.module === ref.module && r.kind === ref.kind && r.id === ref.id),
      ),
    }));
  }

  function queuePrompt(
    session: AgentSession,
    text: string,
    attached: string[],
    attachedImages: AgentImageAttachment[],
    attachedRefs: EntityRef[],
  ) {
    enqueuePrompt(session.id, queuedPrompt(text, attached, attachedImages, attachedRefs));
    setPrompt(session.id, "");
    setAttachments((prev) => ({ ...prev, [session.id]: [] }));
    setImages((prev) => ({ ...prev, [session.id]: [] }));
    setRefs((prev) => ({ ...prev, [session.id]: [] }));
  }

  function editQueuedPrompt(queued: QueuedAgentPrompt) {
    if (!activeSession) return;
    setPrompt(activeSession.id, queued.text);
    setAttachments((prev) => ({ ...prev, [activeSession.id]: queued.attachments }));
    setImages((prev) => ({ ...prev, [activeSession.id]: queued.images }));
    setRefs((prev) => ({ ...prev, [activeSession.id]: queued.refs }));
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

  function handleOpenFile(path: string, line?: number) {
    if (!activeSession) return;
    // Beside: the session stays on screen next to the file it talks about.
    void dispatchIntent(
      "codegraph.reveal",
      {
        module: "codegraph",
        kind: "file",
        id: path,
        label: path.slice(path.lastIndexOf("/") + 1),
        project: activeSession.projectPath,
        line,
      },
      { placement: "beside" },
    );
  }

  function handleSelectCommand(name: string) {
    if (!activeSession) return;
    const command = transcriptModel.commands.find((item) => item.name.replace(/^\//, "") === name.replace(/^\//, ""));
    const current = useAgentSessionsStore.getState().promptBySession[activeSession.id] ?? "";
    if (command?.input) {
      // Left in the field for the argument to be typed after it. Replaces a
      // half-typed name rather than appending to it: picking /review after
      // typing "/rev" has to leave "/review ", not "/rev /review ".
      setPrompt(activeSession.id, promptWithCommand(current, name));
      return;
    }
    // No argument: the command is the whole turn. handleSend with an explicit
    // turn deliberately does not clear the field — a retry must not — so the
    // half-typed name that opened the list is taken out here, and only it: a
    // message already in the box is not ours to throw away.
    setPrompt(activeSession.id, promptWithoutCommand(current));
    void handleSend({ sessionId: activeSession.id, text: commandName(name), attachments: [], images: [], refs: [] });
  }

  async function handleSetMode(nextMode: string) {
    if (!activeSession) return;
    try {
      await setSessionMode(activeSession.id, nextMode);
    } catch (e) {
      reportError(e);
    }
  }

  async function handleSetConfigOption(optionId: string, value: string) {
    if (!activeSession) return;
    try {
      await setSessionConfigOption(activeSession.id, optionId, value);
    } catch (e) {
      reportError(e);
    }
  }

  /** Sends the composer's prompt, or `retry` — a turn that already failed once. */
  async function handleSend(retry?: TurnInput, known?: AgentSession) {
    const session = known ?? (retry ? sessions.find((s) => s.id === retry.sessionId) : activeSession);
    if (!session) return;
    const attached = retry ? retry.attachments : attachments[session.id] ?? [];
    const attachedImages = retry ? retry.images : images[session.id] ?? [];
    const attachedRefs = retry ? retry.refs : refs[session.id] ?? [];
    const text = retry ? retry.text : activePrompt.trim();
    // A prompt with attachments and no words is a turn: "look at these".
    if (!text && attached.length === 0 && attachedImages.length === 0 && attachedRefs.length === 0) return;
    // While the agent is working, a message goes INTO the turn rather than
    // behind it — the vendors' own apps read what you type mid-turn, and so
    // does this. Text only: steering carries content blocks, and the files
    // belong to a turn of their own. A refusal, an agent that cannot steer, or
    // a turn that ended in the meantime all fall through to the queue.
    if (
      shouldSteer({
        status: session.status,
        text,
        attachments: attached.length,
        images: attachedImages.length,
        refs: attachedRefs.length,
        busy,
        retrying: Boolean(known),
      })
    ) {
      if (await steerInto(session, text)) {
        setPrompt(session.id, "");
        return;
      }
    }
    if (!known && (busy || session.status === "running" || session.status === "awaiting_permission")) {
      queuePrompt(session, text, attached, attachedImages, attachedRefs);
      return;
    }
    if (busy) return;
    if (!retry) setPrompt(session.id, "");
    const turn = { sessionId: session.id, text, attachments: attached, images: attachedImages, refs: attachedRefs };
    lastTurn.current = turn;
    setBusy(true);
    setError("");
    setAuthRetry(null);
    try {
      // Resolves without a result when the backend was busy and the prompt was
      // queued instead; its files went into the queue with it.
      await sendAgentTurn(session.id, queuedPrompt(text, attached, attachedImages, attachedRefs));
      // Cleared only once the turn was accepted: a failed send should leave the
      // files where the user put them rather than make them pick again.
      setAttachments((prev) => ({ ...prev, [session.id]: [] }));
      setImages((prev) => ({ ...prev, [session.id]: [] }));
      setRefs((prev) => ({ ...prev, [session.id]: [] }));
    } catch (e) {
      reportError(e);
      if (isAgentAuthRequiredError(e)) setAuthRetry({ kind: "turn", ...turn });
    } finally {
      setBusy(false);
    }
  }

  /** True when the message reached the running turn; false leaves it to the queue. */
  async function steerInto(session: AgentSession, text: string): Promise<boolean> {
    try {
      return (await agentHubSteer(session.id, text)) === "injected";
    } catch {
      // Nothing is lost: the caller queues it instead, which is what this app
      // did for every mid-turn message before steering existed.
      return false;
    }
  }

  async function handleSetGoal(objective: string) {
    if (!activeSession) return;
    try {
      await agentHubSetGoal(activeSession.id, objective);
    } catch (e) {
      reportError(e);
    }
  }

  // The vendor's own hints for a failure, each mapped to the thing this app
  // already does for it.
  function handleFailureAction(action: string) {
    if (action === "retry") {
      void handleSend(lastTurn.current?.sessionId === activeSession?.id ? (lastTurn.current ?? undefined) : undefined);
      return;
    }
    if (action === "new_session") {
      void startNewDraft();
      return;
    }
    if (action === "login") {
      const methods = claudeSignIn.methods.data ?? [];
      // One way in: take it. Several: the error card's panel names them, and
      // picking for the user would be guessing. An agent that signs in
      // inside DevKit shows its panel, where the browser is the first button.
      if (vendorInfo(activeSession?.agentType).signIn === "terminal" && methods.length === 1) {
        claudeSignIn.start(methods[0]);
      } else {
        setErrorCause("authRequired");
      }
    }
  }

  async function handleCancel() {
    if (!activeSession) return;
    try {
      await agentHubCancel(activeSession.id);
      dequeuePendingForSession(activeSession.id);
      dequeueElicitationForSession(activeSession.id);
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
      dequeuePendingForSession(session.id);
      dequeueElicitationForSession(session.id);
      upsertSession(await agentHubGetStatus(session.id));
    } catch (e) {
      reportError(e);
    }
  }

  async function handleCloseFor(session: AgentSession) {
    try {
      await agentHubCloseSession(session.id);
      upsertSession(await agentHubGetStatus(session.id));
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

  // Every page, not the window on screen: a copy that stopped where the scroll
  // did would be the newest half of a conversation with no sign it was half.
  async function handleCopyTranscript(session: AgentSession) {
    try {
      const model = await loadWholeTranscript(session.id);
      await copyText(
        transcriptToMarkdown(model.items, {
          title: session.title,
          projectPath: session.projectPath,
          vendor: session.agentType,
        }),
      );
      const turns = groupTurns(model.items).length;
      toast.success(`Copied ${turns} turn${turns === 1 ? "" : "s"} as Markdown`);
    } catch (e) {
      reportError(e);
    }
  }

  /** The sign-in a vendor offers, wired to retry what was waiting on it. */
  function signInFor(vendor: string) {
    switch (vendorInfo(vendor).signIn) {
      case "terminal":
        return <ClaudeSignIn signIn={claudeSignIn} />;
      case "inApp":
        return <CodexSignIn signIn={codexSignIn} />;
      default:
        return undefined;
    }
  }

  // What stands between a draft and its first message, in the draft's own view.
  const draftBlocked =
    draftVendor.runtimeId !== "" && Boolean(draftRuntime.data && runtimeBlocked(draftRuntime.data));
  const draftNotice = !draft ? null : draftBlocked ? (
    <AgentRuntimeSetup vendor={draftAgent} onOpenSettings={() => requestSettings("agents")} />
  ) : authRetry?.kind === "create" ? (
    <div data-testid="agent-sign-in-panel" className="flex w-full flex-col gap-3 rounded-sm bg-(--state-soft) p-4">
      <p className="text-body-13-medium text-text-primary">{`${draftVendor.name} isn't signed in`}</p>
      <p className="text-caption-11 text-text-tertiary">
        {draftVendor.signIn === "terminal"
          ? "Sign in in the terminal that opens. The session starts and your message is sent as soon as the login finishes."
          : "Sign in below. The session starts and your message is sent as soon as the sign-in finishes."}
      </p>
      {signInFor(authRetry.params.vendor)}
    </div>
  ) : null;

  // The request this session is waiting on is asked inside the conversation,
  // not in the corner: see AgentHubContent's pendingPermission.
  const activePending = useMemo(
    () => pendingForSession(pendingQueue, activeSession?.id),
    [pendingQueue, activeSession?.id],
  );
  const { busy: permissionBusy, resolve: resolvePermission } = useResolvePermission(activePending);
  const activeQuestion = useMemo(
    () => elicitationForSession(elicitationQueue, activeSession?.id),
    [elicitationQueue, activeSession?.id],
  );
  const {
    busy: elicitationBusy,
    error: elicitationError,
    respond: respondElicitation,
  } = useRespondElicitation(activeQuestion);

  // What each session is waiting on the user for: permissions and questions
  // alike, since either one stops the agent until someone answers.
  const pendingCounts = useMemo(() => {
    const map: Record<string, number> = {};
    for (const p of [...pendingQueue, ...elicitationQueue]) {
      map[p.sessionId] = (map[p.sessionId] ?? 0) + 1;
    }
    return map;
  }, [pendingQueue, elicitationQueue]);

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

  useAgentHubSidebar({
    sessions,
    workspaces: sidebarWorkspaces,
    scratchRoot,
    activeSessionId: activeSession?.id,
    pendingCounts,
    onSelectSession: (id) => {
      setDraft(null);
      setActiveSessionId(id);
    },
    onNewSession: () => void startNewDraft(),
    // The "No project" group's + starts another scratch session, not one in
    // the scratch root itself. A workspace's + is the whole workspace: its
    // primary as cwd, the other services granted beside it.
    onNewSessionIn: (projectPath, workspacePaths) => {
      startDraft(projectPath === scratchRoot ? "" : projectPath, null);
      if (workspacePaths && workspacePaths.length > 1) chooseWorkspaceForDraft(workspacePaths);
    },
    onNewChildSession: (parent) => startDraft(parent.projectPath, parent),
    onCancelTurn: (session) => void handleCancelFor(session),
    onCloseSession: (session) => void handleCloseFor(session),
    onRenameSession: (session) => setPendingRename(session),
    onCopyTranscript: (session) => void handleCopyTranscript(session),
    onDeleteSession: (session, descendants) => setPendingDelete({ session, descendants }),
    onCopy: (text) => void copyText(text),
  });

  // The vendor's own failure wins when there is one; Claude never reports a
  // rate-limit rejection through that channel (only through usage_update's
  // rateLimit), so without this a rejected turn had no banner at all — just
  // the composer's context-meter popover, which nobody opens unprompted.
  const activeFailure = transcriptModel.failure ?? rateLimitFailure(transcriptModel.usage);

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
            items={transcriptModel.items}
            usage={transcriptModel.usage}
            commands={transcriptModel.commands}
            contextFiles={contextFiles[activeSession?.id ?? ""] ?? []}
            onSelectCommand={handleSelectCommand}
            onSelectContext={handleSelectContext}
            onOpenFile={(path, line) => void handleOpenFile(path, line)}
            configOptions={transcriptModel.configOptions}
            mode={transcriptModel.mode}
            availableModes={transcriptModel.availableModes}
            onSetMode={(mode) => void handleSetMode(mode)}
            onSetConfigOption={(optionId, value) => void handleSetConfigOption(optionId, value)}
            focusComposerRequest={focusComposerRequest}
            hasMore={transcriptModel.hasMore}
            loadingOlder={transcriptModel.loadingOlder}
            onLoadOlder={transcriptModel.loadOlder}
            draft={
              draft
                ? {
                    projectPath: draft.cwd,
                    parentName: draft.parent?.title || undefined,
                    workspaceSelected: draft.extraReadWritePaths.length > 0,
                  }
                : null
            }
            projects={draftProjects}
            onProjectChange={(path) =>
              setDraft((current) => current && { ...current, cwd: path, extraReadWritePaths: [] })
            }
            onChooseProject={() => void chooseFolderForDraft()}
            workspace={
              currentWorkspace && workspaceServices.length > 0
                ? { name: currentWorkspace.name, paths: workspaceServices.map((s) => s.path) }
                : null
            }
            onChooseWorkspace={chooseWorkspaceForDraft}
            agents={vendors}
            agent={draftAgent}
            onAgentChange={(agent) => {
              setDraftAgent(agent);
              // A sign-in waiting for one agent is not the next one's: the
              // panel would name one vendor and sign in to the other.
              setAuthRetry(null);
            }}
            draftNotice={draftNotice}
            sendDisabled={draft ? busy || draftBlocked : false}
            promptValue={draft ? draftPrompt : activePrompt}
            onPromptChange={(val) => {
              if (draft) setDraftPrompt(val);
              else if (activeSession) setPrompt(activeSession.id, val);
            }}
            onSend={() => {
              if (!draft) return void handleSend();
              if (!draftPrompt.trim() && draft.images.length === 0) return;
              void handleCreateSession({
                vendor: draftAgent,
                cwd: draft.cwd,
                parentSessionId: draft.parent?.id,
                prompt: draftPrompt.trim(),
                images: draft.images,
                extraReadWritePaths: draft.extraReadWritePaths,
              });
            }}
            onCancel={() => void handleCancel()}
            onCopy={(text) => void copyText(text)}
            failure={
              activeFailure && failureKey(activeFailure) !== dismissedFailure ? activeFailure : null
            }
            onFailureAction={handleFailureAction}
            onDismissFailure={() =>
              setDismissedFailure(activeFailure ? failureKey(activeFailure) : "")
            }
            goal={transcriptModel.goal}
            onSetGoal={(objective) => void handleSetGoal(objective)}
            pendingPermission={activePending}
            onResolvePermission={resolvePermission}
            permissionBusy={permissionBusy}
            pendingElicitation={activeQuestion}
            onRespondElicitation={respondElicitation}
            elicitationBusy={elicitationBusy}
            elicitationError={elicitationError}
            onCompact={() => {
              // Through the same path as a typed prompt, so a compact is a turn
              // like any other: it shows, it can fail, it can be retried. Not
              // through the draft, which may hold something half-written.
              if (activeSession) {
                void handleSend({ sessionId: activeSession.id, text: "/compact", attachments: [], images: [], refs: [] });
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
            onNewSession={() => void startNewDraft()}
            onOpenSettings={() => requestSettings("agents")}
            errorDetail={
              errorCause === "authRequired" && activeSession ? signInFor(activeSession.agentType) : undefined
            }
            attachments={activeSession ? attachments[activeSession.id] ?? [] : []}
            imageAttachments={draft ? draft.images : activeSession ? images[activeSession.id] ?? [] : []}
            onRemoveImage={handleRemoveImage}
            refs={activeSession ? refs[activeSession.id] ?? [] : []}
            onRemoveRef={handleRemoveRef}
            onPasteImage={(file) => void handleAddImage(file)}
            onDropImage={(file) => void handleAddImage(file)}
            imageError={imageError}
            queuedPrompts={queuedPrompts}
            queuePaused={queuePaused}
            queuePausedReason={queuePausedReason}
            onResumeQueued={() => activeSession && resumeQueuedPrompts(activeSession.id)}
            onCancelQueued={(id) => activeSession && removeQueuedPrompt(activeSession.id, id)}
            onEditQueued={editQueuedPrompt}
            onBrowseContext={() => void handleBrowseContext()}
            onRemoveAttachment={handleRemoveAttachment}
          />
        </div>

      <RenameSessionDialog
        initialTitle={pendingRename ? pendingRename.title || `Session ${pendingRename.id.slice(0, 8)}` : null}
        // Not caught: the dialog shows the reason and stays open.
        onRename={(title) => (pendingRename ? renameAgentSession(pendingRename.id, title) : undefined)}
        onClose={() => setPendingRename(null)}
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
