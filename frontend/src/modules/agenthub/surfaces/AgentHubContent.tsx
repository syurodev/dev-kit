import * as React from "react";
import { UpdateKind } from "../../../../bindings/devkit/internal/agenthub/models";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyTitle,
} from "@/components/ui/empty";
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
import { AgentSubagentTracker } from "./AgentSubagentTracker";
import { ProjectPicker } from "./ProjectPicker";
import { AgentSessionError, type SessionErrorCause } from "./AgentSessionError";
import { AgentSessionFailure } from "./AgentSessionFailure";
import { AgentPermissionRequest } from "./AgentPermissionRequest";
import { AgentQuestionCard } from "./AgentQuestionCard";
import { PermissionPresence } from "./permissionCard";
import { vendorInfo } from "../vendors";

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
  configOptions?: ConfigOption[];
  mode?: string | null;
  availableModes?: SessionMode[];
  onSetMode?: (mode: string) => void;
  onSetConfigOption?: (optionId: string, value: string) => void;
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
  draft?: { projectPath: string; parentName?: string; workspaceSelected?: boolean } | null;
  /** Projects the draft's picker offers, most recent first. */
  projects?: string[];
  /** Sets the draft's folder; "" is no project. */
  onProjectChange?: (path: string) => void;
  /** Opens the native folder picker for a project not in the list. */
  onChooseProject?: () => void;
  /** The current workspace, offered as "grant every service" in the picker. */
  workspace?: { name: string; paths: string[] } | null;
  onChooseWorkspace?: (paths: string[]) => void;
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
  configOptions = [],
  mode = null,
  availableModes = [],
  onSetMode,
  onSetConfigOption,
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
  onChooseWorkspace,
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
    attachments = [];
    refs = [];
    items = [];
    usage = null;
    commands = [];
    contextFiles = [];
    configOptions = [];
    mode = null;
    availableModes = [];
  }
  // Loaded here rather than in TranscriptStream: the stream is a presentational
  // component that tests render with fixed items, and a fetch inside it would
  // make every one of those hit the bridge.
  const checkpoints = useSessionCheckpoints(session?.id ?? "");
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

  // The floating composer's height, measured, so the transcript can pad its
  // foot by exactly that much. A fixed guess is wrong the moment a file is
  // attached and the composer grows a row of chips.
  const composerRef = React.useRef<HTMLDivElement>(null);
  const [composerHeight, setComposerHeight] = React.useState(0);
  React.useEffect(() => {
    const el = composerRef.current;
    if (!el || typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(([entry]) =>
      setComposerHeight(Math.ceil(entry.borderBoxSize?.[0]?.blockSize ?? entry.contentRect.height)),
    );
    observer.observe(el);
    return () => observer.disconnect();
  }, [effectiveState]);
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
        // Relative, so the composer can float over the transcript's foot.
        // No side or bottom padding either: the transcript scrolls, and padding
        // out here put its scrollbar 16px in from the card's edge. Each body
        // pads its own content (the stream's px-6, the composer's px-6 pb-3).
        "relative flex size-full min-h-0 min-w-0 flex-col overflow-hidden",
        className,
      )}
    >
      {effectiveState === "Empty" ? (
        <Empty data-testid="agent-hub-content-empty" className="min-h-0 gap-3 p-6">
          <EmptyHeader className="w-105 max-w-full gap-3">
            <EmptyTitle className="text-title-16 text-text-secondary">No agent running</EmptyTitle>
            {/* Figma's note: Empty is the first thing a new user meets, so it
                explains what a session IS. The old copy said only where to click. */}
            <EmptyDescription className="text-text-tertiary">
              Each session gets its own sandbox, its own working directory, and its own
              connection to DevKit&apos;s tools. Sessions started by an agent appear as
              children of it.
            </EmptyDescription>
          </EmptyHeader>
          {onNewSession ? (
            <EmptyContent>
              <Button
                type="button"
                size="sm"
                data-testid="empty-new-session-button"
                onClick={onNewSession}
                className="h-8 px-4"
              >
                New session
              </Button>
            </EmptyContent>
          ) : null}
        </Empty>
      ) : (
        // Error and Conversation share one container and one TranscriptStream,
        // so a session failing does not remount the transcript and lose its
        // scroll position and folded turns. Error is not centred: Figma keeps
        // the transcript that led here above the card, because the failed tool
        // call is the evidence for the error.
        <div
          data-testid={isError ? "agent-hub-content-error" : "agent-hub-content-conversation"}
          className="relative flex min-h-0 w-full min-w-0 flex-1 flex-col"
          // How much of the transcript's foot the composer covers; the stream
          // pads by it so the last line of an answer scrolls clear.
          style={{ "--composer-inset": `${composerHeight}px` } as React.CSSProperties}
        >
          <div className="min-h-0 flex-1 overflow-hidden">
            {draft ? (
              // Nothing to show yet but where the agent will work and what it
              // may reach: the folder is the sandbox, and this is the last
              // moment before that is fixed.
              <div
                data-testid="agent-hub-draft"
                className="flex size-full flex-col items-center justify-center px-6"
                style={{ paddingBottom: "var(--composer-inset, 0px)" }}
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
              />
            )}
          </div>

          {/* Floats over the transcript's foot, in its reading column, so what
              you write lines up with what you are answering. Only the composer
              takes the pointer; the strip either side of it lets clicks through
              to the text scrolling underneath. */}
          <div
            ref={composerRef}
            // No gap: the cards above the composer carry their own spacing
            // inside the box that animates, so a card closing takes its gap
            // with it instead of leaving 12px to snap shut at the end.
            className="pointer-events-none absolute inset-x-0 bottom-0 flex w-full flex-col px-6 pb-3"
          >
          {/* A failed session keeps its composer. The card used to replace it,
              which left Retry as the only way forward: the same prompt again,
              or nothing. A turn that failed is often one you want to answer
              with different words, and the rail — the same session by another
              route — never took the field away. So the card sits where the
              permission card does, above the field, and the transcript pads
              for both. */}
          {/* Above the error card: a vendor failure explains why a turn
              stopped, and the card below it only says that one did. */}
          {failure && onFailureAction ? (
            <div className="pointer-events-auto mx-auto mb-3 w-full max-w-180">
              <AgentSessionFailure
                failure={failure}
                onAction={onFailureAction}
                onDismiss={onDismissFailure}
                className="max-w-none"
              />
            </div>
          ) : null}
          {isError ? (
            // An opaque base under the card: its danger wash is translucent,
            // which is right on a surface and wrong floating over a transcript,
            // where the rows read straight through it.
            <div className="pointer-events-auto mx-auto mb-3 w-full max-w-180 rounded-control bg-surface-base">
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
                className="w-full max-w-none"
              >
                {errorDetail}
              </AgentSessionError>
            </div>
          ) : null}
          {/* Above the composer, in the same reading column: the request is
              answered where the conversation that raised it is, and the
              transcript pads by this whole block (composerRef measures it), so
              nothing it covers is unreachable. */}
          <PermissionPresence
            pendingId={onResolvePermission ? pendingPermission?.id : null}
            className="pointer-events-auto mx-auto w-full max-w-180"
          >
            {pendingPermission && onResolvePermission ? (
              <div className="pb-3">
                <AgentPermissionRequest
                  pending={pendingPermission}
                  session={session}
                  onResolve={onResolvePermission}
                  busy={permissionBusy}
                  size="wide"
                />
              </div>
            ) : null}
          </PermissionPresence>
          {/* The question waits behind a permission rather than beside it: the
              permission is the one with a clock, and two cards stacked over a
              composer leave no transcript to read the evidence in. */}
          <PermissionPresence
            pendingId={onRespondElicitation && !(pendingPermission && onResolvePermission) ? pendingElicitation?.id : null}
            className="pointer-events-auto mx-auto w-full max-w-180"
          >
            {pendingElicitation && onRespondElicitation ? (
              // Capped and scrolled: four questions with previews would
              // otherwise push the composer off the bottom of the window.
              <div className="max-h-screen-60 overflow-y-auto pb-3">
                <AgentQuestionCard
                  pending={pendingElicitation}
                  session={session}
                  onRespond={onRespondElicitation}
                  busy={elicitationBusy}
                  error={elicitationError}
                  size="wide"
                />
              </div>
            ) : null}
          </PermissionPresence>
          {/* Inside the measured layer, so the transcript pads its foot for it
              too. See AgentSubagentTracker. */}
          <AgentSubagentTracker
            items={items}
            live={!draft && streaming}
            onOpenFile={onOpenFile}
            className="pointer-events-auto mx-auto mb-2 max-w-180"
          />
          {/* Where the draft will work, changeable until the first message
              fixes it. A child works where its parent does, so its folder is
              shown and not offered. */}
          {draft && onProjectChange && onChooseProject ? (
            <div className="pointer-events-auto mx-auto flex w-full max-w-180 items-center gap-1 px-2 pb-1">
              <ProjectPicker
                value={draft.projectPath}
                projects={projects}
                onChange={onProjectChange}
                onChooseFolder={onChooseProject}
                workspace={workspace}
                onChooseWorkspace={onChooseWorkspace}
                workspaceSelected={Boolean(draft.workspaceSelected)}
                disabled={Boolean(draft.parentName)}
              />
            </div>
          ) : null}
          <div className="pointer-events-auto mx-auto flex w-full max-w-180 items-center gap-3">
            <AgentComposer
              size="wide"
              value={promptValue}
              onChange={onPromptChange}
              onSend={onSend}
              onCancel={onCancel}
              streaming={draft ? false : streaming}
              usage={usage}
              goal={draft ? null : goal}
              onSetGoal={draft ? undefined : onSetGoal}
              turnCount={
                // A turn is a thing the human said. Counting them here beats
                // threading InvokeResult.turnCount through the view: this
                // survives a reload, because the transcript does.
                items.filter(
                  (item) =>
                    item.kind === "user_message_chunk" ||
                    item.kind === UpdateKind.UpdateUserMessageChunk,
                ).length
              }
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
              configOptions={configOptions}
              mode={mode}
              availableModes={availableModes}
              onSetMode={onSetMode}
              onSetConfigOption={onSetConfigOption}
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
              className="min-w-0 flex-1"
            />
          </div>
          </div>
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
