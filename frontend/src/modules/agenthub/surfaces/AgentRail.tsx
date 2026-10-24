import { useState } from "react";
import { EmptyState } from "@/components/ui/empty-state";
import { cn } from "@/lib/utils";
import type { AgentSession, ElicitationResponse, PendingElicitation, PendingPermission } from "@/bridge/agenthub";
import type { QueuedAgentPrompt } from "@/stores/agentSessions";
import type { AvailableCommand, ConfigOption, SessionFailure, SessionMode, TranscriptItem, Usage } from "../types";
import type { ContextFileItem } from "./AgentMenu";
import { TranscriptStream } from "./TranscriptStream";
import { refContextOf } from "../transcript/openReference";
import { loadCheckpointFileDiff, useSessionCheckpoints } from "../useSessionCheckpoints";
import { RewindDialog } from "./RewindDialog";
import { AgentComposer } from "./AgentComposer";
import { composerPlaceholder } from "./AgentHubContent";
import { AgentSessionFailure } from "./AgentSessionFailure";
import { useAgentTray } from "./composer/agentTray";
import { AgentState, type AgentStatus } from "./AgentState";
import { VendorIcon } from "./VendorIcon";

export type AgentRailState = "Conversation" | "Empty";

export type AgentRailProps = {
  state?: AgentRailState;
  activeSession?: AgentSession | null;
  items?: TranscriptItem[];
  /** Older transcript pages exist above the ones held (see useAgentTranscript). */
  hasMore?: boolean;
  loadingOlder?: boolean;
  onLoadOlder?: () => void;
  usage?: Usage | null;
  commands?: AvailableCommand[];
  contextFiles?: ContextFileItem[];
  onSelectCommand?: (name: string) => void;
  /** What "@" finds for a query in the session's folders. */
  onSearchContext?: (query: string) => Promise<ContextFileItem[]>;
  onBrowseContext?: () => Promise<string[]>;
  queuedPrompts?: QueuedAgentPrompt[];
  queuePaused?: boolean;
  /** Why the queue paused, when a refusal said; shown next to Paused. */
  queuePausedReason?: string;
  onResumeQueued?: () => void;
  onCancelQueued?: (id: string) => void;
  onEditQueued?: (prompt: QueuedAgentPrompt) => void;
  configOptions?: ConfigOption[];
  mode?: string | null;
  availableModes?: SessionMode[];
  onSetMode?: (mode: string) => void;
  onSetConfigOption?: (optionId: string, value: string) => void;
  onOpenFile?: (path: string, line?: number) => void;
  promptValue: string;
  onPromptChange: (value: string) => void;
  /** Copy, in each message's hover card. The rail had none; the module always had.  */
  onCopy?: (text: string) => void;
  onSend: () => void;
  onCancel: () => void;
  streaming?: boolean;
  pendingPermission?: PendingPermission | null;
  onResolvePermission?: (optionId: string | null) => void | Promise<void>;
  permissionBusy?: boolean;
  permissionError?: string;
  pendingElicitation?: PendingElicitation | null;
  onRespondElicitation?: (response: ElicitationResponse) => void | Promise<void>;
  elicitationBusy?: boolean;
  elicitationError?: string;
  /**
   * A problem the vendor reports about the session as a whole (or a rejected
   * rate limit, read the same way — see rateLimitFailure): the Agent Hub tab
   * shows this above its composer, and the rail had no such slot at all, so a
   * session watched only from here never learned why a turn stopped.
   */
  failure?: SessionFailure | null;
  onDismissFailure?: () => void;
  errorMessage?: string;
  sendDisabled?: boolean;
  className?: string;
};

function toAgentStatus(status: string): AgentStatus {
  switch (status) {
    case "running":
      return "running";
    case "awaiting_permission":
      return "awaiting";
    case "error":
      return "error";
    case "closed":
      return "closed";
    default:
      return "idle";
  }
}

/**
 * Agent / Rail — Figma `915:97`, states `915:35` Conversation and
 * `915:36` Empty.
 *
 * Figma's note: "The cross-module surface, 320 wide to match the shell's panel
 * slot. Present in every module so a swarm keeps running while the person works
 * elsewhere. Permission requests land here inline rather than in a modal: the
 * request blocks the agent, not the person."
 *
 * Three bands: a header line, the transcript, the rail composer. Which session
 * is shown is chosen by the tabs in the pane's own header (AgentPaneTabs) —
 * Agent Hub's tabs — so the rail no longer carries a strip or a title of its
 * own. The rail paints nothing — panel-surface behind it is the ground, so
 * a fill would be a second opaque layer on an opaque one. Padding is asymmetric
 * on purpose (pl-3 pr-5), leaving the margin the scroll ruler lives in.
 */
