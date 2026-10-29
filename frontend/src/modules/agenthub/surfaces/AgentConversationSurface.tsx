import { vendorInfo } from "../vendors";
import { AgentRuntimeSetup } from "../AgentRuntimeSetup";
import { ClaudeSignIn } from "../ClaudeSignIn";
import { CodexSignIn } from "../CodexSignIn";
import { requestSettings } from "../../../workbench/settingsRequest";
import { AgentHubContent } from "./AgentHubContent";
import { GettingStarted } from "@/workbench/GettingStarted";
import { EmptyState } from "@/components/ui/empty-state";
import { closeAgentSessionTabs, openNewAgentSession } from "../documents";
import { initialModelConfig } from "../modelSettings";
import { turnFromDraft } from "../composerDraft";
import { failureKey } from "../session/sessionModel";
import { useComposerImagesStore } from "@/stores/composerImages";
import { useAgentSessionsStore } from "../../../stores/agentSessions";
import { resumeQueuedPrompts } from "../../../workbench/agentPromptQueue";
import { copyText } from "../../../bridge/clipboard";
import { errorMessage } from "../../../bridge/errors";
import { isWorking } from "../sessionActivity";
import type { AgentHubController } from "../session/useAgentHubController";

/**
 * The session on screen — or the draft that will become one — drawn with
 * everything the tab reads and can do. Every prop below is forwarded from the
 * controller; nothing here decides anything the controller does not.
 */
