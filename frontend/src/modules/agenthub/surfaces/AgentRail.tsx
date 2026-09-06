import { Bot, Sparkles } from "lucide-react";
import { cn } from "@/lib/utils";
import type { AgentSession, PendingPermission } from "@/bridge/agenthub";
import type { AvailableCommand, TranscriptItem, Usage } from "../types";
import { TranscriptStream } from "./TranscriptStream";
import { AgentComposer } from "./AgentComposer";
import { AgentPermissionRequest } from "./AgentPermissionRequest";

export type AgentRailState = "Conversation" | "Empty";

export type AgentRailProps = {
  state?: AgentRailState;
  sessions?: AgentSession[];
  activeSession?: AgentSession | null;
  onSelectSession?: (id: string) => void;
  items?: TranscriptItem[];
  usage?: Usage | null;
  commands?: AvailableCommand[];
  promptValue: string;
  onPromptChange: (value: string) => void;
  onSend: () => void;
  onCancel: () => void;
  streaming?: boolean;
  pendingPermission?: PendingPermission | null;
  onResolvePermission?: (optionId: string | null) => void | Promise<void>;
  permissionBusy?: boolean;
  errorMessage?: string;
  onOpenAgentHub?: () => void;
  className?: string;
};

/**
 * Agent / Rail (Figma 320×856)
 * Panel surface hosting conversation with active agent across any module.
 * Features swarm switcher strip, inline permission handling, and rail composer.
 */
export function AgentRail({
  state: explicitState,
  sessions = [],
  activeSession,
  onSelectSession,
  items = [],
  usage,
  commands = [],
  promptValue,
  onPromptChange,
  onSend,
  onCancel,
  streaming = false,
  pendingPermission,
  onResolvePermission,
  permissionBusy = false,
  errorMessage,
  onOpenAgentHub,
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
        "flex h-full w-[320px] flex-col overflow-hidden bg-surface-base select-none",
        className,
      )}
    >
      {/* 1. Swarm Strip / Session Mini-Switcher */}
      {sessions.length > 1 ? (
        <div
          data-testid="rail-swarm-strip"
          className="flex h-8 shrink-0 items-center gap-1 overflow-x-auto border-b border-border-subtle bg-surface-raised px-2 scrollbar-none"
        >
          {sessions.map((s) => (
            <button
              key={s.id}
              type="button"
              data-testid={`rail-swarm-item-${s.id}`}
              onClick={() => onSelectSession?.(s.id)}
              className={cn(
                "flex h-6 items-center gap-1 rounded-sm px-2 transition-colors cursor-pointer shrink-0",
                s.id === activeSession?.id
                  ? "bg-surface-tinted text-text-primary text-caption-11-medium"
                  : "text-text-tertiary hover:text-text-secondary text-caption-11",
              )}
            >
              <span
                className={cn(
                  "size-1.5 rounded-full",
                  s.status === "running"
                    ? "bg-status-info"
                    : s.status === "awaiting_permission"
                      ? "bg-status-warning"
                      : "bg-text-tertiary",
                )}
              />
              <span className="truncate max-w-[80px]">
                {s.title || s.id.slice(0, 6)}
              </span>
            </button>
          ))}
        </div>
      ) : null}

      {/* 2. Rail Header */}
      {activeSession ? (
        <header
          data-testid="agent-rail-header"
          className="flex h-10 shrink-0 items-center justify-between border-b border-border-subtle px-3"
        >
          <div className="flex items-center gap-1.5 min-w-0">
            <span className="truncate text-body-13-medium text-text-primary">
              {displayTitle}
            </span>
            <span className="rounded-sm bg-surface-tinted px-1 text-code-11 uppercase text-text-tertiary">
              {vendor}
            </span>
          </div>

          <span
            className={cn(
              "rounded-control px-1.5 py-0.5 text-code-11",
              activeSession.status === "running"
                ? "text-status-info bg-status-info/10"
                : activeSession.status === "awaiting_permission"
                  ? "text-status-warning bg-status-warning/10"
                  : "text-text-tertiary bg-surface-tinted",
            )}
          >
            {activeSession.status || "idle"}
          </span>
        </header>
      ) : null}

      {/* 3. Rail Body */}
      {effectiveState === "Empty" ? (
        <div
          data-testid="agent-rail-empty"
          className="flex flex-1 flex-col items-center justify-center gap-2 p-6 text-center"
        >
          <div className="flex size-10 items-center justify-center rounded-lg bg-surface-raised border border-border-subtle text-text-tertiary">
            <Bot className="size-5" aria-hidden="true" />
          </div>
          <p className="text-body-13 text-text-secondary">No active agent session</p>
          <p className="text-caption-11 text-text-tertiary">
            Open Agent Hub to configure and launch an agent swarm.
          </p>
          {onOpenAgentHub ? (
            <button
              type="button"
              data-testid="rail-open-agenthub-button"
              onClick={onOpenAgentHub}
              className="mt-2 inline-flex h-7 items-center gap-1 rounded-control bg-action-primary px-3 text-label-12 text-action-primary-foreground transition-transform active:scale-95 hover:opacity-90 cursor-pointer"
            >
              <Sparkles className="size-3 shrink-0" aria-hidden="true" />
              <span>Open Agent Hub</span>
            </button>
          ) : null}
        </div>
      ) : (
        <div
          data-testid="agent-rail-conversation"
          className="flex flex-1 flex-col min-h-0 min-w-0"
        >
          {/* Inline Permission Request if pending */}
          {isPendingForActiveSession && pendingPermission ? (
            <div
              data-testid="rail-inline-permission"
              className="px-3 pt-2 shrink-0"
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
              vendor={vendor}
              loading={false}
            />
          </div>

          {errorMessage ? (
            <p className="px-3 pb-1 text-caption-11 text-status-danger">{errorMessage}</p>
          ) : null}

          {/* Rail Composer at bottom */}
          <div className="shrink-0 p-3 pt-1">
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
            />
          </div>
        </div>
      )}
    </section>
  );
}
