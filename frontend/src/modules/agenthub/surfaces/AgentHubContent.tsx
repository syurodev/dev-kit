import * as React from "react";
import { Copy, Ellipsis, FolderOpen, Pencil, Square } from "lucide-react";
import { UpdateKind } from "../../../../bindings/devkit/internal/agenthub/models";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyTitle,
} from "@/components/ui/empty";
import type { AgentSession } from "@/bridge/agenthub";
import type { AgentImageAttachment } from "@/bridge/agenthub";
import { errorMessage as describeError } from "@/bridge/errors";
import type { QueuedAgentPrompt } from "@/stores/agentSessions";
import type { AvailableCommand, ConfigOption, SessionMode, TranscriptItem, Usage } from "../types";
import type { ContextFileItem } from "./AgentMenu";
import { TranscriptStream } from "./TranscriptStream";
import { AgentComposer } from "./AgentComposer";
import { AgentSessionError, type SessionErrorCause } from "./AgentSessionError";
import { AgentState, type AgentStatus } from "./AgentState";
import { VendorIcon } from "./VendorIcon";
import { Input } from "@/components/ui/input";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";

export type AgentHubContentState = "Conversation" | "Empty" | "Error";

/** What the rename dialog says after Save: "" once renamed, the reason when it was not. */
export async function submitRename(title: string, rename: (title: string) => void | Promise<void>): Promise<string> {
  try {
    await rename(title);
    return "";
  } catch (error) {
    return describeError(error);
  }
}

