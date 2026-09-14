import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyTitle,
} from "@/components/ui/empty";
import { cn } from "@/lib/utils";
import type { AgentSession, PendingPermission } from "@/bridge/agenthub";
import type { AgentImageAttachment } from "@/bridge/agenthub";
import type { QueuedAgentPrompt } from "@/stores/agentSessions";
import type { AvailableCommand, ConfigOption, SessionMode, TranscriptItem, Usage } from "../types";
import type { ContextFileItem } from "./AgentMenu";
import { TranscriptStream } from "./TranscriptStream";
import { AgentComposer } from "./AgentComposer";
import { AgentPermissionRequest } from "./AgentPermissionRequest";
import { AgentState, type AgentStatus } from "./AgentState";
import { AgentSwarmStrip } from "./AgentSwarmStrip";
import { VendorIcon } from "./VendorIcon";

export type AgentRailState = "Conversation" | "Empty";

export type AgentRailProps = {
  state?: AgentRailState;
  sessions?: AgentSession[];
  activeSession?: AgentSession | null;
  onSelectSession?: (id: string) => void;
  items?: TranscriptItem[];
  /** Older transcript pages exist above the ones held (see useAgentTranscript). */
  hasMore?: boolean;
  loadingOlder?: boolean;
  onLoadOlder?: () => void;
  usage?: Usage | null;
  commands?: AvailableCommand[];
  contextFiles?: ContextFileItem[];
  onSelectCommand?: (name: string) => void;
  onSelectContext?: (path: string) => void;
  onBrowseContext?: () => void;
  attachedFiles?: string[];
  onRemoveAttachment?: (path: string) => void;
  imageAttachments?: AgentImageAttachment[];
  onRemoveImage?: (id: string) => void;
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
  configOptions?: ConfigOption[];
  mode?: string | null;
  availableModes?: SessionMode[];
  onSetMode?: (mode: string) => void;
  onSetConfigOption?: (optionId: string, value: string) => void;
  onOpenFile?: (path: string, line?: number) => void;
  promptValue: string;
  onPromptChange: (value: string) => void;
  onSend: () => void;
  onCancel: () => void;
  streaming?: boolean;
  pendingPermission?: PendingPermission | null;
  onResolvePermission?: (optionId: string | null) => void | Promise<void>;
  permissionBusy?: boolean;
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
 * Four bands: swarm strip, a header naming the agent, the transcript, the rail
 * composer. The rail paints nothing — panel-surface behind it is the ground, so
 * a fill would be a second opaque layer on an opaque one. Padding is asymmetric
 * on purpose (pl-3 pr-5), leaving the margin the scroll ruler lives in.
 */
export function AgentRail({
  state: explicitState,
  sessions = [],
  activeSession,
  onSelectSession,
  items = [],
  hasMore = false,
  loadingOlder = false,
  onLoadOlder,
  usage,
  commands = [],
  contextFiles = [],
  onSelectCommand,
  onSelectContext,
  onBrowseContext,
  attachedFiles = [],
  onRemoveAttachment,
  imageAttachments = [],
  onRemoveImage,
  onPasteImage,
  onDropImage,
  imageError,
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
  onSend,
  onCancel,
  streaming = false,
  pendingPermission,
  onResolvePermission,
  permissionBusy = false,
  errorMessage,
  sendDisabled = false,
  className,
}: AgentRailProps) {
  const effectiveState: AgentRailState = explicitState
    ? explicitState
    : !activeSession
      ? "Empty"
      : "Conversation";

  const displayTitle =
    activeSession?.title ||
    (activeSession ? `Session ${activeSession.id.slice(0, 8)}` : "");
  const vendor = activeSession?.agentType || "claude";

  const isPendingForActiveSession =
    pendingPermission && activeSession && pendingPermission.sessionId === activeSession.id;

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
      {sessions.length > 1 ? (
        <AgentSwarmStrip
          sessions={sessions}
          activeSessionId={activeSession?.id}
          onSelectSession={onSelectSession}
          className="px-3"
        />
      ) : null}

      {/* Figma 913:31: vendor mark, name, live state, then one quiet line
          carrying depth and cwd — the two facts that say what this agent may
          touch. The header used to show a vendor text chip and a colour-coded
          status pill, a third status vocabulary next to AgentState. */}
      {activeSession ? (
        <header
          data-testid="agent-rail-header"
          // pr-10 clears the split panel's close button, pinned top-right.
          className="flex w-full shrink-0 flex-col gap-1 py-3 pr-10 pl-5"
        >
          <div className="flex w-full items-center gap-2">
            <VendorIcon
              vendor={activeSession.agentType}
              className="size-3.5 text-icon-secondary"
            />
            <span className="min-w-0 flex-1 truncate text-body-13-medium text-text-primary">
              {displayTitle}
            </span>
            <AgentState status={toAgentStatus(activeSession.status)} />
          </div>
          <p className="w-full truncate text-caption-11 text-text-tertiary">
            {`depth ${activeSession.depth ?? 0} · ${activeSession.projectPath || "."}`}
          </p>
        </header>
      ) : null}

      {/* 3. Rail Body */}
      {effectiveState === "Empty" ? (
        <Empty data-testid="agent-rail-empty" className="gap-2 px-6 py-3">
          <EmptyHeader className="w-58 max-w-full gap-2">
            <EmptyTitle className="text-text-secondary">No agents running</EmptyTitle>
            <EmptyDescription className="text-caption-11 text-text-tertiary">
              Open Agent Hub to start one — it needs a vendor and a working
              directory.
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : (
        <div
          data-testid="agent-rail-conversation"
          className="flex flex-1 flex-col min-h-0 min-w-0"
        >
          {/* Inline Permission Request if pending */}
          {isPendingForActiveSession && pendingPermission ? (
            <div
              data-testid="rail-inline-permission"
              className="px-6 pt-2 shrink-0"
            >
              <AgentPermissionRequest
                pending={pendingPermission}
                session={activeSession}
                onResolve={onResolvePermission ?? (() => {})}
                busy={permissionBusy}
              />
            </div>
          ) : null}

          {/* Transcript Area */}
          <div className="flex-1 min-h-0 overflow-hidden">
            <TranscriptStream
              items={items}
              streamKey={activeSession?.id}
              vendor={vendor}
              hasMore={hasMore}
              loadingOlder={loadingOlder}
              onLoadOlder={onLoadOlder}
              onOpenFile={onOpenFile}
            />
          </div>

          {errorMessage ? (
            <p className="px-6 pb-1 text-caption-11 text-status-danger">{errorMessage}</p>
          ) : null}

          {/* Rail Composer at bottom */}
          <div className="shrink-0 px-4 pt-1">
            <AgentComposer
              size="rail"
              value={promptValue}
              onChange={onPromptChange}
              onSend={onSend}
              onCancel={onCancel}
              streaming={streaming}
              usage={usage}
              vendor={vendor}
              commands={commands}
              contextFiles={contextFiles}
              onSelectCommand={onSelectCommand}
              onSelectContext={onSelectContext}
              onBrowseContext={onBrowseContext}
              attachedFiles={attachedFiles}
              onRemoveAttachment={onRemoveAttachment}
              imageAttachments={imageAttachments}
              onRemoveImage={onRemoveImage}
              onPasteImage={onPasteImage}
              onDropImage={onDropImage}
              imageError={imageError}
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
            />
          </div>
        </div>
      )}
    </section>
  );
}
