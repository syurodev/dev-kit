import { useQueryClient } from "@tanstack/react-query";
import {
  agentHubCreateSession,
  agentHubNewScratchDirectory,
} from "../../../bridge/agenthub";
import { agentRuntimeQueryKey } from "../../../bridge/agentRuntime";
import { useAgentPinsStore } from "../../../stores/agentPins";
import { useAgentSessionsStore } from "../../../stores/agentSessions";
import { useComposerImagesStore } from "@/stores/composerImages";
import { queuedPrompt, sendAgentTurn } from "../../../workbench/agentPromptQueue";
import { errorMessage } from "../../../bridge/errors";
import { sandboxUnavailableMessage } from "../../../bridge/agenthub";
import { toast } from "sonner";
import { closeNewSessionTab, openAgentSession } from "../documents";
import { createFailure, type CreateParams } from "./sessionModel";
import type { AgentDraft } from "./useAgentDraft";
import type { AgentSessionStatus } from "./useAgentSessionStatus";

/**
 * Creating a session from the draft, and sending the message that is the
 * reason for it. Lives outside the tab that wrote it because that tab closes
 * the moment the session exists: the turn is sent from here, and a refusal
 * puts the words back in the new session's composer.
 */
export function useAgentSessionCreate({ status, draft }: { status: AgentSessionStatus; draft: AgentDraft }) {
  const queryClient = useQueryClient();

  async function handleCreateSession(initial: CreateParams) {
    let params = initial;
    status.setBusy(true);
    status.resetAll();
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
      const agents = useAgentSessionsStore.getState();
      agents.upsertSession(session);
      if (!scratch) useAgentPinsStore.getState().rememberProject(params.cwd);
      agents.setActiveSessionId(session.id);
      draft.clearDraft();
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
          queuedPrompt(text, params.attachments ?? [], params.images ?? [], [], params.draft),
        )
          .then(() => useComposerImagesStore.getState().forget((params.images ?? []).map((image) => image.id)))
          .catch((e) => {
            const store = useAgentSessionsStore.getState();
            const restore = params.draft || text;
            if (restore && !store.promptBySession[session.id]) store.setPrompt(session.id, restore);
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
        status.setAuthRetry({ kind: "create", params });
        return;
      }
      status.reportError(e);
      if (failure.sandboxHint) {
        status.setSandboxHint(sandboxUnavailableMessage());
      }
    } finally {
      status.setBusy(false);
    }
  }

  return { handleCreateSession };
}

export type AgentSessionCreate = ReturnType<typeof useAgentSessionCreate>;
