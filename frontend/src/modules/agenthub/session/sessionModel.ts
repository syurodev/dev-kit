import {
  isAgentApiKeyError,
  isAgentAuthRequiredError,
  isAgentNotInstalledError,
  isRuntimeRequirementError,
} from "../../../bridge/agentRuntime";
import {
  agentHubRenameSession,
  isSandboxUnavailableError,
  type AgentImageAttachment,
} from "../../../bridge/agenthub";
import type { EntityRef } from "@/workbench/intents";
import { useAgentSessionsStore } from "../../../stores/agentSessions";
import { vendorInfo } from "../vendors";

/** A session to create, and the first message that is the reason for it. */
export type CreateParams = {
  vendor: string;
  cwd: string;
  workspaceId?: string;
  parentSessionId?: string;
  prompt?: string;
  /** The draft the prompt was read from, chips and all: what goes back into the field if the first turn is refused. */
  draft?: string;
  attachments?: string[];
  images?: AgentImageAttachment[];
  /** The rest of a workspace's services — see chooseWorkspaceForDraft. */
  extraReadWritePaths?: string[];
  initialConfig?: Record<string, string>;
};
export type TurnInput = {
  sessionId: string;
  text: string;
  attachments: string[];
  images: AgentImageAttachment[];
  refs: EntityRef[];
};
/** The action that failed on sign-in, retried once the sign-in succeeds. */
export type AuthRetry = { kind: "create"; params: CreateParams } | ({ kind: "turn" } & TurnInput);

export type ShortcutKey = Pick<
  KeyboardEvent,
  "key" | "metaKey" | "ctrlKey" | "altKey" | "shiftKey" | "repeat" | "isComposing" | "defaultPrevented"
>;

/**
 * ⌘N on macOS, Ctrl+N elsewhere — not either on both: on a Mac, Ctrl+N is the
 * text field's own next-line. Stands aside for a key something else already
 * handled, an IME composition, and an open dialog or menu, where the key
 * belongs to what is open.
 */
export function isNewSessionShortcut(event: ShortcutKey, mac: boolean, overlayOpen: boolean): boolean {
  if (event.defaultPrevented || event.isComposing || event.repeat || event.altKey || event.shiftKey || overlayOpen) {
    return false;
  }
  const modifier = mac ? event.metaKey && !event.ctrlKey : event.ctrlKey && !event.metaKey;
  return modifier && event.key.toLowerCase() === "n";
}

/**
 * Whether a message should go into the turn that is running rather than behind
 * it. Text only: steering carries content blocks, and files belong to a turn of
 * their own. Not while the agent waits on a permission — that turn is parked on
 * the user, and the card asking is already on screen.
 */
export function shouldSteer(input: {
  status: string;
  text: string;
  attachments: number;
  images: number;
  refs?: number;
  busy: boolean;
  retrying: boolean;
}): boolean {
  return (
    !input.retrying &&
    !input.busy &&
    input.text.length > 0 &&
    input.attachments === 0 &&
    input.images === 0 &&
    (input.refs ?? 0) === 0 &&
    input.status === "running"
  );
}

/** A failure and its revision: the agent republishes one when it changes. */
export function failureKey(failure: { id: string; revision: number }): string {
  return `${failure.id}:${failure.revision}`;
}

/**
 * The turn Retry / Restart sends again: the one this view sent last, when it
 * was for this session — it still has its files and images — and otherwise
 * the session's last prompt as the transcript recorded it. The fallback is
 * what makes the button work after a relaunch or on a session that failed in
 * another window: the view's memory of its last turn is gone there, and
 * Restart used to fall through to the empty composer and do nothing at all.
 *
 * A recorded prompt carries its words, not its files: the transcript keeps an
 * attachment's name, not the path or bytes it was sent from. A prompt with no
 * words has nothing to resend, and there is no retry (undefined).
 */
export function retryTurnFor(
  sessionId: string,
  last: TurnInput | null,
  items: readonly { kind: string; text?: string; origin?: string }[],
): TurnInput | undefined {
  if (!sessionId) return undefined;
  if (last?.sessionId === sessionId) return last;
  for (let index = items.length - 1; index >= 0; index -= 1) {
    const item = items[index];
    // A subagent's prompt is its parent's business, not this session's turn.
    if (item.kind !== "user_message_chunk" || item.origin) continue;
    const text = item.text?.trim() ?? "";
    return text ? { sessionId, text, attachments: [], images: [], refs: [] } : undefined;
  }
  return undefined;
}

/** Renames a session. Throws, so the rename dialog stays open and says why. */
export async function renameAgentSession(
  sessionId: string,
  title: string,
  rename: (sessionId: string, title: string) => Promise<void> = agentHubRenameSession,
): Promise<void> {
  await rename(sessionId, title);
  const { sessionsById, upsertSession } = useAgentSessionsStore.getState();
  const session = sessionsById[sessionId];
  // The store's row, not the one the view rendered: its status may have moved on.
  if (session) upsertSession({ ...session, title });
}

/**
 * What a failed create turns into, for the vendor that failed — read from the
 * vendor table, so the install panel, the sign-in and the sandbox hint are
 * Codex's as much as Claude's, and an agent with none of them (the mock) gets
 * the plain error card.
 */
export function createFailure(vendor: string, error: unknown) {
  const info = vendorInfo(vendor);
  const runtime = info.runtimeId !== "" && (isAgentNotInstalledError(error) || isRuntimeRequirementError(error));
  return {
    /** The runtime whose status to re-read, or "". */
    refreshRuntime: runtime ? info.runtimeId : "",
    /** Nothing to add: the install panel already says it. */
    shownByInstallPanel: runtime && isAgentNotInstalledError(error),
    /** Show the vendor's own sign-in, and retry the create once it lands. */
    signIn: info.signIn !== "none" && isAgentAuthRequiredError(error),
    /** The chosen API key failed: the error card sends the person to Settings. */
    apiKey: isAgentApiKeyError(error),
    sandboxHint: info.runtimeId !== "" && isSandboxUnavailableError(error),
  };
}
