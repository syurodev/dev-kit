import * as React from "react";
import { UpdateKind } from "../../../../bindings/devkit/internal/agenthub/models";
import { cn } from "@/lib/utils";
import { Bot, Plus } from "lucide-react";
import { EmptyState } from "@/components/ui/empty-state";
import type { AgentSession, ElicitationResponse, PendingElicitation, PendingPermission } from "@/bridge/agenthub";
import type { AgentImageAttachment } from "@/bridge/agenthub";
import type { QueuedAgentPrompt } from "@/stores/agentSessions";
import type { EntityRef } from "@/workbench/intents";
import type {
  AvailableCommand,
  ConfigOption,
  SessionFailure,
  SessionGoal,
  SessionMode,
  TranscriptItem,
  Usage,
} from "../types";
import type { ContextFileItem } from "./AgentMenu";
import { TranscriptStream } from "./TranscriptStream";
import { loadCheckpointFileDiff, useSessionCheckpoints } from "../useSessionCheckpoints";
import { RewindDialog } from "./RewindDialog";
import { AgentComposer } from "./AgentComposer";
import { ProjectPicker } from "./ProjectPicker";
import { AgentSessionError, type SessionErrorCause } from "./AgentSessionError";
import { AgentSessionFailure } from "./AgentSessionFailure";
import { useAgentTray } from "./composer/agentTray";
import { vendorInfo } from "../vendors";
import { SessionSummaryButton, SessionSummaryCard } from "../summary/SessionSummary";

export type AgentHubContentState = "Conversation" | "Empty" | "Error";

