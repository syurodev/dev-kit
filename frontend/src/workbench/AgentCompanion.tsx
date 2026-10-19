import { useEffect, useRef, useState, type ReactElement } from "react";
import { frozenWhileHidden } from "@/lib/frozenWhileHidden";
import {
  agentHubCancel,
  agentHubGetStatus,
  agentHubChooseFiles,
  agentHubListContextFiles,
} from "@/bridge/agenthub";
import { errorMessage } from "@/bridge/errors";
import { AgentRail } from "@/modules/agenthub/surfaces";
import { providerRetryFailure } from "@/modules/agenthub/providerRetry";
import { rateLimitFailure } from "@/modules/agenthub/surfaces/AgentContextMeter";
import { failureKey } from "@/modules/agenthub/AgentHubView";
import { useAgentTranscript } from "@/modules/agenthub/transcript";
import { useResolvePermission } from "@/modules/agenthub/permissions";
import { elicitationForSession, useRespondElicitation } from "@/modules/agenthub/elicitations";
import { setSessionConfigOption, setSessionMode } from "@/modules/agenthub/transcriptControls";
import { useAgentSessionList, useAgentSessionsStore } from "@/stores/agentSessions";
import { EMPTY_ARRAY } from "@/lib/stableEmpty";
import { appendChips, refChip, turnFromDraft } from "@/modules/agenthub/composerDraft";
import { useComposerImagesStore } from "@/stores/composerImages";
import type { QueuedAgentPrompt } from "@/stores/agentSessions";
import { queuedPrompt, resumeQueuedPrompts, sendAgentTurn } from "./agentPromptQueue";
import { agentPaneSessionId, useAgentPaneSessionIds } from "./AgentPaneTabs";
import { useFollowToasts, useFollowing } from "@/modules/agenthub/follow";
import { openFileRef, refContextOf } from "@/modules/agenthub/transcript/openReference";

/**
 * The running conversation, in the shell's companion slot.
 *
 * Everything it renders comes from the store, so it is a view onto the same
 * conversation the Agent Hub module shows — not a second one. Switching modules
 * does not disturb it, because the shell owns this slot and the stream that
 * feeds it (see useAgentHubStream).
 *
 * Session creation stays in the Agent Hub module: that is a setup step with
 * vendor and working directory to choose, not something to do mid-conversation.
 */
