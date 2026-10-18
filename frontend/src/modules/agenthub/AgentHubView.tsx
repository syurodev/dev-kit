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
  agentHubRenameSession,
  agentHubCreateSession,
  agentHubConfigCatalog,
  agentHubPrepareSession,
  agentHubResumeSession,
  agentHubGetStatus,
  agentHubListVendorTypes,
  agentHubListContextFiles,
  agentHubListDraftContextFiles,
  CLAUDE_VENDOR,
  OPENCODE_VENDOR,
  isSandboxUnavailableError,
  sandboxUnavailableMessage,
  type AgentSession,
  type AgentContextFile,
  type AgentImageAttachment,
} from "../../bridge/agenthub";
import { codeGraphProjectRoot } from "../../bridge/codegraph";
import type { EntityRef } from "@/workbench/intents";
import { copyText } from "../../bridge/clipboard";
import { errorMessage, toAppError } from "../../bridge/errors";
import { useAgentTranscript } from "./transcript";
import { openFileRef, refContextOf } from "./transcript/openReference";
import { toast } from "sonner";
import { pendingForSession, useResolvePermission } from "./permissions";
import { elicitationForSession, useRespondElicitation } from "./elicitations";
import { setSessionConfigOption, setSessionMode } from "./transcriptControls";
import { commandName, promptWithCommand, promptWithoutCommand } from "./slashCommands";
import { AgentHubContent } from "./surfaces";
import { GettingStarted } from "@/workbench/GettingStarted";
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
import { EmptyState } from "@/components/ui/empty-state";
import { closeAgentSessionTabs, closeNewSessionTab, openAgentSession, openNewAgentSession, sessionTitle, useAgentDraftRequests } from "./documents";
import { useSplitPanelActions } from "@/components/ui/split-panel";
import { AgentActivityPanel, useFollowing } from "./follow";
import { SubagentPanel, useSubagentPanel } from "./subagents";
import { draftConfigOptions, initialModelConfig } from "./modelSettings";
import { onAgentSignInCompleted } from "@/bridge/agentSignIn";

// Until the backend answers: the agents DevKit ships. The mock adapter is a
// test double and is not offered.
const DEFAULT_VENDORS: string[] = [...SHIPPED_VENDORS];

