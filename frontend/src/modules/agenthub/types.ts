import type { UpdateKind } from "../../../bindings/devkit/internal/agenthub/models";

/**
 * ContentBlock is one ACP displayable unit. Only `text` carries meaning for
 * every vendor; the rest are kept as-is so the UI can decide what it can show.
 * Mirrors internal/acp/types_v1.go: ContentBlock — the Go side aliases it
 * rather than copying it, so this is the one shape to keep in step.
 */
export type ContentBlock = {
  type: string;
  text?: string;
  mimeType?: string;
  data?: string;
  uri?: string;
  name?: string;
  /** Set when type === "resource": the contents travel with the block. */
  resource?: {
    uri: string;
    text?: string;
    blob?: string;
    mimeType?: string;
  };
};

/**
 * ToolCallLocation is a file the tool call touched.
 * Mirrors internal/agenthub/update.go: ToolCallLocation
 */
export type ToolCallLocation = {
  path: string;
  line?: number;
};

/**
 * ToolCallContent is one of ACP's three tool-call content shapes.
 * Mirrors internal/agenthub/update.go: ToolCallContent
 */
export type ToolCallContent = {
  type: "content" | "diff" | "terminal" | string;
  content?: ContentBlock;
  path?: string;
  oldText?: string | null;
  newText?: string;
  terminalId?: string;
  /** Set by the page read: text cut to a preview, or a diff too large to
   * preview reduced to its path. */
  truncated?: boolean;
};

/**
 * ToolCall is the normalized form of a tool_call / tool_call_update payload.
 * Mirrors internal/agenthub/update.go: ToolCall
 */
export type ToolCall = {
  id: string;
  title?: string;
  kind?: string;
  /** The tool itself ("Read", "Agent", "compact"); tells a subagent from a compaction, which share the `think` kind. */
  name?: string;
  status?: string;
  content?: ToolCallContent[];
  locations?: ToolCallLocation[];
  rawInput?: unknown;
  rawOutput?: unknown;
  /** Set by the page read when oversized raw fields were cut to a preview;
 * agentHubToolCallDetail serves the whole thing. */
  truncated?: boolean;
};

/**
 * PlanEntry is one step of a `plan` update.
 * Mirrors internal/agenthub/update.go: PlanEntry
 */
export type PlanEntry = {
  content: string;
  priority?: string;
  status?: string;
};

/**
 * AvailableCommand is one entry of an available_commands_update.
 * Mirrors internal/agenthub/update.go: AvailableCommand
 */
export type AvailableCommand = {
  name: string;
  description?: string;
  input?: { hint?: string } | null;
};

/**
 * Usage is the context/cost snapshot carried by a usage_update.
 * Mirrors internal/agenthub/update.go: Usage
 */
export type Usage = {
  used: number;
  size: number;
  amount: number;
  currency?: string;
  /** The subscription's own ceiling, which the context window says nothing about. */
  rateLimit?: RateLimit | null;
  /**
   * What only some vendors report. Absent means "this vendor does not say",
   * never zero: the summary draws a section for them only when they are here.
   */
  tokens?: TokenBreakdown | null;
  limits?: LimitWindow[] | null;
};

/** One tally of tokens. Mirrors internal/agenthub/update.go: TokenCounts */
export type TokenCounts = { total: number; input: number; cached: number; output: number; reasoning: number };

/** The session's tokens split, and the last request's. Mirrors: TokenBreakdown */
export type TokenBreakdown = { session: TokenCounts; lastRequest: TokenCounts };

/**
 * One ceiling a plan puts on the account (a window that resets, not the context
 * window). resetsAt is Unix seconds. Mirrors: LimitWindow
 */
export type LimitWindow = {
  label: string;
  usedPercent: number;
  resetsAt?: number;
  /** "ok", "warning" or "reached". */
  status?: string;
};

/**
 * An MCP server the vendor started for the session. state is "starting",
 * "ready", "failed" or "cancelled". Mirrors: Connector
 */
export type Connector = { name: string; state: string; error?: string; needsLogin?: boolean };

/**
 * What a Claude subscription has left, from a usage_update's _meta.
 * Mirrors internal/agenthub/update.go: RateLimit
 */
export type RateLimit = {
  /** "allowed", "allowed_warning" (close to the ceiling) or "rejected". */
  status: string;
  /** Unix seconds; absent when the vendor did not say. */
  resetsAt?: number;
  limitType?: string;
  /** Percentage of the window used. */
  utilization?: number;
};

/**
 * A session-scoped problem the vendor reports: a sign-in that expired, a usage
 * limit reached, a provider outage. Actions are its own hints for the way out.
 * Mirrors internal/agenthub/update.go: SessionFailure
 */