export function AgentCompanion({ visible = true }: { visible?: boolean }) {
  const sessions = useAgentSessionList();
  const activeSessionId = useAgentSessionsStore((s) => s.activeSessionId);
  // The panel's tabs are Agent Hub's (AgentPaneTabs, in the pane header).
  const tabIds = useAgentPaneSessionIds();
  const upsertSession = useAgentSessionsStore((s) => s.upsertSession);
  const setPrompt = useAgentSessionsStore((s) => s.setPrompt);
  const pending = useAgentSessionsStore((s) => s.pending);
  const { busy: permBusy, resolve: resolvePermission } = useResolvePermission(pending);
  const enqueuePrompt = useAgentSessionsStore((s) => s.enqueuePrompt);
  const removeQueuedPrompt = useAgentSessionsStore((s) => s.removeQueuedPrompt);

  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  // A vendor failure (or a rejected rate limit, read the same way) the user
  // waved off. Keyed by id and revision, same as AgentHubView: a rate-limit
  // failure's revision is its resetsAt, so dismissing it holds until a new
  // window makes it news again.
  const [dismissedFailure, setDismissedFailure] = useState("");
  const [contextFiles, setContextFiles] = useState<Awaited<ReturnType<typeof agentHubListContextFiles>>>([]);
  const pendingAgentAttach = useAgentSessionsStore((s) => s.pendingAgentAttach);
  const consumeAgentAttach = useAgentSessionsStore((s) => s.consumeAgentAttach);

  const shownId = agentPaneSessionId(tabIds, activeSessionId);
  const session = sessions.find((s) => s.id === shownId) ?? null;
  const elicitationQueue = useAgentSessionsStore((s) => s.elicitationQueue);
  const question = elicitationForSession(elicitationQueue, session?.id);
  const {
    busy: questionBusy,
    error: questionError,
    respond: respondQuestion,
  } = useRespondElicitation(question);
  const prompt = useAgentSessionsStore((s) => s.promptBySession[session?.id ?? ""] ?? "");
  // EMPTY_ARRAY, not []: a fresh [] is a new snapshot on every read (see AgentHubView).
  const queuedPrompts = useAgentSessionsStore((s) => s.queuedPromptsBySession[session?.id ?? ""] ?? EMPTY_ARRAY);
  const queuePaused = useAgentSessionsStore((s) => Boolean(s.queuePausedBySession[session?.id ?? ""]));
  const queuePausedReason = useAgentSessionsStore((s) => s.queuePauseReasonBySession[session?.id ?? ""] ?? "");

  // Live transcript stream for the active session in rail
  const transcriptModel = useAgentTranscript(session?.id ?? "", { enabled: visible });
  // The vendor's own failure wins when there is one; a rejected rate limit is
  // read the same way, since Claude never reports that through the vendor's
  // failure channel (see AgentContextMeter's rateLimitFailure).
  const activeFailure =
    transcriptModel.failure ?? providerRetryFailure(transcriptModel.retry) ?? rateLimitFailure(transcriptModel.usage);
  // Following this session from outside Agent Hub: each change it finishes is
  // a toast with Open — there is no pane here to fill (follow.tsx).
  const [following] = useFollowing(session?.id);
  useFollowToasts(session?.id ?? "", transcriptModel.items, visible && following);

  useEffect(() => {
    if (!visible || !session) return;
    let mounted = true;
    agentHubListContextFiles(session.id)
      .then((files) => mounted && setContextFiles(files))
      .catch(() => mounted && setContextFiles([]));
    return () => { mounted = false; };
  }, [session?.id, visible]);

  // A cross-module `agent.attach` (docs/dev-kit/cross-module.mdx § Primitive
  // 2) lands here the same way it lands in AgentHubView's matching effect —
  // whichever of the two is actually on screen picks it up.
  useEffect(() => {
    if (!visible || !pendingAgentAttach || !session) return;
    const { refs: attachedRefs, prompt: attachedPrompt } = pendingAgentAttach;
    // The refs become chips at the end of the draft, then the prompt it came with.
    let next = appendChips(useAgentSessionsStore.getState().promptBySession[session.id] ?? "", attachedRefs.map(refChip));
    if (attachedPrompt) next = next ? `${next}\n\n${attachedPrompt}` : attachedPrompt;
    if (next) setPrompt(session.id, next);
    consumeAgentAttach();
  }, [visible, pendingAgentAttach, session, setPrompt, consumeAgentAttach]);


  async function handleSend(inputText?: string) {
    if (!session) return;
    // What the turn carries is read off the draft: its words and each chip in it.
    const sentDraft = inputText === undefined ? prompt : "";
    const drafted = inputText === undefined
      ? turnFromDraft(prompt, useComposerImagesStore.getState().byId)
      : { text: inputText, attachments: [], images: [], refs: [] };
    const { text, attachments, images, refs } = drafted;
    if (!text && attachments.length === 0 && images.length === 0 && refs.length === 0) return;
    const turn = queuedPrompt(text, attachments, images, refs, sentDraft);
    setPrompt(session.id, "");
    if (busy || session.status === "running" || session.status === "awaiting_permission") {
      enqueuePrompt(session.id, turn);
      return;
    }
    setBusy(true);
    setError("");
    try {
      await sendAgentTurn(session.id, turn);
      // The pictures' bytes are kept only while the draft may yet be sent.
      useComposerImagesStore.getState().forget(images.map((image) => image.id));
    } catch (e) {
      setError(errorMessage(e));
      // A turn that never started takes nothing with it: the draft, chips and
      // all, goes back into an empty field.
      if (sentDraft && !useAgentSessionsStore.getState().promptBySession[session.id]) setPrompt(session.id, sentDraft);
    } finally {
      setBusy(false);
    }
  }

  function editQueuedPrompt(queued: QueuedAgentPrompt) {
    if (!session) return;
    useComposerImagesStore.getState().addAll(queued.images);
    setPrompt(session.id, queued.draft ?? queued.text);
    removeQueuedPrompt(session.id, queued.id);
  }

  function selectCommand(name: string) {
    if (!session) return;
    const command = transcriptModel.commands.find((item) => item.name.replace(/^\//, "") === name.replace(/^\//, ""));
    const commandName = name.startsWith("/") ? name : `/${name}`;
    if (command?.input) {
      const current = useAgentSessionsStore.getState().promptBySession[session.id] ?? "";
      setPrompt(session.id, `${current}${current && !/\s$/.test(current) ? " " : ""}${commandName} `);
    } else {
      void handleSend(commandName);
    }
  }

  function openFile(path: string, line?: number) {
    if (!session) return;
    // The companion sits over whatever module is on screen; the file opens
    // where you are working. Resolved in the session's workspace, so it opens
    // in the folder that holds it.
    void openFileRef(line !== undefined ? { path, line } : { path }, refContextOf(session), { placement: "replace" });
  }

  async function setMode(nextMode: string) {
    if (!session) return;
    try { await setSessionMode(session.id, nextMode); }
    catch (e) { setError(errorMessage(e)); }
  }

  async function setConfigOption(optionId: string, value: string) {
    if (!session) return;
    try { await setSessionConfigOption(session.id, optionId, value); }
    catch (e) { setError(errorMessage(e)); }
  }

  async function handleCancel() {
    if (!session) return;
    try {
      await agentHubCancel(session.id);
      useAgentSessionsStore.getState().dequeuePendingForSession(session.id);
      useAgentSessionsStore.getState().dequeueElicitationForSession(session.id);
      upsertSession(await agentHubGetStatus(session.id));
    } catch (e) {
      setError(errorMessage(e));
    }
  }

  // Hidden (its pane closed, or inside Agent Hub) it stays mounted but frozen:
  // the store keeps changing under it — the same session's prompt is typed in
  // Agent Hub — and the rail must not re-render, nor its composer reset its
  // editor, for any of it. Shown again, it renders once with what is current.
  const frozen = useRef<ReactElement | null>(null);
  return frozenWhileHidden(frozen, visible, (
    <AgentRail
      activeSession={session}
      items={transcriptModel.items}
      hasMore={transcriptModel.hasMore}
      loadingOlder={transcriptModel.loadingOlder}
      onLoadOlder={transcriptModel.loadOlder}
      usage={transcriptModel.usage}
      commands={transcriptModel.commands}
      contextFiles={contextFiles}
      onSelectCommand={selectCommand}
      onSearchContext={(query) => (session ? agentHubListContextFiles(session.id, query) : Promise.resolve([]))}
      queuedPrompts={queuedPrompts}
      queuePaused={queuePaused}
      queuePausedReason={queuePausedReason}
      onResumeQueued={() => session && resumeQueuedPrompts(session.id)}
      onCancelQueued={(id) => session && removeQueuedPrompt(session.id, id)}
      onEditQueued={editQueuedPrompt}
      onBrowseContext={async () => {
        if (!session) return [];
        try {
          return await agentHubChooseFiles(session.projectPath);
        } catch (e) {
          setError(errorMessage(e));
          return [];
        }
      }}
      configOptions={transcriptModel.configOptions}
      mode={transcriptModel.mode}
      availableModes={transcriptModel.availableModes}
      onSetMode={(mode) => void setMode(mode)}
      onSetConfigOption={(optionId, value) => void setConfigOption(optionId, value)}
      onOpenFile={(path, line) => void openFile(path, line)}
      promptValue={prompt}
      onPromptChange={(val) => session && setPrompt(session.id, val)}
      onSend={() => void handleSend()}
      onCancel={() => void handleCancel()}
      streaming={busy || session?.status === "running"}
      pendingPermission={pending}
      onResolvePermission={resolvePermission}
      permissionBusy={permBusy}
      pendingElicitation={question}
      onRespondElicitation={respondQuestion}
      elicitationBusy={questionBusy}
      elicitationError={questionError}
      failure={activeFailure && failureKey(activeFailure) !== dismissedFailure ? activeFailure : null}
      onDismissFailure={() => setDismissedFailure(activeFailure ? failureKey(activeFailure) : "")}
      errorMessage={error}
      sendDisabled={false}
    />
  ));
}
