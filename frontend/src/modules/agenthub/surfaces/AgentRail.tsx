import { Bot } from "lucide-react";
import { cn } from "@/lib/utils";
import type { AgentSession, PendingPermission } from "@/bridge/agenthub";
import type { AvailableCommand, TranscriptItem, Usage } from "../types";
import { TranscriptStream } from "./TranscriptStream";
import { AgentComposer } from "./AgentComposer";
import { AgentPermissionRequest } from "./AgentPermissionRequest";
import { AgentState, type AgentStatus } from "./AgentState";
import { AgentSwarmStrip } from "./AgentSwarmStrip";

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
 * Four bands: the swarm strip, a header naming the agent, the transcript, then
 * the rail composer. The rail itself paints nothing — panel-surface behind it
 * is the ground, so a fill here would be a second opaque layer on an opaque
 * layer. Padding is asymmetric on purpose: pl-3 pr-5, leaving the right margin
 * the shell's scroll ruler lives in.
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
        // No fill: panel-surface behind the rail is the ground. pl-3 pr-5
        // leaves the right margin the shell's scroll ruler occupies.
        "flex h-full w-[320px] flex-col overflow-hidden pt-0 pr-5 pb-3 pl-3 select-none",
        className,
      )}
    >
      {sessions.length > 1 ? (
        <AgentSwarmStrip
          sessions={sessions}
          activeSessionId={activeSession?.id}
          onSelectSession={onSelectSession}
        />
      ) : null}

      {/* Figma 913:31: vendor mark, name, live state, then one quiet line
          carrying depth and cwd — the two facts that say what this agent may
          touch. The header used to show a vendor text chip and a colour-coded
          status pill, a third status vocabulary next to AgentState. */}
      {activeSession ? (
        <header
          data-testid="agent-rail-header"
          className="flex w-full shrink-0 flex-col gap-1 px-2 py-3"
        >
          <div className="flex w-full items-center gap-2">
            <Bot className="size-3.5 shrink-0 text-icon-secondary" aria-hidden="true" />
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
        <div
          data-testid="agent-rail-empty"
          className="flex flex-1 flex-col items-center justify-center gap-2 px-3 py-3 pr-5 text-center"
        >
          <p className="text-body-13-medium text-text-secondary">
            No agents running
          </p>
          <p className="w-[232px] text-caption-11 text-text-tertiary">
            Open Agent Hub to start one — it needs a vendor and a working
            directory.
          </p>
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