export type AgentHubContentProps = {
  state?: AgentHubContentState;
  session?: AgentSession | null;
  items?: TranscriptItem[];
  usage?: Usage | null;
  commands?: AvailableCommand[];
  contextFiles?: ContextFileItem[];
  onSelectCommand?: (name: string) => void;
  onSelectContext?: (path: string) => void;
  /** What "@" finds for a query — the draft's folders, or the session's. */
  onSearchContext?: (query: string) => Promise<ContextFileItem[]>;
  configOptions?: ConfigOption[];
  mode?: string | null;
  availableModes?: SessionMode[];
  onSetMode?: (mode: string) => void;
  onSetConfigOption?: (optionId: string, value: string) => void | Promise<void>;
  configLoading?: boolean;
  configError?: string;
  onRefreshConfig?: () => void;
  focusComposerRequest?: number;
  /** Older transcript pages exist above the ones held (see useAgentTranscript). */
  hasMore?: boolean;
  loadingOlder?: boolean;
  onLoadOlder?: () => void;
  promptValue: string;
  onPromptChange: (value: string) => void;
  onSend: () => void;
  onCancel: () => void;
  streaming?: boolean;
  errorCause?: SessionErrorCause;
  errorMessage?: string;
  onRetrySession?: () => void;
  onNewSession?: () => void;
  /** Under the empty state's call to action — the first-run checklist. */
  emptyExtra?: React.ReactNode;
  /** Opens Settings → Agents, for errors whose fix is an install. */
  onOpenSettings?: () => void;
  /** Shown inside the error card, under the reason — e.g. the sign-in buttons. */
  errorDetail?: React.ReactNode;
  /** Files the user attached to the turn being composed. */
  attachments?: string[];
  onBrowseContext?: () => void;
  onRemoveAttachment?: (path: string) => void;
  imageAttachments?: AgentImageAttachment[];
  onRemoveImage?: (id: string) => void;
  /** Cross-module refs an `agent.attach` intent handed this turn. */
  refs?: EntityRef[];
  onRemoveRef?: (ref: EntityRef) => void;
  onPasteImage?: (file: File) => void;
  onDropImage?: (file: File) => void;
  imageError?: string;
  queuedPrompts?: QueuedAgentPrompt[];
  queuePaused?: boolean;
  /** Why the queue paused, when a refusal said; shown next to Paused. */
  queuePausedReason?: string;
  onResumeQueued?: () => void;
  onCancelQueued?: (id: string) => void;
  onEditQueued?: (prompt: QueuedAgentPrompt) => void;
  onOpenFile?: (path: string, line?: number) => void;
  /**
   * A problem the vendor reported about the session as a whole, and what the
   * user can do about it. Shown above the composer, where the error card is.
   */
  failure?: SessionFailure | null;
  onFailureAction?: (action: string) => void;
  onDismissFailure?: () => void;
  /** What the session works towards, and the ask to change it; see AgentComposer. */
  goal?: SessionGoal | null;
  onSetGoal?: (objective: string) => void;
  /** Copies a string — a message, a code block. */
  onCopy?: (text: string) => void;
  /** Sends /compact. Offered in the context popover only when the agent lists the command. */
  onCompact?: () => void;
  /**
   * A permission this session is waiting on, asked here rather than in a toast.
   * You are looking at the conversation the request came out of, so the answer
   * belongs in it — a card floating in the corner of the same screen makes you
   * look away from the evidence to answer.
   */
  pendingPermission?: PendingPermission | null;
  onResolvePermission?: (optionId: string | null) => void | Promise<void>;
  permissionBusy?: boolean;
  /**
   * A question this session's agent is waiting on, asked here for the same
   * reason as the permission: the conversation it came out of is on screen.
   */
  pendingElicitation?: PendingElicitation | null;
  onRespondElicitation?: (response: ElicitationResponse) => void | Promise<void>;
  elicitationBusy?: boolean;
  elicitationError?: string;
  /**
   * A session being started. Its folder (and parent) are known; its agent is
   * picked in the composer; nothing exists on the backend until the first
   * message is sent. Takes precedence over `session`.
   */
  draft?: { projectPath: string; workspaceId?: string; parentName?: string; workspaceSelected?: boolean } | null;
  /** Projects the draft's picker offers, most recent first. */
  projects?: string[];
  /** Sets the draft's folder; "" is no project. */
  onProjectChange?: (path: string) => void;
  /** Opens the native folder picker for a project not in the list. */
  onChooseProject?: () => void;
  /** The current workspace, offered as "grant every service" in the picker. */
  workspace?: { id: string; name: string; paths: string[] } | null;
  workspaceChoices?: { id: string; name: string; paths: string[] }[];
  onChooseWorkspace?: (workspaceId: string, paths: string[]) => void;
  onChooseWorkspaceContext?: (workspaceId: string) => void;
  agents?: string[];
  agent?: string;
  onAgentChange?: (agent: string) => void;
  /** What stands between the draft and its first message: an install, a sign-in. */
  draftNotice?: React.ReactNode;
  /** Blocks sending and typing — a create in flight, an agent that cannot start. */
  sendDisabled?: boolean;
  className?: string;
};

/**
 * Agent Hub / Content — Figma `952:2346`, states `918:127` Conversation,
 * `952:2245` Empty, `953:2424` Error.
 *
 * Figma's note: "Module body, 1136x804 for the sidebar-open slot. Empty is the
 * first thing a new user meets, so it says what a session is rather than only
 * that there is none."
 *
 * No header. The session's name, state and actions are all on its sidebar
 * row already — the active one is the highlighted row — and a glass bar
 * repeating them took the top of the transcript to say nothing new. Rename,
 * copy and close live in that row's context menu.
 *
 * Empty has no composer — there is no agent to name and nothing
 * to send to.
 */

/**
 * What the field says when nothing is typed, by the session's state. Only the
 * states where sending does something other than "ask" get a line: a parked
 * session wakes up, and a session waiting on a permission is not going to
 * read a prompt until that is answered — the field still takes one, into the
 * queue, but it should not say the agent is working when it is waiting on you.
 */
export function composerPlaceholder(status: string | undefined): string | undefined {
  switch (status) {
    case "closed":
      return "Send to reopen this session…";
    case "awaiting_permission":
      return "Waiting for your answer above — anything you send is queued…";
    default:
      return undefined;
  }
}

/** The last segment of a path — the folder a developer calls the project. */
function projectLabel(path: string): string {
  return path.split("/").filter(Boolean).pop() ?? path;
}