export type AgentHubContentProps = {
  state?: AgentHubContentState;
  session?: AgentSession | null;
  parentSession?: AgentSession | null;
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
  onCloseSession?: () => void;
  onRenameSession?: (title: string) => void | Promise<void>;
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
  /** Copies a string for the header's session menu — the path, the id. */
  onCopy?: (text: string) => void;
  /** Sends /compact. Offered in the context popover only when the agent lists the command. */
  onCompact?: () => void;
  /**
   * A session being started. Its folder (and parent) are known; its agent is
   * picked in the composer; nothing exists on the backend until the first
   * message is sent. Takes precedence over `session`.
   */
  draft?: { projectPath: string; parentName?: string } | null;
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
 * Three states, one header, and the header is the whole identity of the agent
 * on one line: vendor mark, name, where it works, live state, and a menu.
 *
 * Where it works is the project's own name ("billing-service"), and the parent
 * when there is one — "from auth-refactor". The line used to be
 * "depth 2 · parent auth-refactor · /Users/…/billing-service": depth is how the
 * hub counts, not something anyone chose, and the absolute path took the whole
 * row to say what the folder's name says. The full path is the menu's first
 * line, a click away, next to the action that copies it.
 *
 * Two buttons went. "Cancel turn" repeated the composer's Stop, which sits
 * under the text being written — one action, one control. "Close" was a
 * bordered button at the same weight as that, for the one action here that
 * ends a process; it lives in the menu now, as it does on the sidebar row.
 *
 * Empty has no header and no composer — there is no agent to name and nothing
 * to send to.
 */

/** The last segment of a path — the folder a developer calls the project. */
function projectLabel(path: string): string {
  return path.split("/").filter(Boolean).pop() ?? path;
}

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
  onCloseSession,
  onRenameSession,
  onNewSession,
  onOpenSettings,
  errorDetail,
  attachments = [],
  onBrowseContext,
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
  onOpenFile,
  onCopy,
  onCompact,
  draft,
  agents = [],
  agent = "claude",
  onAgentChange,
  draftNotice,
  sendDisabled = false,
  className,
}: AgentHubContentProps) {
  const [renameOpen, setRenameOpen] = React.useState(false);
  const [renameValue, setRenameValue] = React.useState("");
  const [renameError, setRenameError] = React.useState("");
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
  // The glass header's height, measured for the same reason as the composer's:
  // the body scrolls under it, and its first line should start just below it.
  const headerRef = React.useRef<HTMLDivElement>(null);
  const [headerHeight, setHeaderHeight] = React.useState(0);
  const hasHeader = Boolean(draft) || (effectiveState !== "Empty" && Boolean(session));
  React.useEffect(() => {
    const el = headerRef.current;
    if (!el || typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(([entry]) =>
      setHeaderHeight(Math.ceil(entry.borderBoxSize?.[0]?.blockSize ?? entry.contentRect.height)),
    );
    observer.observe(el);
    return () => observer.disconnect();
  }, [hasHeader]);

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
  const vendor = draft ? agent : session?.agentType || "claude";

  // Parent is omitted rather than shown empty — a root agent has no parent, and
  // "from —" reads as a missing value.
  const project = session ? projectLabel(session.projectPath) : "";
  const context = session
    ? [
        // Not when it only repeats the name. An agent is often named after the
        // folder it was opened in, and "Dev-kit-backend dev-kit-backend" says
        // one thing twice.
        project.toLowerCase() === displayTitle.toLowerCase() ? null : project,
        parentSession ? `from ${parentSession.title || parentSession.id.slice(0, 8)}` : null,
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
        // Relative, no top padding and no gap: the header floats over the body
        // rather than taking a row above it, so the body starts at the top edge
        // and pads itself by the header's measured height instead.
        // No side or bottom padding either: the transcript scrolls, and padding
        // out here put its scrollbar 16px in from the card's edge. Each body
        // pads its own content (the stream's px-6, the composer's px-6 pb-3).
        "relative flex size-full min-h-0 min-w-0 flex-col overflow-hidden",
        className,
      )}
      style={{ "--header-inset": `${headerHeight}px` } as React.CSSProperties}
    >
      {/* The header floats over the body as a glass card, the same material
          as the composer at the foot: content scrolls under both and the
          blur keeps each legible over whatever is passing. The outer layer is
          what gets measured (card plus its 12px gap) and lets the pointer
          through that gap; only the card takes clicks. */}
      {hasHeader ? (
        <div ref={headerRef} className="pointer-events-none absolute inset-x-4 top-0 z-10 pt-3">
          <div data-testid="agent-hub-header-bar" className="pointer-events-auto rounded-xl glass-float">
      {draft ? (
        <header
          data-testid="agent-hub-content-header"
          className="flex w-full shrink-0 items-center gap-2 px-6 py-2 select-none"
        >
          <VendorIcon vendor={vendor} className="size-3.5 text-icon-secondary" />
          <div className="flex min-w-0 flex-1 items-baseline gap-2">
            <h2 className="min-w-0 shrink truncate text-title-14 text-text-primary">New session</h2>
            <p
              data-testid="content-header-context"
              className="min-w-0 truncate text-body-13 text-text-tertiary"
              style={{ flexShrink: 100 }}
            >
              {[projectLabel(draft.projectPath), draft.parentName ? `from ${draft.parentName}` : null]
                .filter(Boolean)
                .join(" · ")}
            </p>
          </div>
        </header>
      ) : null}

      {!draft && effectiveState !== "Empty" && session ? (
        <header
          data-testid="agent-hub-content-header"
          className="flex w-full shrink-0 items-center gap-2 px-6 py-2 select-none"
        >
          <VendorIcon
            vendor={vendor}
            working={session.status === "running"}
            className="size-3.5 text-icon-secondary"
          />
          {/* The name gives way last: it truncates, but only after the project
              line beside it has. */}
          <div className="flex min-w-0 flex-1 items-baseline gap-2">
            <h2 className="min-w-0 shrink truncate text-title-14 text-text-primary">{displayTitle}</h2>
            {context ? (
              <p
                data-testid="content-header-context"
                className="min-w-0 truncate text-body-13 text-text-tertiary"
                style={{ flexShrink: 100 }}
              >
                {context}
              </p>
            ) : null}
          </div>
          <AgentState status={toAgentStatus(session.status)} />
          {onCopy || onCloseSession || onRenameSession ? (
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-sm"
                  aria-label="Session actions"
                  data-testid="content-session-menu"
                >
                  <Ellipsis aria-hidden="true" />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="w-64">
                <DropdownMenuLabel className="truncate">{session.projectPath || "."}</DropdownMenuLabel>
                {onCopy ? (
                  <>
                    <DropdownMenuItem icon={<FolderOpen />} onSelect={() => onCopy(session.projectPath)}>
                      Copy working directory
                    </DropdownMenuItem>
                    <DropdownMenuItem icon={<Copy />} onSelect={() => onCopy(session.id)}>
                      Copy session ID
                    </DropdownMenuItem>
                  </>
                ) : null}
                {onRenameSession ? (
                  <DropdownMenuItem icon={<Pencil />} data-testid="content-rename-session" onSelect={() => {
                    setRenameValue(displayTitle);
                    setRenameOpen(true);
                  }}>
                    Rename session
                  </DropdownMenuItem>
                ) : null}
                {onCloseSession ? (
                  <>
                    <DropdownMenuSeparator />
                    <DropdownMenuItem
                      icon={<Square />}
                      data-testid="content-close-session"
                      disabled={session.status === "closed"}
                      onSelect={onCloseSession}
                    >
                      Close session
                    </DropdownMenuItem>
                  </>
                ) : null}
              </DropdownMenuContent>
            </DropdownMenu>
          ) : null}
        </header>
      ) : null}
          </div>
        </div>
      ) : null}

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
          className={cn("relative flex min-h-0 w-full min-w-0 flex-1 flex-col", isError && "gap-6")}
          // How much of the transcript's foot the composer covers; the stream
          // pads by it so the last line of an answer scrolls clear.
          style={isError ? undefined : ({ "--composer-inset": `${composerHeight}px` } as React.CSSProperties)}
        >
          <div className="min-h-0 flex-1 overflow-hidden">
            {draft ? (
              // Nothing to show yet but where the agent will work and what it
              // may reach: the folder is the sandbox, and this is the last
              // moment before that is fixed.
              <div
                data-testid="agent-hub-draft"
                className="flex size-full flex-col items-center justify-center px-6"
                style={{
                  paddingTop: "var(--header-inset, 0px)",
                  paddingBottom: "var(--composer-inset, 0px)",
                }}
              >
                <div className="flex w-full max-w-120 flex-col gap-3">
                  <p className="text-title-14 text-text-primary">
                    {`Start a session in ${projectLabel(draft.projectPath) || draft.projectPath}`}
                  </p>
                  <p className="text-body-13 text-text-tertiary">
                    {`Pick an agent below and send your first message. It can only reach paths inside ${draft.projectPath}.`}
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
                streaming={!isError && streaming}
                onCopy={isError ? undefined : onCopy}
                // The error card replaces the composer, so there is nowhere to edit into.
                onEditPrompt={isError ? undefined : onPromptChange}
                hasMore={hasMore}
                loadingOlder={loadingOlder}
                onLoadOlder={onLoadOlder}
                onOpenFile={onOpenFile}
              />
            )}
          </div>

          {isError ? (
            <div className="w-full shrink-0 px-6 pb-6">
              <AgentSessionError
                cause={errorCause}
                agentName={displayTitle || "Agent"}
                reason={errorMessage}
                onRetry={onRetrySession}
                onOpenSettings={onOpenSettings}
                onClose={onCloseSession}
                className="w-full max-w-none"
              >
                {errorDetail}
              </AgentSessionError>
            </div>
          ) : (
          // Floats over the transcript's foot, in its reading column, so what
          // you write lines up with what you are answering. Only the composer
          // takes the pointer; the strip either side of it lets clicks through
          // to the text scrolling underneath.
          <div
            ref={composerRef}
            className="pointer-events-none absolute inset-x-0 bottom-0 flex w-full px-6 pb-3"
          >
          <div className="pointer-events-auto mx-auto flex w-full max-w-180 items-center gap-3">
            <AgentComposer
              size="wide"
              value={promptValue}
              onChange={onPromptChange}
              onSend={onSend}
              onCancel={onCancel}
              streaming={draft ? false : streaming}
              usage={usage}
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
              agentName={draft ? agent.charAt(0).toUpperCase() + agent.slice(1) : displayTitle}
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
          )}
        </div>
      )}
      <Dialog
        open={renameOpen}
        onOpenChange={(open) => {
          setRenameOpen(open);
          if (!open) setRenameError("");
        }}
      >
        <DialogContent className="sm:max-w-md">
          <DialogHeader><DialogTitle>Rename session</DialogTitle></DialogHeader>
          <form onSubmit={(event) => {
            event.preventDefault();
            const title = renameValue.trim();
            if (!title || !onRenameSession) return;
            // A refused rename keeps the dialog open with the reason, so the
            // name can be fixed and saved again.
            void submitRename(title, onRenameSession).then((reason) => {
              setRenameError(reason);
              if (!reason) setRenameOpen(false);
            });
          }}>
            <Input
              value={renameValue}
              onChange={(event) => setRenameValue(event.target.value)}
              maxLength={120}
              autoFocus
              aria-label="Session name"
              aria-invalid={Boolean(renameError)}
              aria-describedby={renameError ? "rename-session-error" : undefined}
            />
            {renameError ? (
              <p id="rename-session-error" role="alert" className="mt-2 text-caption-11 text-status-danger">
                {renameError}
              </p>
            ) : null}
            <DialogFooter className="mt-4">
              <Button
                type="button"
                variant="ghost"
                onClick={() => {
                  setRenameOpen(false);
                  setRenameError("");
                }}
              >
                Cancel
              </Button>
              <Button type="submit" disabled={!renameValue.trim()}>Save</Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </main>
  );
}
