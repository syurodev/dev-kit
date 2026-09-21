import { useEffect, useState } from "react";
import {
  agentHubCancel,
  agentHubGetStatus,
  agentHubChooseFiles,
  agentHubListContextFiles,
  type AgentImageAttachment,
} from "@/bridge/agenthub";
import { errorMessage } from "@/bridge/errors";
import { AgentRail } from "@/modules/agenthub/surfaces";
import { useAgentTranscript } from "@/modules/agenthub/transcript";
import { useResolvePermission } from "@/modules/agenthub/permissions";
import { elicitationForSession, useRespondElicitation } from "@/modules/agenthub/elicitations";
import { setSessionConfigOption, setSessionMode } from "@/modules/agenthub/transcriptControls";
import { useAgentSessionList, useAgentSessionsStore } from "@/stores/agentSessions";
import { dispatchIntent, type EntityRef } from "@/workbench/intents";
import { EMPTY_ARRAY } from "@/lib/stableEmpty";
import { imageAttachmentFromFile } from "@/modules/agenthub/imageAttachments";
import type { QueuedAgentPrompt } from "@/stores/agentSessions";
import { queuedPrompt, resumeQueuedPrompts, sendAgentTurn } from "./agentPromptQueue";

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
  const setActiveSessionId = useAgentSessionsStore((s) => s.setActiveSessionId);
  const upsertSession = useAgentSessionsStore((s) => s.upsertSession);
  const setPrompt = useAgentSessionsStore((s) => s.setPrompt);
  const pending = useAgentSessionsStore((s) => s.pending);
  const { busy: permBusy, resolve: resolvePermission } = useResolvePermission(pending);
  const enqueuePrompt = useAgentSessionsStore((s) => s.enqueuePrompt);
  const removeQueuedPrompt = useAgentSessionsStore((s) => s.removeQueuedPrompt);

  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [attachments, setAttachments] = useState<string[]>([]);
  const [images, setImages] = useState<AgentImageAttachment[]>([]);
  const [refs, setRefs] = useState<EntityRef[]>([]);
  const [imageError, setImageError] = useState("");
  const [contextFiles, setContextFiles] = useState<Awaited<ReturnType<typeof agentHubListContextFiles>>>([]);
  const pendingAgentAttach = useAgentSessionsStore((s) => s.pendingAgentAttach);
  const consumeAgentAttach = useAgentSessionsStore((s) => s.consumeAgentAttach);

  const session = sessions.find((s) => s.id === activeSessionId) ?? sessions[0] ?? null;
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
    setRefs((current) => [...current, ...attachedRefs]);
    if (attachedPrompt) {
      const draft = useAgentSessionsStore.getState().promptBySession[session.id] ?? "";
      setPrompt(session.id, draft ? `${draft}\n\n${attachedPrompt}` : attachedPrompt);
    }
    consumeAgentAttach();
  }, [visible, pendingAgentAttach, session, setPrompt, consumeAgentAttach]);


  async function handleSend(inputText?: string) {
    if (!session) return;
    const text = inputText ?? prompt.trim();
    if (!text && attachments.length === 0 && images.length === 0 && refs.length === 0) return;
    const turn = queuedPrompt(text, attachments, images, refs);
    setPrompt(session.id, "");
    if (busy || session.status === "running" || session.status === "awaiting_permission") {
      enqueuePrompt(session.id, turn);
      setAttachments([]);
      setImages([]);
      setRefs([]);
      return;
    }
    setBusy(true);
    setError("");
    try {
      await sendAgentTurn(session.id, turn);
      // Cleared only once the turn was accepted or queued: a failed send leaves
      // the files where the user put them.
      setAttachments([]);
      setImages([]);
      setRefs([]);
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  }

  async function addImage(file: File) {
    try {
      const image = await imageAttachmentFromFile(file);
      setImages((current) => [...current, image]);
      setImageError("");
    } catch (e) {
      setImageError(e instanceof Error ? e.message : "Could not attach image");
    }
  }

  function editQueuedPrompt(queued: QueuedAgentPrompt) {
    if (!session) return;
    setPrompt(session.id, queued.text);
    setAttachments(queued.attachments);
    setImages(queued.images);
    setRefs(queued.refs);
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

  function selectContext(path: string) {
    setAttachments((current) => current.includes(path) ? current : [...current, path]);
  }

  function openFile(path: string, line?: number) {
    if (!session) return;
    // The companion sits over whatever module is on screen; the file opens
    // where you are working, as Code Graph decides.
    void dispatchIntent("codegraph.reveal", {
      module: "codegraph",
      kind: "file",
      id: path,
      label: path.slice(path.lastIndexOf("/") + 1),
      project: session.projectPath,
      line,
    });
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

  return (
    <AgentRail
      sessions={sessions}
      activeSession={session}
      onSelectSession={(id) => setActiveSessionId(id)}
      items={transcriptModel.items}
      hasMore={transcriptModel.hasMore}
      loadingOlder={transcriptModel.loadingOlder}
      onLoadOlder={transcriptModel.loadOlder}
      usage={transcriptModel.usage}
      commands={transcriptModel.commands}
      contextFiles={contextFiles}
      onSelectCommand={selectCommand}
      onSelectContext={selectContext}
      attachedFiles={attachments}
      onRemoveAttachment={(path) => setAttachments((current) => current.filter((item) => item !== path))}
      imageAttachments={images}
      onRemoveImage={(id) => setImages((current) => current.filter((image) => image.id !== id))}
      refs={refs}
      onRemoveRef={(ref) =>
        setRefs((current) => current.filter((r) => !(r.module === ref.module && r.kind === ref.kind && r.id === ref.id)))
      }
      onPasteImage={(file) => void addImage(file)}
      onDropImage={(file) => void addImage(file)}
      imageError={imageError}
      queuedPrompts={queuedPrompts}
      queuePaused={queuePaused}
      queuePausedReason={queuePausedReason}
      onResumeQueued={() => session && resumeQueuedPrompts(session.id)}
      onCancelQueued={(id) => session && removeQueuedPrompt(session.id, id)}
      onEditQueued={editQueuedPrompt}
      onBrowseContext={() => {
        if (!session) return;
        void agentHubChooseFiles(session.projectPath).then((picked) => {
          setAttachments((current) => [...current, ...picked.filter((path) => !current.includes(path))]);
        }).catch((e) => setError(errorMessage(e)));
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
      errorMessage={error}
      sendDisabled={false}
    />
  );
}