export function AgentRail({
  state: explicitState,
  activeSession,
  items = [],
  hasMore = false,
  loadingOlder = false,
  onLoadOlder,
  usage,
  commands = [],
  contextFiles = [],
  onSelectCommand,
  onSearchContext,
  onBrowseContext,
  queuedPrompts = [],
  queuePaused = false,
  queuePausedReason,
  onResumeQueued,
  onCancelQueued,
  onEditQueued,
  configOptions = [],
  mode = null,
  availableModes = [],
  onSetMode,
  onSetConfigOption,
  onOpenFile,
  promptValue,
  onPromptChange,
  onCopy,
  onSend,
  onCancel,
  streaming = false,
  pendingPermission,
  onResolvePermission,
  permissionBusy = false,
  permissionError,
  pendingElicitation,
  onRespondElicitation,
  elicitationBusy = false,
  elicitationError,
  failure,
  onDismissFailure,
  errorMessage,
  sendDisabled = false,
  className,
}: AgentRailProps) {
  const checkpoints = useSessionCheckpoints(activeSession?.id ?? "");
  // References in the prose resolve in this session's workspace. A new object
  // per render is fine: TranscriptStream keeps it by value.
  const refContext = refContextOf(activeSession);
  // Which turn's files the confirmation is about, or "" for closed. The dialog
  // plans for itself, so opening it costs one call and nothing is planned for a
  // turn nobody asked about.
  const [rewinding, setRewinding] = useState<{ promptMessageId: string; editAfter: boolean }>({
    promptMessageId: "",
    editAfter: false,
  });
  const effectiveState: AgentRailState = explicitState
    ? explicitState
    : !activeSession
      ? "Empty"
      : "Conversation";

  const vendor = activeSession?.agentType ?? "";

  const isPendingForActiveSession =
    pendingPermission && activeSession && pendingPermission.sessionId === activeSession.id;
  const isQuestionForActiveSession =
    pendingElicitation && activeSession && pendingElicitation.sessionId === activeSession.id;
  // The same tray the Hub tab's composer has: the rail has no sidebar and a
  // 320px transcript, so this is where a subagent working in the background,
  // a permission or a question shows itself. Icon-only by default here.
  const tray = useAgentTray({
    size: "rail",
    session: activeSession,
    projectPath: activeSession?.projectPath ?? "",
    live: streaming,
    items,
    onOpenFile,
    permission:
      isPendingForActiveSession && pendingPermission
        ? { pending: pendingPermission, onResolve: onResolvePermission ?? (() => {}), busy: permissionBusy, error: permissionError }
        : null,
    question:
      isQuestionForActiveSession && pendingElicitation
        ? {
            pending: pendingElicitation,
            onRespond: onRespondElicitation ?? (() => {}),
            busy: elicitationBusy,
            error: elicitationError,
          }
        : null,
  });

  return (
    <section
      role="region"
      aria-label="Agent Rail"
      data-testid="agent-rail"
      data-state={effectiveState}
      className={cn(
        // No fill: panel-surface behind the rail is the ground. No side
        // padding: the transcript scrolls edge to edge so its scrollbar sits on
        // the panel's edge, and each band below pads itself.
        // w-full, not a fixed 320: the split panel is resizable, and a fixed
        // width left the rail where it was with empty panel beside it.
        "flex h-full w-full min-w-0 flex-col overflow-hidden pt-0 pb-3 select-none",
        className,
      )}
    >
      {/* Figma 913:31's meta line: vendor mark, live state, then depth and
          cwd — the two facts that say what this agent may touch. The title is
          the tab's, in the pane header above. */}
      {activeSession ? (
        <header
          data-testid="agent-rail-header"
          className="flex w-full shrink-0 items-center gap-2 py-2 pr-4 pl-5"
        >
          <VendorIcon vendor={activeSession.agentType} className="size-3.5 shrink-0 text-icon-secondary" />
          <AgentState status={toAgentStatus(activeSession.status)} />
          <p className="min-w-0 flex-1 truncate text-caption-11 text-text-tertiary" title={activeSession.projectPath || "."}>
            {`depth ${activeSession.depth ?? 0} · ${activeSession.projectPath || "."}`}
          </p>
        </header>
      ) : null}

      {/* 3. Rail Body */}
      {effectiveState === "Empty" ? (
        <EmptyState
          size="rail"
          data-testid="agent-rail-empty"
          className="flex-1"
          title="No session open"
          body="Pick one from the menu above, or start one in Agent Hub — it needs a vendor and a working directory."
        />
      ) : (
        <div
          data-testid="agent-rail-conversation"
          // `agent-conversation`: the box `cqh` in the composer's caps is
          // measured against — the rail's height, not the window's.
          className="agent-conversation flex flex-1 flex-col min-h-0 min-w-0"
        >
          {/* Transcript Area */}
          <div className="flex-1 min-h-0 overflow-hidden">
            <TranscriptStream
              items={items}
              streamKey={activeSession?.id}
              vendor={vendor}
              checkpoints={checkpoints}
              loadFileDiff={loadCheckpointFileDiff}
              streaming={streaming}
              onCopy={onCopy}
              onEditPrompt={onPromptChange}
              onRewind={(promptMessageId, editAfter) => setRewinding({ promptMessageId, editAfter })}
              hasMore={hasMore}
              loadingOlder={loadingOlder}
              onLoadOlder={onLoadOlder}
              onOpenFile={onOpenFile}
              refContext={refContext}
              // The rail sits beside the work already: a reference opens in place.
              refPlacement="replace"
            />
          </div>

          <RewindDialog
            open={Boolean(rewinding.promptMessageId)}
            onOpenChange={(open) =>
              setRewinding((previous) => (open ? previous : { promptMessageId: "", editAfter: false }))
            }
            sessionId={activeSession?.id ?? ""}
            promptMessageId={rewinding.promptMessageId}
            checkpoint={rewinding.promptMessageId ? checkpoints.get(rewinding.promptMessageId) : undefined}
            queuedPrompts={queuedPrompts?.length ?? 0}
            editAfter={rewinding.editAfter}
            onRewound={(result, editAfter) => {
              if (editAfter && result.promptText) onPromptChange(result.promptText);
            }}
          />

          {/* Same slot Agent Hub gives a vendor failure, just above the
              composer: the rail is the same session by another route, and
              had no such card at all — a turn that stopped for a reason the
              vendor gave (or a rejected rate limit, read the same way) said
              nothing here even when it did in the Hub tab.
              No onAction wired: the only failure the rail feeds in today is
              the synthetic rate-limit one, which carries no actions — a
              vendor failure with a real action (sign-in, retry) needs the
              Hub's own flows for those, not yet threaded through the rail. */}

          {failure ? (
            <div className="px-4 pb-1">
              <AgentSessionFailure
                failure={failure}
                onAction={() => {}}
                onDismiss={onDismissFailure}
                className="max-w-none"
              />
            </div>
          ) : null}

          {errorMessage ? (
            <p className="px-6 pb-1 text-caption-11 text-status-danger">{errorMessage}</p>
          ) : null}

          {/* Rail Composer at bottom: the same region as the Hub's, in the flow
              below the transcript, capped so a short rail scrolls inside it. */}
          <div data-testid="agent-composer-region" className="composer-region-cap flex min-h-0 shrink-0 flex-col px-4 pt-1">
            <AgentComposer
              size="rail"
              trayStrip={tray.strip}
              trayPanel={tray.panel}
              value={promptValue}
              onChange={onPromptChange}
              onSend={onSend}
              onCancel={onCancel}
              streaming={streaming}
              usage={usage}
              vendor={vendor}
              placeholder={composerPlaceholder(activeSession?.status)}
              commands={commands}
              contextFiles={contextFiles}
              onSelectCommand={onSelectCommand}
              onSearchContext={onSearchContext}
              onBrowseContext={onBrowseContext}
              queuedPrompts={queuedPrompts}
              queuePaused={queuePaused}
              queuePausedReason={queuePausedReason}
              onResumeQueued={onResumeQueued}
              onCancelQueued={onCancelQueued}
              onEditQueued={onEditQueued}
              configOptions={configOptions}
              mode={mode}
              availableModes={availableModes}
              onSetMode={onSetMode}
              onSetConfigOption={onSetConfigOption}
              disabled={sendDisabled}
              className="min-h-0 w-full"
            />
          </div>
        </div>
      )}
    </section>
  );
}
