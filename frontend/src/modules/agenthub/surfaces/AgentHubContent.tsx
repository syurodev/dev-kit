import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import type { AgentSession } from "@/bridge/agenthub";
import type { AvailableCommand, TranscriptItem, Usage } from "../types";
import { TranscriptStream } from "./TranscriptStream";
import { AgentComposer } from "./AgentComposer";
import { AgentSessionError, type SessionErrorCause } from "./AgentSessionError";
import { AgentState, type AgentStatus } from "./AgentState";
import { VendorIcon } from "./VendorIcon";

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
  onCloseSession?: () => void;
  onNewSession?: () => void;
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
 * Three states, one header, and the header is the whole identity of the agent
 * on one line: vendor mark, name, live state, then the lineage in the quiet
 * tier — "depth 2 · parent auth-refactor · ~/acme/billing-service" is the
 * boundary this agent may act within, which is the thing you check before you
 * let it carry on. The previous implementation had a chevron breadcrumb, a
 * vendor pill, a depth pill, a folder icon and a fourth colour-coded status
 * pill; the pill was a third status vocabulary next to AgentState, and the
 * lineage was scattered across four chips instead of read as one sentence.
 *
 * Empty has no header and no composer — there is no agent to name and nothing
 * to send to. Error keeps the header but drops "Cancel turn": the turn is
 * already over.
 */

/** Same mapping as AgentRow: the bridge says awaiting_permission, Figma says awaiting. */
function toAgentStatus(status: string | undefined): AgentStatus {
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
  onCloseSession,
  onNewSession,
  className,
}: AgentHubContentProps) {
  const effectiveState: AgentHubContentState = explicitState
    ? explicitState
    : !session
      ? "Empty"
      : session.status === "error"
        ? "Error"
        : "Conversation";

  const displayTitle =
    session?.title || (session ? `Session ${session.id.slice(0, 8)}` : "");
  const vendor = session?.agentType || "claude";

  // One sentence, not four chips. Parent is omitted rather than shown empty —
  // a root agent has no parent, and "parent —" reads as a missing value.
  const lineage = session
    ? [
        `depth ${session.depth}`,
        parentSession
          ? `parent ${parentSession.title || parentSession.id.slice(0, 8)}`
          : null,
        session.projectPath || ".",
      ]
        .filter(Boolean)
        .join(" · ")
    : "";

  return (
    <main
      role="main"
      aria-label="Agent Hub Content"
      data-testid="agent-hub-content"
      data-state={effectiveState}
      className={cn(
        "flex size-full min-h-0 min-w-0 flex-col gap-2 overflow-hidden px-4 pt-3 pb-4",
        className,
      )}
    >
      {effectiveState !== "Empty" && session ? (
        <header
          data-testid="agent-hub-content-header"
          className="flex w-full shrink-0 items-center gap-3 px-5 py-4 select-none"
        >
          <VendorIcon vendor={vendor} className="size-4 text-icon-secondary" />
          <h2 className="shrink-0 text-title-16 text-text-primary">{displayTitle}</h2>
          <AgentState status={toAgentStatus(session.status)} />
          <p className="min-w-0 flex-1 truncate text-body-12 text-text-tertiary">
            {lineage}
          </p>
          {effectiveState === "Conversation" && streaming ? (
            <Button
              type="button"
              variant="outline"
              size="sm"
              data-testid="content-cancel-turn"
              onClick={onCancel}
              className="h-8 border-border-subtle px-3 text-text-secondary"
            >
              Cancel turn
            </Button>
          ) : null}
          {onCloseSession ? (
            <Button
              type="button"
              variant="outline"
              size="sm"
              data-testid="content-close-session"
              onClick={onCloseSession}
              className="h-8 border-border-subtle px-3 text-text-secondary"
            >
              Close
            </Button>
          ) : null}
        </header>
      ) : null}

      {effectiveState === "Empty" ? (
        <div
          data-testid="agent-hub-content-empty"
          className="flex min-h-0 w-full flex-1 flex-col items-center justify-center gap-3 p-5 text-center"
        >
          <p className="text-title-16 text-text-secondary">No agent running</p>
          {/* Figma's note: Empty is the first thing a new user meets, so it
              explains what a session IS. The old copy said only where to click. */}
          <p className="w-[420px] max-w-full text-body-12 text-text-tertiary">
            Each session gets its own sandbox, its own working directory, and its own
            connection to DevKit&apos;s tools. Sessions started by an agent appear as
            children of it.
          </p>
          {onNewSession ? (
            <Button
              type="button"
              size="sm"
              data-testid="empty-new-session-button"
              onClick={onNewSession}
              className="h-8 px-4"
            >
              New session
            </Button>
          ) : null}
        </div>
      ) : effectiveState === "Error" ? (
        // Not centred: Figma keeps the transcript that led here above the card,
        // because the failed tool call is the evidence for the error.
        <div
          data-testid="agent-hub-content-error"
          className="flex min-h-0 w-full flex-1 flex-col gap-5"
        >
          <div className="min-h-0 flex-1">
            <TranscriptStream items={items} vendor={vendor} agentName={displayTitle} />
          </div>
          <div className="w-full shrink-0 px-5 pb-5">
            <AgentSessionError
              cause={errorCause}
              agentName={displayTitle || "Agent"}
              reason={errorMessage}
              onRetry={onRetrySession}
              onClose={onCloseSession}
              className="w-full max-w-none"
            />
          </div>
        </div>
      ) : (
        <div
          data-testid="agent-hub-content-conversation"
          className="flex min-h-0 w-full min-w-0 flex-1 flex-col"
        >
          <div className="min-h-0 flex-1 overflow-hidden">
            <TranscriptStream
              items={items}
              vendor={vendor}
              agentName={displayTitle}
              loading={false}
            />
          </div>

          <div className="flex w-full shrink-0 items-center gap-3 px-5">
            <AgentComposer
              size="wide"
              value={promptValue}
              onChange={onPromptChange}
              onSend={onSend}
              onCancel={onCancel}
              streaming={streaming}
              usage={usage}
              vendor={vendor}
              agentName={displayTitle}
              commands={commands}
              className="min-w-0 flex-1"
            />
          </div>
        </div>
      )}
    </main>
  );
}
