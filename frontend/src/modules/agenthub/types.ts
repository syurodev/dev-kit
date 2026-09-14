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
};

/**
 * ToolCall is the normalized form of a tool_call / tool_call_update payload.
 * Mirrors internal/agenthub/update.go: ToolCall
 */
export type ToolCall = {
  id: string;
  title?: string;
  kind?: string;
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
  messageId?: string;
  raw?: unknown;
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
  options?: ConfigSelectOption[];
};

/** Mirrors internal/agenthub/update.go: ConfigSelectOption */
export type ConfigSelectOption = {
  value: string;
  name?: string;
  description?: string;
  group?: string;
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
  toolCallId?: string;
  toolCall?: ToolCall;
  plan?: PlanEntry[];
  subagent?: Subagent;
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
  commands: AvailableCommand[];
  mode: string | null;
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