/** A session to create, and the first message that is the reason for it. */
type CreateParams = {
  vendor: string;
  cwd: string;
  workspaceId?: string;
  parentSessionId?: string;
  prompt?: string;
  attachments?: string[];
  images?: AgentImageAttachment[];
  /** The rest of a workspace's services — see chooseWorkspaceForDraft. */
  extraReadWritePaths?: string[];
  initialConfig?: Record<string, string>;
};
export type TurnInput = {
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

/**
 * The turn Retry / Restart sends again: the one this view sent last, when it
 * was for this session — it still has its files and images — and otherwise
 * the session's last prompt as the transcript recorded it. The fallback is
 * what makes the button work after a relaunch or on a session that failed in
 * another window: the view's memory of its last turn is gone there, and
 * Restart used to fall through to the empty composer and do nothing at all.
 *
 * A recorded prompt carries its words, not its files: the transcript keeps an
 * attachment's name, not the path or bytes it was sent from. A prompt with no
 * words has nothing to resend, and there is no retry (undefined).
 */
export function retryTurnFor(
  sessionId: string,
  last: TurnInput | null,
  items: readonly { kind: string; text?: string; origin?: string }[],
): TurnInput | undefined {
  if (!sessionId) return undefined;
  if (last?.sessionId === sessionId) return last;
  for (let index = items.length - 1; index >= 0; index -= 1) {
    const item = items[index];
    // A subagent's prompt is its parent's business, not this session's turn.
    if (item.kind !== "user_message_chunk" || item.origin) continue;
    const text = item.text?.trim() ?? "";
    return text ? { sessionId, text, attachments: [], images: [], refs: [] } : undefined;
  }
  return undefined;
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

/**
 * One Agent Hub tab: the session `params.sessionId` names, or — with no id —
 * the module's home tab, where a new session is drafted (documents.ts). Every
 * tab is its own instance, so what a view does for the whole module (commands,
 * ⌘N, an attach) it does only while it is the one on screen.
 */
export function AgentHubView({ params }: { params?: Record<string, unknown> } = {}) {
  const tabSessionId = typeof params?.sessionId === "string" ? params.sessionId : "";
  // Where the app is working, for a new draft to start in.
  const { folder: workFolder } = useModuleFolder("agenthub");
  const visible = useModuleVisibility();
  const sessions = useAgentSessionList();
  const sessionsLoaded = useAgentSessionsStore((state) => state.sessionsLoaded);
  const setActiveSessionId = useAgentSessionsStore((state) => state.setActiveSessionId);
  const pendingAgentHubCommand = useAgentSessionsStore((state) => state.pendingAgentHubCommand);
  const consumeAgentHubCommand = useAgentSessionsStore((state) => state.consumeAgentHubCommand);
  const markCompletedRead = useAgentSessionsStore((state) => state.markCompletedRead);
  const recoveryPending = useRef(false);
  const upsertSession = useAgentSessionsStore((state) => state.upsertSession);
  const setPrompt = useAgentSessionsStore((state) => state.setPrompt);
  const enqueuePrompt = useAgentSessionsStore((state) => state.enqueuePrompt);
  const removeQueuedPrompt = useAgentSessionsStore((state) => state.removeQueuedPrompt);
  const pendingQueue = useAgentSessionsStore((state) => state.pendingQueue);
  const dequeuePendingForSession = useAgentSessionsStore((state) => state.dequeuePendingForSession);
  const elicitationQueue = useAgentSessionsStore((state) => state.elicitationQueue);
  const dequeueElicitationForSession = useAgentSessionsStore((state) => state.dequeueElicitationForSession);

  const [vendors, setVendors] = useState<string[]>(DEFAULT_VENDORS);
  // A session being started. Its folder and parent are known when it begins;
  // its agent is picked in the composer; the conversation is created on the
  // first send. A vendor may read its catalog in a short-lived process before
  // that, without creating a conversation. This replaced a dialog that asked
  // for the agent up front, before anyone had said what they wanted from it.
  // Images pasted into the draft belong to it: they go when it is sent or left.
  const [draft, setDraft] = useState<{
    cwd: string;
    workspaceId: string;
    parent: AgentSession | null;
    images: AgentImageAttachment[];
    // Files picked with "@" before there is a session to attach them to; they
    // go with the first message.
    attachments: string[];
    // The rest of a workspace's services, when "grant every service" was
    // picked instead of one folder — see chooseWorkspaceForDraft.
    extraReadWritePaths: string[];
  } | null>(null);
  const [draftAgent, setDraftAgent] = useState<string>(CLAUDE_VENDOR);
  const [draftPrompt, setDraftPrompt] = useState("");
  const [draftSelections, setDraftSelections] = useState<Record<string, Record<string, string>>>({});
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
  const draftCatalog = useQuery({
    queryKey: ["agenthub", "configCatalog", draftAgent, draftRuntime.data?.current?.version],
    queryFn: () => agentHubConfigCatalog(draftAgent),
    enabled: visible && draft !== null && draftVendor.configCatalog === true && Boolean(draftRuntime.data && !runtimeBlocked(draftRuntime.data)),
    staleTime: 60_000,
    retry: false,
  });
  const draftOptions = draftConfigOptions(draftCatalog.data ?? [], draftSelections[draftAgent] ?? {});
  useEffect(() => onAgentSignInCompleted(() => {
    void queryClient.invalidateQueries({ queryKey: ["agenthub", "configCatalog"] });
    void queryClient.invalidateQueries({ queryKey: ["agenthub", "sessionConfig"] });
  }), [queryClient]);
  // A vendor failure the user waved off. Keyed by id and revision: the agent
  // republishes a failure with a new revision when something about it changed,
  // and that is news again.
  const [dismissedFailure, setDismissedFailure] = useState("");
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

  // This tab's session, and only it: a tab whose session is gone says so
  // rather than quietly showing another one.
  const activeSession = useMemo(
    () => (tabSessionId ? sessions.find((s) => s.id === tabSessionId) ?? null : null),
    [sessions, tabSessionId],
  );
  // opencode's sign-in methods are listed by starting opencode, so they are only
  // asked for once opencode is what the person is working with (the draft's
  // agent, this tab's session, or a create waiting on a sign-in).
  const openCodeSignIn = useClaudeSignIn(
    retryAfterSignIn,
    OPENCODE_VENDOR,
    (draft !== null && draftAgent === OPENCODE_VENDOR) ||
      activeSession?.agentType === OPENCODE_VENDOR ||
      (authRetry?.kind === "create" && authRetry.params.vendor === OPENCODE_VENDOR),
  );
  /** The terminal sign-in controller of a vendor that signs in that way. */
  const terminalSignIn = (vendor: string | undefined) => (vendor === OPENCODE_VENDOR ? openCodeSignIn : claudeSignIn);
  const sessionConfig = useQuery({
    queryKey: ["agenthub", "sessionConfig", activeSession?.id],
    queryFn: async () => { await agentHubPrepareSession(activeSession!.id); return true; },
    enabled: visible && !draft && Boolean(activeSession && activeSession.status !== "closed" && activeSession.status !== "running" && activeSession.status !== "awaiting_permission"),
    staleTime: 60_000,
    retry: false,
  });

  // A cross-module `agent.attach` (docs/dev-kit/cross-module.mdx § Primitive
  // 2) leaves its refs here rather than reaching into this component
  // directly, since the caller — a manifest handler — has no reference to
  // whichever composer is actually mounted (this view's, or the companion's;
  // AgentCompanion.tsx has the matching effect). Applied to whichever session
  // is active when it lands, same as askAgent does for plain text.
  const pendingAgentAttach = useAgentSessionsStore((state) => state.pendingAgentAttach);
  useEffect(() => {
    if (!visible || !pendingAgentAttach || !activeSession) return;
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
  }, [visible, pendingAgentAttach, activeSession, setPrompt]);

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
      newSession();
    } else if (pendingAgentHubCommand === "cancel") {
      void handleCancel();
    } else {
      setFocusComposerRequest((request) => request + 1);
    }
  }, [consumeAgentHubCommand, pendingAgentHubCommand, visible]);

  useEffect(() => {
    if (visible && activeSession?.id) markCompletedRead(activeSession.id);
  }, [activeSession?.id, markCompletedRead, visible]);

  // The tab on screen is the session the Agent panel follows once you leave
  // Agent Hub, and the one notifications and the permission toast treat as seen.
  useEffect(() => {
    if (visible && tabSessionId) setActiveSessionId(tabSessionId);
  }, [visible, tabSessionId, setActiveSessionId]);

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
      void startNewDraft();
      return;
    }
    startDraft(draftRequest.cwd, draftRequest.parent ?? null, draftRequest.workspaceId);
    const paths = draftRequest.workspacePaths ?? [];
    if (draftRequest.workspaceId && paths.length > 1) chooseWorkspaceForDraft(draftRequest.workspaceId, paths);
  }, [visible, tabSessionId, draftRequest]);

  useEffect(() => {
    if (!visible) return;
    function handleKeyDown(event: KeyboardEvent) {
      const overlayOpen = Boolean(document.querySelector('[role="dialog"], [role="alertdialog"], [role="menu"]'));
      if (!isNewSessionShortcut(event, detectPlatform() === "mac", overlayOpen)) return;
      event.preventDefault();
      newSession();
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
  function startDraft(cwd: string, parent: AgentSession | null, workspaceId = "") {
    setDraft({ cwd, workspaceId: parent?.workspaceId || workspaceId, parent, images: [], attachments: [], extraReadWritePaths: [] });
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
  function chooseWorkspaceForDraft(workspaceId: string, paths: string[]) {
    if (paths.length === 0) return;
    const [cwd, ...rest] = paths;
    setDraft((current) => current && { ...current, cwd, workspaceId, extraReadWritePaths: rest });
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

  /** "+ New", ⌘N, the empty state: drafted here in the new-session tab, else in it. */
  function newSession() {
    if (tabSessionId) openNewAgentSession();
    else void startNewDraft();
  }

  /** "New project…" in the draft's picker: the native folder dialog. */
  async function chooseFolderForDraft() {
    // Opens where the draft already points, else the folder the app is working in.
    const start = draft?.cwd || workFolder || (await codeGraphProjectRoot().catch(() => ""));
    try {
      const picked = await agentHubChooseDirectory(start);
      // "" is a cancelled picker: the draft keeps the folder it had. Picking
      // one folder narrows back from "whole workspace" if that was chosen.
      if (picked) setDraft((current) => current && { ...current, cwd: picked, workspaceId: "", extraReadWritePaths: [] });
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
        workspaceId: params.workspaceId,
        parentSessionId: params.parentSessionId,
        // A scratch session gets nothing beyond its own scratch folder — the
        // "whole workspace" pick only applies to a real cwd.
        extraReadWritePaths: scratch ? [] : params.extraReadWritePaths,
        initialConfig: params.initialConfig,
      });
      upsertSession(session);
      if (!scratch) rememberProject(params.cwd);
      setActiveSessionId(session.id);
      setDraft(null);
      setDraftPrompt("");
      // The session gets its own tab and this draft's tab goes: the draft has
      // become that session.
      openAgentSession(session);
      closeNewSessionTab();
      if (params.prompt || params.attachments?.length || params.images?.length) {
        // The message was written before the session existed and is the reason
        // it was created. Sent from here, outside any view: the tab that wrote
        // it has just closed. A refusal puts the words back in the session's
        // composer, where the person will be looking.
        const text = params.prompt ?? "";
        void sendAgentTurn(
          session.id,
          queuedPrompt(text, params.attachments ?? [], params.images ?? []),
        ).catch((e) => {
          const agents = useAgentSessionsStore.getState();
          if (text && !agents.promptBySession[session.id]) agents.setPrompt(session.id, text);
          toast.error(errorMessage(e));
        });
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
    if (draft) {
      setDraft((current) => current && { ...current, attachments: current.attachments.filter((p) => p !== path) });
      return;
    }
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

  /**
   * What "@" finds for a query. A draft names its folders itself — the one it
   * will work in, then the workspace's others — since there is no session row
   * to read them from yet; a session's are read from its row. A draft with no
   * folder picked (a scratch session to be) has nothing to search.
   */
  function searchContext(query: string) {
    if (draft) {
      return draft.cwd
        ? agentHubListDraftContextFiles([draft.cwd, ...draft.extraReadWritePaths], query)
        : Promise.resolve([]);
    }
    return activeSession ? agentHubListContextFiles(activeSession.id, query) : Promise.resolve([]);
  }

  function handleSelectContext(path: string) {
    // The draft first: activeSession falls back to the first session even
    // while a draft is on screen, and the file belongs to the draft's message.
    if (draft) {
      setDraft((current) =>
        current && !current.attachments.includes(path) ? { ...current, attachments: [...current.attachments, path] } : current,
      );
      return;
    }
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
    // Resolved in the session's workspace, so a path opens in the folder that
    // holds it whichever workspace the title bar shows. Beside: the session
    // stays on screen next to the file it talks about.
    void openFileRef(line !== undefined ? { path, line } : { path }, refContextOf(activeSession), { placement: "beside" });
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
    if (draft) {
      setDraftSelections((previous) => ({
        ...previous,
        [draftAgent]: initialModelConfig(draftConfigOptions(
          draftCatalog.data ?? [], { ...previous[draftAgent], [optionId]: value },
        )),
      }));
      return;
    }
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
      // The words were cleared above so the field is ready for the next
      // message; a turn that never started must not take them with it. Put
      // back only into an empty field — the person may already be typing.
      if (!retry && text && !useAgentSessionsStore.getState().promptBySession[session.id]) {
        setPrompt(session.id, text);
      }
      // A refused prompt (an attachment DevKit will not send) leaves the
      // session idle, so there is no error card to say so: without this the
      // send looked like it did nothing.
      if (toAppError(e).code === "validation_failed") toast.error(errorMessage(e));
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

  function retryTurn(): TurnInput | undefined {
    return retryTurnFor(activeSession?.id ?? "", lastTurn.current, transcriptModel.items);
  }

  // The vendor's own hints for a failure, each mapped to the thing this app
  // already does for it.
  async function handleResumeSession() {
    if (!activeSession || recoveryPending.current) return;
    recoveryPending.current = true;
    try {
      await agentHubResumeSession(activeSession.id);
      upsertSession(await agentHubGetStatus(activeSession.id));
      setErrorCause(undefined);
      await queryClient.invalidateQueries({ queryKey: ["agenthub", "sessionConfig", activeSession.id] });
    } catch (e) { reportError(e); }
    finally { recoveryPending.current = false; }
  }

  function handleFailureAction(action: string) {
    if (action === "resume") { void handleResumeSession(); return; }
    if (action === "retry") {
      const retry = retryTurn();
      if (retry) void handleSend(retry);
      return;
    }
    if (action === "new_session") {
      newSession();
      return;
    }
    if (action === "login") {
      const terminal = terminalSignIn(activeSession?.agentType);
      const methods = terminal.methods.data ?? [];
      // One way in: take it. Several: the error card's panel names them, and
      // picking for the user would be guessing. An agent that signs in
      // inside DevKit shows its panel, where the browser is the first button.
      if (vendorInfo(activeSession?.agentType).signIn === "terminal" && methods.length === 1) {
        terminal.start(methods[0]);
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

  /** The sign-in a vendor offers, wired to retry what was waiting on it. */
  function signInFor(vendor: string) {
    switch (vendorInfo(vendor).signIn) {
      case "terminal":
        return <ClaudeSignIn signIn={terminalSignIn(vendor)} vendor={vendor} />;
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
  const activeFailure = transcriptModel.failure ?? rateLimitFailure(transcriptModel.usage);

  // A tab restored for a session the list does not have yet is still loading;
  // once the list is in, the session is gone (deleted elsewhere, or with its
  // parent) and the tab says so instead of showing some other session.
  if (tabSessionId && !activeSession) {
    return (
      <div data-testid="agenthub-view" className="relative flex size-full min-h-0 flex-1 overflow-hidden bg-surface-base">
        {sessionsLoaded ? (
          <EmptyState
            data-testid="agenthub-session-missing"
            className="flex-1"
            title="Session not available"
            body="It was deleted, or belongs to a list this window could not load."
            action={{ label: "Close tab", onClick: () => closeAgentSessionTabs([tabSessionId], { remember: false }) }}
            secondaryAction={{ label: "New session", onClick: () => openNewAgentSession() }}
          />
        ) : (
          <EmptyState data-testid="agenthub-session-loading" className="flex-1" busy title="Loading session…" />
        )}
      </div>
    );
  }

  return (
    <div
      data-testid="agenthub-view"
      className="relative flex size-full min-h-0 flex-1 overflow-hidden bg-surface-base"
    >
        <div className="flex size-full min-h-0 flex-1 overflow-hidden">
          {/* The swarm tree is NOT drawn here — it is the module's rail
              (AgentHubSidebar, ModuleManifest.sidebar), in the shell's
              sidebar column. Drawing it here as well gave two vertical rails
              side by side whenever the shell sidebar was open. */}

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
            configOptions={draft ? draftOptions : transcriptModel.configOptions}
            configLoading={draft ? draftCatalog.isFetching : sessionConfig.isFetching}
            configError={(draft ? draftCatalog.error : sessionConfig.error) ? errorMessage(draft ? draftCatalog.error : sessionConfig.error) : undefined}
            onRefreshConfig={draft ? (draftVendor.configCatalog ? () => { void draftCatalog.refetch(); } : undefined) : activeSession ? () => { void sessionConfig.refetch(); } : undefined}
            mode={transcriptModel.mode}
            availableModes={transcriptModel.availableModes}
            onSetMode={(mode) => void handleSetMode(mode)}
            onSetConfigOption={handleSetConfigOption}
            focusComposerRequest={focusComposerRequest}
            hasMore={transcriptModel.hasMore}
            loadingOlder={transcriptModel.loadingOlder}
            onLoadOlder={transcriptModel.loadOlder}
            draft={
              draft
                ? {
                    projectPath: draft.cwd,
                    workspaceId: draft.workspaceId,
                    parentName: draft.parent?.title || undefined,
                    workspaceSelected: draft.extraReadWritePaths.length > 0,
                  }
                : null
            }
            projects={draftProjects}
            onProjectChange={(path) =>
              setDraft((current) => current && { ...current, cwd: path, workspaceId: "", extraReadWritePaths: [] })
            }
            onChooseProject={() => void chooseFolderForDraft()}
            workspace={
              currentWorkspace && workspaceServices.length > 0
                ? { id: currentWorkspace.id, name: currentWorkspace.name, paths: workspaceServices.map((s) => s.path) }
                : null
            }
            workspaceChoices={sidebarWorkspaces}
            onChooseWorkspace={chooseWorkspaceForDraft}
            onChooseWorkspaceContext={(workspaceId) => setDraft((current) => current && { ...current, workspaceId, extraReadWritePaths: [] })}
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
              if (!draftPrompt.trim() && draft.images.length === 0 && draft.attachments.length === 0) return;
              void handleCreateSession({
                vendor: draftAgent,
                cwd: draft.cwd,
                workspaceId: draft.workspaceId,
                parentSessionId: draft.parent?.id,
                prompt: draftPrompt.trim(),
                images: draft.images,
                attachments: draft.attachments,
                extraReadWritePaths: draft.extraReadWritePaths,
                initialConfig: initialModelConfig(draftOptions),
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
            connectors={transcriptModel.connectors}
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
            // Reopen the same conversation; replaying a turn is a separate action.
            onRetrySession={() => void handleResumeSession()}
            onNewSession={newSession}
            emptyExtra={<GettingStarted onNewSession={newSession} />}
            onOpenSettings={() => requestSettings("agents")}
            errorDetail={
              errorCause === "authRequired" && activeSession ? signInFor(activeSession.agentType) : undefined
            }
            attachments={draft ? draft.attachments : activeSession ? attachments[activeSession.id] ?? [] : []}
            onSearchContext={searchContext}
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

    </div>
  );
}