export type SessionFailure = {
  id: string;
  revision: number;
  category: string;
  /** "error" stops the session's work; "warning" is a notice. */
  severity: string;
  title: string;
  details?: string;
  reason?: string;
  /** "login" | "retry" | "new_session", as the vendor names them. */
  actions?: string[];
  /**
   * Set only on the notice DevKit derives from a ProviderRetry (never sent by
   * Go): when the agent will try again. The banner counts down to it, and it
   * has no Dismiss — the next attempt or the turn ending replaces it.
   */
  retryAt?: string;
};

/**
 * The model provider failed and the agent is waiting to try again (opencode's
 * retry notice). Transient: the next attempt replaces it and null clears it.
 * Mirrors internal/agenthub/update.go: ProviderRetry
 */
export type ProviderRetry = {
  attempt: number;
  /** RFC 3339, the vendor's own time. */
  nextRetryAt?: string;
  message?: string;
  /** The provider's HTTP status, when it gave one. */
  status?: number;
  type?: string;
};

/**
 * What a session is working towards, from the goal extension.
 * Mirrors internal/agenthub/update.go: SessionGoal
 */
export type SessionGoal = {
  objective: string;
  status?: string;
  iterations?: number;
  lastReason?: string;
  createdAt?: string;
};

/**
 * SessionUpdate is the hub-owned view of one session/update notification.
 * Raw is always populated, including for UpdateUnknown.
 * Mirrors internal/agenthub/update.go: SessionUpdate
 */
export type SessionUpdate = {
  kind: UpdateKind | string;
  content?: ContentBlock | null;
  toolCall?: ToolCall | null;
  plan?: PlanEntry[];
  commands?: AvailableCommand[];
  mode?: string;
  /**
   * The agent's whole mode list, on a current_mode_update the hub makes from
   * the session/new or session/load answer. The agent's own update carries
   * only `mode`, and leaves the list as it was.
   */
  availableModes?: SessionMode[];
  /** Set on subagent_spawned and subagent_state_update. */
  subagent?: Subagent | null;
  /** Set on the three async_task_* kinds. */
  asyncTask?: AsyncTask | null;
  /** The whole set of session settings, on config_option_update. */
  configOptions?: ConfigOption[];
  /**
   * The vendor's own name for the session, on session_info_update. Empty on
   * the goal-carrying shape of the same kind — one kind, two payloads.
   */
  title?: string;
  usage?: Usage | null;
  /** Both ride on a session_info_update's _meta; see their types. */
  failure?: SessionFailure | null;
  goal?: SessionGoal | null;
  /** The goal was removed — which `goal: null` alone cannot say, since most
   * session_info_updates carry no goal at all. */
  goalCleared?: boolean;
  /** On a provider_retry_update: the outage being waited out; null/absent clears it. */
  retry?: ProviderRetry | null;
  /** The whole MCP server list, on a devkit_connectors_update. */
  connectors?: Connector[];
  messageId?: string;
  /** The subagent session an update came from; empty for the session's own. */
  origin?: string;
  /** What a prompt carried besides text, on DevKit's echo of it. */
  attachments?: PromptAttachment[];
  /** Who wrote a user message that the person did not type. */
  source?: MessageSource;
  raw?: unknown;
};

/**
 * Where a user message came from when it was not the person at the composer:
 * another Agent Hub session, or the person's terminal CLI. Mirrors
 * peer.go: MessageSource. The label is stored with the row, so a reload draws
 * the same thing the live message did.
 */
export type MessageSource = {
  kind: "session" | "terminal" | string;
  sessionId?: string;
  agentType?: string;
  /** The sender's title when the message was sent; the live title wins. */
  name?: string;
  messageId?: string;
  replyTo?: string;
  /** Which step of the exchange this is, from 1. */
  hop: number;
};

/** One file, image, PDF document or ref a prompt carried, by name. Mirrors update.go: PromptAttachment. */
export type PromptAttachment = {
  name: string;
  kind: "file" | "image" | "document" | "ref" | string;
  /** The devkit:// EntityRef URI, set only when kind is "ref". */
  uri?: string;
  /** Set when kind is "document": the handle the agent reads it with (doc1) and its page count. */
  documentId?: string;
  pages?: number;
};

/**
 * Subagent is an agent the vendor ran inside its own process.
 *
 * Not a DevKit session: nothing here created it, the composer cannot address
 * it, and closing it is not ours to do. Name and task arrive on spawn; the
 * state update carries only the id and the new state, so the two have to be
 * kept together by id rather than read one at a time.
 */
export type Subagent = {
  id: string;
  name?: string;
  task?: string;
  state?: string;
};