export function AgentConversationSurface({ hub }: { hub: AgentHubController }) {
  const { tabSessionId, status, draft, reads, create, actions } = hub;
  const { activeSession, transcriptModel, sessionConfig, draftProjects } = reads;

  // The sign-in a vendor offers, wired to retry what was waiting on it.
  function signInFor(vendor: string) {
    switch (vendorInfo(vendor).signIn) {
      case "terminal":
        return <ClaudeSignIn signIn={hub.terminalSignIn(vendor)} vendor={vendor} />;
      case "inApp":
        return <CodexSignIn signIn={hub.codexSignIn} />;
      default:
        return undefined;
    }
  }

  // What stands between a draft and its first message, in the draft's own view.
  const draftNotice = !draft.draft ? null : draft.draftBlocked ? (
    <AgentRuntimeSetup vendor={draft.draftAgent} onOpenSettings={() => requestSettings("agents")} />
  ) : status.authRetry?.kind === "create" ? (
    <div data-testid="agent-sign-in-panel" className="flex w-full flex-col gap-3 rounded-sm bg-(--state-soft) p-4">
      <p className="text-body-13-medium text-text-primary">{`${draft.draftVendor.name} isn't signed in`}</p>
      <p className="text-caption-11 text-text-tertiary">
        {draft.draftVendor.signIn === "terminal"
          ? "Sign in in the terminal that opens. The session starts and your message is sent as soon as the login finishes."
          : "Sign in below. The session starts and your message is sent as soon as the sign-in finishes."}
      </p>
      {signInFor(status.authRetry.params.vendor)}
    </div>
  ) : null;

  // A tab restored for a session the list does not have yet is still loading;
  // once the list is in, the session is gone (deleted elsewhere, or with its
  // parent) and the tab says so instead of showing some other session.
  if (tabSessionId && !activeSession) {
    return (
      <div data-testid="agenthub-view" className="relative flex size-full min-h-0 flex-1 overflow-hidden bg-surface-base">
        {reads.sessionsLoaded ? (
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
          contextFiles={reads.contextFiles[activeSession?.id ?? ""] ?? []}
          onSelectCommand={actions.handleSelectCommand}
          onOpenFile={(path, line) => void actions.handleOpenFile(path, line)}
          configOptions={draft.draft ? draft.draftOptions : transcriptModel.configOptions}
          configLoading={draft.draft ? draft.draftCatalog.isFetching : sessionConfig.isFetching}
          configError={(draft.draft ? draft.draftCatalog.error : sessionConfig.error) ? errorMessage(draft.draft ? draft.draftCatalog.error : sessionConfig.error) : undefined}
          onRefreshConfig={draft.draft ? (draft.draftVendor.configCatalog ? () => { void draft.draftCatalog.refetch(); } : undefined) : activeSession ? () => { void sessionConfig.refetch(); } : undefined}
          mode={transcriptModel.mode}
          availableModes={transcriptModel.availableModes}
          onSetMode={(mode) => void actions.handleSetMode(mode)}
          onSetConfigOption={actions.handleSetConfigOption}
          focusComposerRequest={hub.focusComposerRequest}
          hasMore={transcriptModel.hasMore}
          loadingOlder={transcriptModel.loadingOlder}
          onLoadOlder={transcriptModel.loadOlder}
          draft={
            draft.draft
              ? {
                  projectPath: draft.draft.cwd,
                  workspaceId: draft.draft.workspaceId,
                  parentName: draft.draft.parent?.title || undefined,
                  workspaceSelected: draft.draft.extraReadWritePaths.length > 0,
                }
              : null
          }
          projects={draftProjects}
          onProjectChange={draft.setDraftProject}
          onChooseProject={() => void draft.chooseFolderForDraft()}
          workspace={
            reads.currentWorkspace && reads.workspaceServices.length > 0
              ? { id: reads.currentWorkspace.id, name: reads.currentWorkspace.name, paths: reads.workspaceServices.map((s) => s.path) }
              : null
          }
          workspaceChoices={reads.sidebarWorkspaces}
          onChooseWorkspace={draft.chooseWorkspaceForDraft}
          onChooseWorkspaceContext={draft.chooseWorkspaceContext}
          agents={reads.vendors}
          agent={draft.draftAgent}
          onAgentChange={draft.setDraftAgent}
          draftNotice={draftNotice}
          sendDisabled={draft.draft ? status.busy || draft.draftBlocked : false}
          promptValue={draft.draft ? draft.draftPrompt : reads.activePrompt}
          onPromptChange={(val) => {
            if (draft.draft) draft.setDraftPrompt(val);
            else if (activeSession) useAgentSessionsStore.getState().setPrompt(activeSession.id, val);
          }}
          onSend={() => {
            if (!draft.draft) return void actions.handleSend();
            const first = turnFromDraft(draft.draftPrompt, useComposerImagesStore.getState().byId);
            if (!first.text && first.attachments.length === 0 && first.images.length === 0 && first.refs.length === 0) return;
            void create({
              vendor: draft.draftAgent,
              cwd: draft.draft.cwd,
              workspaceId: draft.draft.workspaceId,
              parentSessionId: draft.draft.parent?.id,
              prompt: first.text,
              draft: draft.draftPrompt,
              images: first.images,
              attachments: first.attachments,
              extraReadWritePaths: draft.draft.extraReadWritePaths,
              initialConfig: initialModelConfig(draft.draftOptions),
            });
          }}
          onCancel={() => void actions.handleCancel()}
          onCopy={(text) => void copyText(text)}
          failure={
            reads.activeFailure && failureKey(reads.activeFailure) !== actions.dismissedFailure ? reads.activeFailure : null
          }
          onFailureAction={hub.handleFailureAction}
          onDismissFailure={actions.dismissFailure}
          goal={transcriptModel.goal}
          connectors={transcriptModel.connectors}
          onSetGoal={(objective) => void actions.handleSetGoal(objective)}
          pendingPermission={reads.activePending}
          onResolvePermission={reads.resolvePermission}
          onApproveScopedPermission={reads.approveScopedPermission}
          permissionBusy={reads.permissionBusy}
          permissionError={reads.permissionError}
          pendingElicitation={reads.activeQuestion}
          onRespondElicitation={reads.respondElicitation}
          elicitationBusy={reads.elicitationBusy}
          elicitationError={reads.elicitationError}
          onCompact={() => {
            // Through the same path as a typed prompt, so a compact is a turn
            // like any other: it shows, it can fail, it can be retried. Not
            // through the draft, which may hold something half-written.
            if (activeSession) {
              void actions.handleSend({ sessionId: activeSession.id, text: "/compact", attachments: [], images: [], refs: [] });
            }
          }}
          streaming={status.busy || isWorking(activeSession)}
          errorMessage={status.errorReason || status.error || status.sandboxHint}
          errorCause={status.errorCause}
          onDismissError={reads.canDismissError && activeSession ? actions.dismissError : undefined}
          // Reopen the same conversation; replaying a turn is a separate action.
          onRetrySession={() => void actions.handleResumeSession()}
          onNewSession={draft.newSession}
          emptyExtra={<GettingStarted onNewSession={draft.newSession} />}
          onOpenSettings={() => requestSettings("agents")}
          errorDetail={
            status.errorCause === "authRequired" && activeSession ? signInFor(activeSession.agentType) : undefined
          }
          onSearchContext={actions.searchContext}
          queuedPrompts={reads.queuedPrompts}
          queuePaused={reads.queuePaused}
          queuePausedReason={reads.queuePausedReason}
          onResumeQueued={() => activeSession && resumeQueuedPrompts(activeSession.id)}
          onCancelQueued={(id) => activeSession && useAgentSessionsStore.getState().removeQueuedPrompt(activeSession.id, id)}
          onEditQueued={actions.editQueuedPrompt}
          onBrowseContext={actions.handleBrowseContext}
        />
      </div>
    </div>
  );
}
