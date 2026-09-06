import { Bot, ChevronRight, Folder, Sparkles } from "lucide-react";
import { cn } from "@/lib/utils";
import type { AgentSession } from "@/bridge/agenthub";
import type { AvailableCommand, TranscriptItem, Usage } from "../types";
import { TranscriptStream } from "./TranscriptStream";
import { AgentComposer } from "./AgentComposer";
import { AgentSessionError, type SessionErrorCause } from "./AgentSessionError";

export type AgentHubContentState = "Conversation" | "Empty" | "Error";

export type AgentHubContentProps = {
  state?: AgentHubContentState;
  session?: AgentSession | null;
  parentSession?: AgentSession | null;
  items?: TranscriptItem[];
  usage?: Usage | null;
  commands?: AvailableCommand[];
  promptValue: string;
  onPromptChange: (value: string) => void;
  onSend: () => void;
  onCancel: () => void;
  streaming?: boolean;
  errorCause?: SessionErrorCause;
  errorMessage?: string;
  onRetrySession?: () => void;
  onSwitchVendor?: () => void;
  onNewSession?: () => void;
  className?: string;
};

/**
 * Agent Hub / Content (Figma 1136×804)
 * The wide reading surface of the Agent Hub module.
 * Manages 3 states: Conversation, Empty, Error.
 */
export function AgentHubContent({
  state: explicitState,
  session,
  parentSession,
  items = [],
  usage,
  commands = [],
  promptValue,
  onPromptChange,
  onSend,
  onCancel,
  streaming = false,
  errorCause = "crashed",
  errorMessage,
  onRetrySession,
  onSwitchVendor,
  onNewSession,
  className,
}: AgentHubContentProps) {
  // Determine effective state
  const effectiveState: AgentHubContentState = explicitState
    ? explicitState
    : !session
      ? "Empty"
      : session.status === "error"
        ? "Error"
        : "Conversation";

  const displayTitle = session?.title || (session ? `Session ${session.id.slice(0, 8)}` : "");
  const vendor = session?.agentType || "claude";

  return (
    <main
      role="main"
      aria-label="Agent Hub Content"
      data-testid="agent-hub-content"
      data-state={effectiveState}
      className={cn(
        "flex flex-1 flex-col min-w-0 min-h-0 bg-transparent overflow-hidden",
        className,
      )}
    >
      {/* Surface Header (Breadcrumb & Meta) */}
      {session ? (
        <header
          data-testid="agent-hub-content-header"
          className="flex h-11 shrink-0 items-center justify-between border-b border-border-subtle bg-surface-base/80 px-4 backdrop-blur-sm select-none"
        >
          {/* Breadcrumb / Swarm Hierarchy */}
          <div className="flex items-center gap-1.5 min-w-0">
            {parentSession ? (
              <>
                <span className="truncate text-caption-11 text-text-tertiary">
                  {parentSession.title || parentSession.id.slice(0, 8)}
                </span>
                <ChevronRight className="size-3 text-text-tertiary shrink-0" aria-hidden="true" />
              </>
            ) : null}

            <h2 className="truncate text-title-16 text-text-primary">
              {displayTitle}
            </h2>

            <span className="ml-1 rounded-sm bg-surface-tinted px-1.5 py-0.5 text-code-11 uppercase text-text-tertiary">
              {vendor}
            </span>

            {session.depth > 0 ? (
              <span className="rounded-sm border border-border-subtle bg-surface-base px-1.5 py-0.5 text-caption-11 text-text-tertiary">
                depth {session.depth}
              </span>
            ) : null}
          </div>

          {/* Right meta: path + status */}
          <div className="flex items-center gap-2 shrink-0">
            <div className="flex items-center gap-1 text-caption-11 text-text-tertiary">
              <Folder className="size-3 shrink-0" aria-hidden="true" />
              <span className="truncate max-w-[180px] text-code-11">
                {session.projectPath || "."}
              </span>
            </div>

            <span
              data-testid="content-header-status"
              className={cn(
                "rounded-control px-2 py-0.5 text-body-12",
                session.status === "running"
                  ? "bg-status-info/10 text-status-info"
                  : session.status === "awaiting_permission"
                    ? "bg-status-warning/10 text-status-warning"
                    : session.status === "error"
                      ? "bg-status-danger/10 text-status-danger"
                      : "bg-surface-tinted text-text-tertiary",
              )}
            >
              {session.status || "idle"}
            </span>
          </div>
        </header>
      ) : null}

      {/* Surface Body based on State */}
      {effectiveState === "Empty" ? (
        <div
          data-testid="agent-hub-content-empty"
          className="flex flex-1 flex-col items-center justify-center gap-3 p-8 text-center"
        >
          <div className="flex size-12 items-center justify-center rounded-xl bg-surface-raised border border-border-subtle text-text-tertiary shadow-sm">
            <Bot className="size-6" aria-hidden="true" />
          </div>
          <div className="max-w-sm">
            <h3 className="text-title-16 text-text-primary">No active agent session</h3>
            <p className="mt-1 text-body-13 text-text-secondary">
              Select a session from the swarm tree on the left, or launch a new agent session.
            </p>
          </div>
          {onNewSession ? (
            <button
              type="button"
              data-testid="empty-new-session-button"
              onClick={onNewSession}
              className="mt-2 inline-flex h-8 items-center gap-1.5 rounded-control bg-action-primary px-3.5 text-label-12 text-action-primary-foreground transition-transform active:scale-95 hover:opacity-90 cursor-pointer"
            >
              <Sparkles className="size-3.5" aria-hidden="true" />
              <span>Launch New Agent</span>
            </button>
          ) : null}
        </div>
      ) : effectiveState === "Error" ? (
        <div
          data-testid="agent-hub-content-error"
          className="flex flex-1 items-center justify-center p-6"
        >
          <AgentSessionError
            cause={errorCause}
            message={errorMessage}
            onRetry={onRetrySession}
            onSwitchVendor={onSwitchVendor}
          />
        </div>
      ) : (
        /* Conversation State */
        <div
          data-testid="agent-hub-content-conversation"
          className="flex flex-1 flex-col min-h-0 min-w-0"
        >
          {/* Transcript scroller area */}
          <div className="flex-1 min-h-0 overflow-hidden">
            <TranscriptStream
              items={items}
              vendor={vendor}
              loading={false}
            />
          </div>

          {/* Composer at the bottom — wide size */}
          <div className="shrink-0 px-6 pb-5 pt-1">
            <AgentComposer
              size="wide"
              value={promptValue}
              onChange={onPromptChange}
              onSend={onSend}
              onCancel={onCancel}
              streaming={streaming}
              usage={usage}
              vendor={vendor}
              commands={commands}
            />
          </div>
        </div>
      )}
    </main>
  );
}