/**
 * AsyncTask is work the vendor runs in the background, past the end of the turn
 * that started it.
 *
 * Like Subagent, no single message is complete: name, taskType, showInTranscript
 * and canStop come with the spawn, state with the state update, usage and
 * lastToolName with progress. Keep them together by id.
 * Mirrors internal/agenthub/update.go: AsyncTask
 */
export type AsyncTask = {
  id: string;
  name?: string;
  taskType?: string;
  description?: string;
  summary?: string;
  state?: string;
  lastToolName?: string;
  outputFilePath?: string;
  toolCallId?: string;
  showInTranscript?: boolean;
  canStop?: boolean;
  usage?: AsyncTaskUsage | null;
};

/**
 * AsyncTaskUsage is what a background task has spent so far. Not the session's
 * usage: that one counts context and money, this one counts the task.
 * Mirrors internal/agenthub/update.go: AsyncTaskUsage
 */
export type AsyncTaskUsage = {
  totalTokens: number;
  toolUses: number;
  durationMs: number;
};

/**
 * ConfigOption is one per-session setting — model, effort, fast mode.
 *
 * currentValue follows type: a value id on a "select", a boolean on a
 * "boolean". Options arrive flat even when the agent grouped them, each value
 * keeping the group it came from.
 * Mirrors internal/agenthub/update.go: ConfigOption
 */
export type ConfigOption = {
  id: string;
  name?: string;
  description?: string;
  category?: string;
  type?: string;
  currentValue?: string | boolean;
  defaultValue?: string;
  options?: ConfigSelectOption[];
};

/** One mode the agent can run in. Mirrors internal/acp/types_v1.go: SessionMode */
export type SessionMode = {
  id: string;
  name: string;
  description?: string;
};

/** Mirrors internal/agenthub/update.go: ConfigSelectOption */
export type ConfigSelectOption = {
  value: string;
  name?: string;
  description?: string;
  group?: string;
  defaultEffort?: string;
  supportedEfforts?: ConfigSelectOption[];
};

/**
 * TranscriptItem is one row in the rendered conversation stream.
 */
export type TranscriptItem = {
  id: string;
  kind: UpdateKind | string;
  /** The persisted row's seq, when the item came from a page read. The walk
   * backwards keys off it; live items have none yet. */
  seq?: number;
  /** The session a page read loaded this item from — the handle a truncated
   * tool call needs to fetch its full output. */
  sessionId?: string;
  text?: string;
  messageId?: string;
  /** Set when the row came from a subagent rather than the session itself. */
  origin?: string;
  /** A prompt's attachments, by name. */
  attachments?: PromptAttachment[];
  /** Set on a user message another session (or the terminal) sent. */
  source?: MessageSource;
  toolCallId?: string;
  toolCall?: ToolCall;
  plan?: PlanEntry[];
  subagent?: Subagent;
  /** One row per background task, merged from the three async_task_* kinds. */
  asyncTask?: AsyncTask;
  raw?: unknown;
  createdAt?: string;
  updatedAt?: string;
};

/**
 * TranscriptModel is the session-level render state.
 */
export type TranscriptModel = {
  items: TranscriptItem[];
  usage: Usage | null;
  /**
   * The vendor's last word on the session as a whole: a failure it reported
   * (sign-in expired, usage limit, provider outage) and the objective it is
   * working towards. Both arrive on session_info_update and are session state,
   * not transcript rows — the latest one replaces the last.
   */
  failure: SessionFailure | null;
  goal: SessionGoal | null;
  /**
   * The provider outage the agent is retrying through right now. Session
   * state like the failure, but cleared explicitly (the next attempt's null,
   * the turn ending) — a new prompt does not clear it, because the single row
   * the recorder keeps sits above that prompt on a reload.
   */
  retry: ProviderRetry | null;
  /** The MCP servers the vendor started, for a vendor that reports them; empty otherwise. */
  connectors: Connector[];
  commands: AvailableCommand[];
  mode: string | null;
  /** The modes the agent advertised; empty when it listed none. */
  availableModes: SessionMode[];
  configOptions: ConfigOption[];
  /** O(1) lookup for tool_call_update; maintained with the item snapshot. */
  toolCallIndex: Map<string, number>;
};

/**
 * Where a session's transcript stands in the backwards page walk.
 * hasMore false means the top of the conversation is on screen — until then
 * the transcript says so rather than silently ending.
 */
export type TranscriptPaging = {
  hasMore: boolean;
  loadingOlder: boolean;
};

/**
 * Action type for transcriptReducer.
 */
export type TranscriptAction =
  | { type: "init"; entries: any[] }
  | { type: "update"; update: SessionUpdate }
  | { type: "reset" };
