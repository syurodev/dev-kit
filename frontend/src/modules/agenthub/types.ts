import type { UpdateKind } from "../../../bindings/devkit/internal/agenthub/models";

/**
 * ContentBlock is one ACP displayable unit. Only `text` carries meaning for
 * every vendor; the rest are kept as-is so the UI can decide what it can show.
 * Mirrors internal/agenthub/update.go: ContentBlock
 */
export type ContentBlock = {
  type: string;
  text?: string;
  mimeType?: string;
  data?: string;
  uri?: string;
  name?: string;
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
  usage?: Usage | null;
  messageId?: string;
  raw?: unknown;
};

/**
 * TranscriptItem is one row in the rendered conversation stream.
 */
export type TranscriptItem = {
  id: string;
  kind: UpdateKind | string;
  text?: string;
  messageId?: string;
  toolCallId?: string;
  toolCall?: ToolCall;
  plan?: PlanEntry[];
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
};

/**
 * Action type for transcriptReducer.
 */
export type TranscriptAction =
  | { type: "init"; entries: any[] }
  | { type: "update"; update: SessionUpdate }
  | { type: "reset" };