export function AgentHubContent({
  state: explicitState,
  session,
  items = [],
  usage,
  commands = [],
  contextFiles = [],
  onSelectCommand,
  onSelectContext,
  onSearchContext,
  configOptions = [],
  mode = null,
  availableModes = [],
  onSetMode,
  onSetConfigOption,
  configLoading,
  configError,
  onRefreshConfig,
  focusComposerRequest = 0,
  hasMore = false,
  loadingOlder = false,
  onLoadOlder,
  promptValue,
  onPromptChange,
  onSend,
  onCancel,
  streaming = false,
  errorCause = "crashed",
  errorMessage,
  onRetrySession,
  onNewSession,
  emptyExtra,
  onOpenSettings,
  errorDetail,
  attachments = [],
  onBrowseContext,
  onRemoveAttachment,
  imageAttachments = [],
  onRemoveImage,
  refs = [],
  onRemoveRef,
  onPasteImage,
  onDropImage,
  imageError,
  queuedPrompts = [],
  queuePaused = false,
  queuePausedReason,
  onResumeQueued,
  onCancelQueued,
  onEditQueued,
  onOpenFile,
  onCopy,
  failure,
  onFailureAction,
  onDismissFailure,
  goal,
  onSetGoal,
  onCompact,
  pendingPermission,
  onResolvePermission,
  permissionBusy = false,
  pendingElicitation,
  onRespondElicitation,
  elicitationBusy = false,
  elicitationError,
  draft,
  projects = [],
  onProjectChange,
  onChooseProject,
  workspace = null,
  workspaceChoices = [],
  onChooseWorkspace,
  onChooseWorkspaceContext,
  agents = [],
  agent = "",
  onAgentChange,
  draftNotice,
  sendDisabled = false,
  className,
}: AgentHubContentProps) {
  // A draft has no session yet, so nothing the view holds for the selected
  // one is its to show: the composer used to carry that session's context
  // meter, turn count, mode and commands into a conversation not yet started.
  if (draft) {
    session = null;
    failure = null;
    pendingPermission = null;
    pendingElicitation = null;
    queuedPrompts = [];
    // attachments are not cleared: the view passes the draft's own ("@" picks
    // them before there is a session), the way it passes its images.
    refs = [];
    items = [];
    usage = null;
    commands = [];
    contextFiles = [];
    mode = null;
    availableModes = [];
  }
  // Loaded here rather than in TranscriptStream: the stream is a presentational
  // component that tests render with fixed items, and a fetch inside it would
  // make every one of those hit the bridge.
  const checkpoints = useSessionCheckpoints(session?.id ?? "");
  // A turn is a thing the human said. Counting them here beats threading
  // InvokeResult.turnCount through the view: this survives a reload, because
  // the transcript does. The composer's meter and the summary both read it.
  const turnCount = React.useMemo(
    () =>
      items.filter(
        (item) => item.kind === "user_message_chunk" || item.kind === UpdateKind.UpdateUserMessageChunk,
      ).length,
    [items],
  );
  // The composer tray: where this work is, what runs under it, and — one at a
  // time, in the tray's panel — the permission or question the agent is
  // waiting on. They used to stack above the composer as separate cards.
  const tray = useAgentTray({
    size: "wide",
    session,
    projectPath: draft ? draft.projectPath : session?.projectPath ?? "",
    live: !draft && streaming,
    items,
    onOpenFile,
    // The draft's folder, changeable until the first message fixes it. A
    // child works where its parent does, so its folder is shown and not
    // offered.
    leading:
      draft && onProjectChange && onChooseProject ? (
        <ProjectPicker
          variant="tray"
          value={draft.projectPath}
          projects={projects}
          onChange={onProjectChange}
          onChooseFolder={onChooseProject}
          workspace={workspace}
          workspaceChoices={workspaceChoices}
          onChooseWorkspace={onChooseWorkspace}
          onChooseWorkspaceContext={onChooseWorkspaceContext}
          selectedWorkspaceId={draft.workspaceId ?? ""}
          workspaceSelected={Boolean(draft.workspaceSelected)}
          disabled={Boolean(draft.parentName)}
        />
      ) : undefined,
    permission:
      pendingPermission && onResolvePermission
        ? { pending: pendingPermission, onResolve: onResolvePermission, busy: permissionBusy }
        : null,
    question:
      pendingElicitation && onRespondElicitation
        ? { pending: pendingElicitation, onRespond: onRespondElicitation, busy: elicitationBusy, error: elicitationError }
        : null,
  });
  // Which turn the rewind confirmation is about, and whether the user came in
  // through the pencil. "" means closed; the dialog plans for itself, so
  // nothing is planned for a turn nobody asked about.
  const [rewinding, setRewinding] = React.useState<{ promptMessageId: string; editAfter: boolean }>({
    promptMessageId: "",
    editAfter: false,
  });
  const effectiveState: AgentHubContentState = explicitState
    ? explicitState
    : draft
      ? "Conversation"
      : !session
      ? "Empty"
      : session.status === "error"
        ? "Error"
        : "Conversation";
  const isError = effectiveState === "Error";

  // Offered only when the agent says it has the command: a button that sends a
  // prompt the agent does not understand is a button that makes it reply "?".
  const canCompact = commands.some((command) => command.name.replace(/^\//, "") === "compact");

  const displayTitle = draft
    ? "New session"
    : session?.title || (session ? `Session ${session.id.slice(0, 8)}` : "");
  const vendor = draft ? agent : session?.agentType ?? "";

  return (
    <main
      role="main"
      aria-label="Agent Hub Content"
      data-testid="agent-hub-content"
      data-state={effectiveState}
      className={cn(
        // No side or bottom padding: the transcript scrolls, and padding out
        // here put its scrollbar 16px in from the card's edge. Each body pads
        // its own content (the stream's px-6, the composer region's px-6 pb-3).
        "relative flex size-full min-h-0 min-w-0 flex-col overflow-hidden",
        className,
      )}
    >
      {effectiveState === "Empty" ? (
        // Figma's note: Empty is the first thing a new user meets, so it
        // explains what a session IS. The old copy said only where to click.
        <EmptyState
          data-testid="agent-hub-content-empty"
          className="min-h-0"
          icon={Bot}
          title="No agent running"
          body="Each session gets its own sandbox, its own working directory, and its own connection to DevKit's tools. Sessions started by an agent appear as children of it."
          action={onNewSession ? { label: "New session", icon: Plus, onClick: onNewSession } : undefined}
        >
          {emptyExtra}
        </EmptyState>
      ) : (
        // Error and Conversation share one container and one TranscriptStream,
        // so a session failing does not remount the transcript and lose its
        // scroll position and folded turns. Error is not centred: Figma keeps
        // the transcript that led here above the card, because the failed tool
        // call is the evidence for the error.
        <div
          data-testid={isError ? "agent-hub-content-error" : "agent-hub-content-conversation"}
          // A column of two regions. The transcript takes what is left and
          // scrolls in it; the composer region takes its own height below it,
          // in the normal flow — nothing is laid over the transcript's foot,
          // so no text scrolls behind the field. `agent-conversation` makes
          // `cqh` in the caps below mean this box's height.
          className="agent-conversation relative flex min-h-0 w-full min-w-0 flex-1 flex-col"
        >
          <div data-testid="agent-hub-transcript-region" className="min-h-0 flex-1 overflow-hidden">
            {draft ? (
              // Nothing to show yet but where the agent will work and what it
              // may reach: the folder is the sandbox, and this is the last
              // moment before that is fixed.
              <div
                data-testid="agent-hub-draft"
                className="flex size-full flex-col items-center justify-center px-6"
              >
                <div className="flex w-full max-w-120 flex-col gap-3">
                  <p className="text-title-14 text-text-primary">
                    {draft.projectPath
                      ? `Start a session in ${projectLabel(draft.projectPath) || draft.projectPath}`
                      : "Start a session without a project"}
                    {draft.parentName ? (
                      <span className="text-text-tertiary">{` · from ${draft.parentName}`}</span>
                    ) : null}
                  </p>
                  <p className="text-body-13 text-text-tertiary">
                    {draft.projectPath
                      ? `Pick an agent below and send your first message. It can only reach paths inside ${draft.projectPath}.`
                      : "Pick an agent below and send your first message. It gets an empty scratch folder of its own and cannot reach your projects."}
                  </p>
                  {draftNotice}
                  {errorMessage ? (
                    <p data-testid="agent-hub-draft-error" className="text-body-13 text-status-danger">
                      {errorMessage}
                    </p>
                  ) : null}
                </div>
              </div>
            ) : (
              <TranscriptStream
                items={items}
                streamKey={session?.id}
                vendor={vendor}
                checkpoints={checkpoints}
                loadFileDiff={loadCheckpointFileDiff}
                onRewind={(promptMessageId, editAfter) => setRewinding({ promptMessageId, editAfter })}
                streaming={!isError && streaming}
                onCopy={onCopy}
                onEditPrompt={onPromptChange}
                hasMore={hasMore}
                loadingOlder={loadingOlder}
                onLoadOlder={onLoadOlder}
                onOpenFile={onOpenFile}
                // 760 here; the rail keeps 720. On a wide conversation the
                // scroller's right padding clears the docked summary, so the
                // column centres in what is left rather than running under it.
                columnClassName="max-w-190"
                viewportClassName="@6xl/agent-conversation:pr-80"
              />
            )}
          </div>

          {/* The composer region: the same reading column as the transcript
              (max-w-190, px-6, and the same room for a docked summary), a small
              inset at the foot, and its own height.
              `shrink` + the cap: on a short pane it gives way before the
              transcript does, and what does not fit scrolls inside it rather
              than pushing Send out of the pane. */}
          <div
            data-testid="agent-composer-region"
            className="composer-region-cap flex min-h-0 w-full shrink-0 flex-col px-6 pb-3 @6xl/agent-conversation:pr-80"
          >
            <div className="mx-auto flex min-h-0 w-full max-w-190 flex-col gap-2">
              {/* A failed session keeps its composer. The card used to replace it,
                  which left Retry as the only way forward: the same prompt again,
                  or nothing. A turn that failed is often one you want to answer
                  with different words, and the rail — the same session by another
                  route — never took the field away. So the feedback sits in the
                  region, in the flow, above the cluster; it scrolls in its own
                  box if both cards are showing on a short pane. */}
              {failure || isError ? (
                <div data-testid="agent-composer-feedback" className="flex max-h-40 min-h-0 shrink flex-col gap-2 overflow-y-auto">
                  {/* Above the error card: a vendor failure explains why a turn
                      stopped, and the card below it only says that one did. */}
                  {failure && onFailureAction ? (
                    <AgentSessionFailure
                      failure={failure}
                      onAction={onFailureAction}
                      onDismiss={onDismissFailure}
                      className="max-w-none shrink-0"
                    />
                  ) : null}
                  {isError ? (
                    <AgentSessionError
                      cause={errorCause}
                      agentName={displayTitle || "Agent"}
                      reason={errorMessage}
                      onRetry={onRetrySession}
                      onOpenSettings={onOpenSettings}
                      // No Close here. The thing to do with a failed turn is retry
                      // it, and a button that parks the session sat right beside the
                      // one that retries it — which is how a session people meant to
                      // retry got closed instead. Close stays in the ⋯ menu, where it
                      // is chosen rather than reached for.
                      className="w-full max-w-none shrink-0"
                    >
                      {errorDetail}
                    </AgentSessionError>
                  ) : null}
                </div>
              ) : null}
              <AgentComposer
                size="wide"
                trayStrip={tray.strip}
                trayPanel={tray.panel}
                value={promptValue}
                onChange={onPromptChange}
                onSend={onSend}
                onCancel={onCancel}
                streaming={draft ? false : streaming}
                usage={usage}
                goal={draft ? null : goal}
                onSetGoal={draft ? undefined : onSetGoal}
                turnCount={turnCount}
                vendor={vendor}
                // A draft is not named yet; its placeholder addresses the agent
                // that will answer — "Ask Claude…", not "Ask New session…".
                agentName={draft ? vendorInfo(agent).name : displayTitle}
                // A closed session says what sending will do, since it does one
                // thing more than usual: it wakes the agent's process back up.
                placeholder={composerPlaceholder(session?.status)}
                commands={commands}
                contextFiles={contextFiles}
                onSelectCommand={onSelectCommand}
                onSelectContext={onSelectContext}
                onSearchContext={onSearchContext}
                configOptions={configOptions}
                mode={mode}
                availableModes={availableModes}
                onSetMode={onSetMode}
                onSetConfigOption={onSetConfigOption}
                configLoading={configLoading}
                configError={configError}
                onRefreshConfig={onRefreshConfig}
                focusRequest={focusComposerRequest}
                onCompact={canCompact ? onCompact : undefined}
                agents={draft ? agents : undefined}
                agent={draft ? agent : undefined}
                onAgentChange={draft ? onAgentChange : undefined}
                disabled={sendDisabled}
                attachedFiles={attachments}
                onBrowseContext={onBrowseContext}
                onRemoveAttachment={onRemoveAttachment}
                imageAttachments={imageAttachments}
                onRemoveImage={onRemoveImage}
                refs={refs}
                onRemoveRef={onRemoveRef}
                onPasteImage={onPasteImage}
                onDropImage={onDropImage}
                imageError={imageError}
                queuedPrompts={queuedPrompts}
                queuePaused={queuePaused}
                queuePausedReason={queuePausedReason}
                onResumeQueued={onResumeQueued}
                onCancelQueued={onCancelQueued}
                onEditQueued={onEditQueued}
                className="min-h-0 w-full"
              />
            </div>
          </div>

          {/* The session summary. Docked at the top-right when the conversation
              is at least @6xl (72rem) wide — a container query on this box, not
              the window — and a round corner button with the same content as a
              popover below that. Both are drawn and CSS picks one: no measuring.
              The dock lets clicks through its empty height; the card itself
              takes them, and scrolls inside itself past the box's height. */}
          {session && !draft ? (
            <>
              <div
                data-testid="session-summary-dock"
                className="pointer-events-none absolute top-4 right-4 bottom-4 z-10 hidden @6xl/agent-conversation:block"
              >
                <SessionSummaryCard
                  sessionId={session.id}
                  items={items}
                  checkpoints={checkpoints}
                  usage={usage}
                  turnCount={turnCount}
                  live={streaming}
                  onOpenFile={onOpenFile}
                />
              </div>
              <div
                data-testid="session-summary-corner"
                className="absolute top-3 right-3 z-10 @6xl/agent-conversation:hidden"
              >
                <SessionSummaryButton
                  sessionId={session.id}
                  items={items}
                  checkpoints={checkpoints}
                  usage={usage}
                  turnCount={turnCount}
                  live={streaming}
                  onOpenFile={onOpenFile}
                />
              </div>
            </>
          ) : null}
        </div>
      )}
      {/* Mounted beside the session dialogs rather than inside the turn: the
          turn is virtualised, and a dialog whose host can be unmounted by a
          scroll is a dialog that closes when the user scrolls. */}
      <RewindDialog
        open={Boolean(rewinding.promptMessageId)}
        onOpenChange={(open) => setRewinding((previous) => (open ? previous : { promptMessageId: "", editAfter: false }))}
        sessionId={session?.id ?? ""}
        promptMessageId={rewinding.promptMessageId}
        checkpoint={rewinding.promptMessageId ? checkpoints.get(rewinding.promptMessageId) : undefined}
        queuedPrompts={queuedPrompts?.length ?? 0}
        editAfter={rewinding.editAfter}
        onRewound={(result, editAfter) => {
          // The message goes back in the composer only when they asked for it
          // there. A plain rewind leaves the composer alone.
          if (editAfter && result.promptText) onPromptChange(result.promptText);
        }}
      />
    </main>
  );
}
