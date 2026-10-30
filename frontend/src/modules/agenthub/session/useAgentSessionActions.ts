import { useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import {
  agentHubChooseFiles,
  agentHubGetStatus,
  agentHubListContextFiles,
  agentHubListDraftContextFiles,
  agentHubResumeSession,
  agentHubSetGoal,
  agentHubSteer,
  type AgentSession,
} from "../../../bridge/agenthub";
import { isAgentAuthRequiredError } from "../../../bridge/agentRuntime";
import { errorMessage, toAppError } from "../../../bridge/errors";
import { useAgentSessionsStore, type QueuedAgentPrompt } from "../../../stores/agentSessions";
import { useComposerImagesStore } from "@/stores/composerImages";
import { queuedPrompt, sendAgentTurn } from "../../../workbench/agentPromptQueue";
import { toast } from "sonner";
import { turnFromDraft } from "../composerDraft";
import { commandName, promptWithCommand, promptWithoutCommand } from "../slashCommands";
import { setSessionConfigOption, setSessionMode } from "../transcriptControls";
import { openFileRef, refContextOf } from "../transcript/openReference";
import { dismissSessionFailure } from "../sidebar/dismissFailure";
import { cancelSessionTurn } from "./cancelTurn";
import { failureKey, retryTurnFor, shouldSteer, type TurnInput } from "./sessionModel";
import type { AgentDraft } from "./useAgentDraft";
import type { AgentSessionReads } from "./useAgentSessionReads";
import type { AgentSessionStatus } from "./useAgentSessionStatus";

/**
 * What a tab does to the session on screen: send, steer, queue, retry, resume,
 * cancel, and the small edits around them (a queued prompt, a mode, a goal, a
 * command, a file). It reads everything through useAgentSessionReads and keeps
 * only what is genuinely local to an action: the last turn sent (Retry resends
 * it) and what the person dismissed.
 */
export function useAgentSessionActions({
  reads,
  draft,
  status,
}: {
  reads: AgentSessionReads;
  draft: AgentDraft;
  status: AgentSessionStatus;
}) {
  const queryClient = useQueryClient();
  const { activeSession, activePrompt, sessions, transcriptModel } = reads;
  // The last turn sent, so Retry resends it: the composer is cleared on send.
  const lastTurn = useRef<TurnInput | null>(null);
  const recoveryPending = useRef(false);
  // A vendor failure the user waved off. Keyed by id and revision: the agent
  // republishes a failure with a new revision when something about it changed,
  // and that is news again.
  const [dismissedFailure, setDismissedFailure] = useState("");

  function queuePrompt(session: AgentSession, turn: ReturnType<typeof turnFromDraft>, draftText: string) {
    const store = useAgentSessionsStore.getState();
    store.enqueuePrompt(session.id, queuedPrompt(turn.text, turn.attachments, turn.images, turn.refs, draftText));
    store.setPrompt(session.id, "");
  }

  function editQueuedPrompt(queued: QueuedAgentPrompt) {
    if (!activeSession) return;
    const store = useAgentSessionsStore.getState();
    // The draft it was sent from, so its chips come back where they stood; the
    // images' bytes go back where the chips look for them.
    useComposerImagesStore.getState().addAll(queued.images);
    store.setPrompt(activeSession.id, queued.draft ?? queued.text);
    store.removeQueuedPrompt(activeSession.id, queued.id);
  }

  /**
   * What "@" finds for a query. A draft names its folders itself — the one it
   * will work in, then the workspace's others — since there is no session row
   * to read them from yet; a session's are read from its row. A draft with no
   * folder picked (a scratch session to be) has nothing to search.
   */
  function searchContext(query: string) {
    if (draft.draft) {
      return draft.draft.cwd
        ? agentHubListDraftContextFiles([draft.draft.cwd, ...draft.draft.extraReadWritePaths], query)
        : Promise.resolve([]);
    }
    return activeSession ? agentHubListContextFiles(activeSession.id, query) : Promise.resolve([]);
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
    const store = useAgentSessionsStore.getState();
    const current = store.promptBySession[activeSession.id] ?? "";
    if (command?.input) {
      // Left in the field for the argument to be typed after it. Replaces a
      // half-typed name rather than appending to it: picking /review after
      // typing "/rev" has to leave "/review ", not "/rev /review ".
      store.setPrompt(activeSession.id, promptWithCommand(current, name));
      return;
    }
    // No argument: the command is the whole turn. handleSend with an explicit
    // turn deliberately does not clear the field — a retry must not — so the
    // half-typed name that opened the list is taken out here, and only it: a
    // message already in the box is not ours to throw away.
    store.setPrompt(activeSession.id, promptWithoutCommand(current));
    void handleSend({ sessionId: activeSession.id, text: commandName(name), attachments: [], images: [], refs: [] });
  }

  async function handleSetMode(nextMode: string) {
    if (!activeSession) return;
    try {
      await setSessionMode(activeSession.id, nextMode);
    } catch (e) {
      status.reportError(e);
    }
  }

  async function handleSetConfigOption(optionId: string, value: string) {
    if (draft.draft) {
      draft.setDraftConfigOption(optionId, value);
      return;
    }
    if (!activeSession) return;
    try {
      await setSessionConfigOption(activeSession.id, optionId, value);
    } catch (e) {
      status.reportError(e);
    }
  }

  /** Sends the composer's prompt, or `retry` — a turn that already failed once. */
  async function handleSend(retry?: TurnInput, known?: AgentSession) {
    const session = known ?? (retry ? sessions.find((s) => s.id === retry.sessionId) : activeSession);
    if (!session) return;
    // What the turn carries is read off the draft: its words, and each chip in
    // it. A retry carries what its first attempt did.
    const drafted = retry ? null : turnFromDraft(activePrompt, useComposerImagesStore.getState().byId);
    const attached = retry ? retry.attachments : drafted!.attachments;
    const attachedImages = retry ? retry.images : drafted!.images;
    const attachedRefs = retry ? retry.refs : drafted!.refs;
    const text = retry ? retry.text : drafted!.text;
    const sentDraft = retry ? "" : activePrompt;
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
        busy: status.busy,
        retrying: Boolean(known),
      })
    ) {
      if (await steerInto(session, text)) {
        useAgentSessionsStore.getState().setPrompt(session.id, "");
        return;
      }
    }
    if (!known && (status.busy || session.status === "running" || session.status === "awaiting_permission")) {
      queuePrompt(session, { text, attachments: attached, images: attachedImages, refs: attachedRefs }, sentDraft);
      return;
    }
    if (status.busy) return;
    const store = useAgentSessionsStore.getState();
    if (!retry) store.setPrompt(session.id, "");
    const turn = { sessionId: session.id, text, attachments: attached, images: attachedImages, refs: attachedRefs };
    lastTurn.current = turn;
    status.setBusy(true);
    status.setError("");
    status.setAuthRetry(null);
    try {
      // Resolves without a result when the backend was busy and the prompt was
      // queued instead; its files went into the queue with it.
      await sendAgentTurn(session.id, queuedPrompt(text, attached, attachedImages, attachedRefs, sentDraft));
      // The pictures' bytes are only kept for a draft that may yet be sent:
      // once the turn was accepted they have done their job.
      useComposerImagesStore.getState().forget(attachedImages.map((image) => image.id));
    } catch (e) {
      status.reportError(e);
      // The draft was cleared above so the field is ready for the next
      // message; a turn that never started must not take it — files, images and
      // all, since they are chips in it — with it. Put back only into an empty
      // field: the person may already be typing.
      if (sentDraft && !useAgentSessionsStore.getState().promptBySession[session.id]) {
        useAgentSessionsStore.getState().setPrompt(session.id, sentDraft);
      }
      // A refused prompt (an attachment DevKit will not send) leaves the
      // session idle, so there is no error card to say so: without this the
      // send looked like it did nothing.
      if (toAppError(e).code === "validation_failed") toast.error(errorMessage(e));
      if (isAgentAuthRequiredError(e)) status.setAuthRetry({ kind: "turn", ...turn });
    } finally {
      status.setBusy(false);
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
      status.reportError(e);
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
      useAgentSessionsStore.getState().upsertSession(await agentHubGetStatus(activeSession.id));
      status.setErrorCause(undefined);
      // The notice that offered Resume ("Previous turn was interrupted") stays
      // in the transcript until the next prompt, so without this the button
      // seemed to do nothing. Said once, where the button was.
      if (reads.activeFailure) setDismissedFailure(failureKey(reads.activeFailure));
      toast.success("Session resumed. Send your next instruction to continue.");
      await queryClient.invalidateQueries({ queryKey: ["agenthub", "sessionConfig", activeSession.id] });
    } catch (e) { status.reportError(e); }
    finally { recoveryPending.current = false; }
  }

  async function handleCancel() {
    if (!activeSession) return;
    await cancelSessionTurn(activeSession.id, status.reportError);
  }

  /** The files the person picked, for the composer to put at the caret; none when cancelled or when the dialog failed. */
  async function handleBrowseContext(): Promise<string[]> {
    try {
      return await agentHubChooseFiles(draft.draft ? draft.draft.cwd : (activeSession?.projectPath ?? ""));
    } catch (e) {
      status.reportError(e);
      return [];
    }
  }

  /** "Dismiss" on a failed session's banner: clears the failure from the rail too. */
  function dismissError() {
    if (reads.canDismissError && activeSession) dismissSessionFailure(activeSession);
  }

  return {
    dismissedFailure,
    dismissFailure: () => setDismissedFailure(reads.activeFailure ? failureKey(reads.activeFailure) : ""),
    dismissError,
    handleSend,
    handleCancel,
    handleResumeSession,
    handleSetMode,
    handleSetConfigOption,
    handleSetGoal,
    handleSelectCommand,
    handleOpenFile,
    handleBrowseContext,
    searchContext,
    editQueuedPrompt,
    retryTurn,
  };
}

export type AgentSessionActions = ReturnType<typeof useAgentSessionActions>;
