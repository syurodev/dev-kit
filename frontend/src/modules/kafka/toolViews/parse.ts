/**
 * Shape checks for the kafka.* tool results (docs/dev-kit/tool-result-view.mdx),
 * run synchronously in the manifest before a card's chunk loads: anything
 * these do not recognise falls back to the transcript's generic block.
 *
 * Mirrors internal/kafka/broker.go (snake_case JSON). The results name no
 * profile or cluster (the tools act on the live connection), so neither does
 * a card.
 */

import { parseToolOutput } from "@/workbench/toolResultViews";

/** broker.go: Message. `key`/`timestamp` are omitempty. */
export type PeekedMessage = { offset: number; key?: string; value: string; timestamp?: number };

export type PeekResult = { topic: string; partition: number; messages: PeekedMessage[] };

export type TopicDetail = { name: string; replicationFactor: number; partitions: number[] };

export type ConsumerGroup = { groupId: string; lag: number };

export type PublishResult = { topic: string; partition: number; offset: number; timestamp?: number };

/** How long a value runs in the table before it is cut. */
export const VALUE_PREVIEW_CHARS = 120;

function isRecord(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

function isInteger(value: unknown): value is number {
  return typeof value === "number" && Number.isSafeInteger(value);
}

function inputField(rawInput: unknown, key: string): unknown {
  return isRecord(rawInput) ? rawInput[key] : undefined;
}

function message(value: unknown): PeekedMessage | null {
  if (!isRecord(value) || !isInteger(value.offset) || typeof value.value !== "string") return null;
  return {
    offset: value.offset,
    key: typeof value.key === "string" ? value.key : undefined,
    value: value.value,
    timestamp: isInteger(value.timestamp) ? value.timestamp : undefined,
  };
}

/** kafka.peekMessages: `{result:[{offset,key?,value,timestamp?}]}`; input `{topic, partition}`. */
export function parsePeekMessages(rawInput: unknown, rawOutput: unknown): PeekResult | null {
  const parsed = parseToolOutput(rawOutput);
  if (!Array.isArray(parsed)) return null;
  const messages: PeekedMessage[] = [];
  for (const item of parsed) {
    const read = message(item);
    if (!read) return null;
    messages.push(read);
  }
  const topic = inputField(rawInput, "topic");
  const partition = inputField(rawInput, "partition");
  return {
    topic: typeof topic === "string" ? topic : "",
    // Go reads a missing partition as its zero value.
    partition: isInteger(partition) ? partition : 0,
    messages,
  };
}

/** A value as one line of at most VALUE_PREVIEW_CHARS: whitespace folded, the rest cut with "…". */
export function valuePreview(value: string): string {
  const line = value.replace(/\s+/g, " ").trim();
  return line.length > VALUE_PREVIEW_CHARS ? `${line.slice(0, VALUE_PREVIEW_CHARS - 1)}…` : line;
}

/** The open row after a click on `index`: a second click on the open one closes it. */
export function toggleRow(current: number | null, index: number): number | null {
  return current === index ? null : index;
}

/** kafka.listTopics: `{result:[{name}]}`. */
export function parseTopicList(rawOutput: unknown): string[] | null {
  const parsed = parseToolOutput(rawOutput);
  if (!Array.isArray(parsed)) return null;
  const names: string[] = [];
  for (const item of parsed) {
    if (!isRecord(item) || typeof item.name !== "string") return null;
    names.push(item.name);
  }
  return names;
}

/** kafka.describeTopic: `{name, replication_factor, partitions:[{id}]}`. */
export function parseTopicDetail(rawOutput: unknown): TopicDetail | null {
  const parsed = parseToolOutput(rawOutput);
  if (!isRecord(parsed) || typeof parsed.name !== "string" || !Array.isArray(parsed.partitions)) return null;
  const partitions: number[] = [];
  for (const item of parsed.partitions) {
    if (!isRecord(item) || !isInteger(item.id)) return null;
    partitions.push(item.id);
  }
  return {
    name: parsed.name,
    replicationFactor: isInteger(parsed.replication_factor) ? parsed.replication_factor : 0,
    partitions,
  };
}

/** kafka.listConsumerGroups: `{result:[{group_id, lag?}]}` — Go leaves a zero lag out. */
export function parseConsumerGroups(rawOutput: unknown): ConsumerGroup[] | null {
  const parsed = parseToolOutput(rawOutput);
  if (!Array.isArray(parsed)) return null;
  const groups: ConsumerGroup[] = [];
  for (const item of parsed) {
    if (!isRecord(item) || typeof item.group_id !== "string") return null;
    groups.push({ groupId: item.group_id, lag: isInteger(item.lag) ? item.lag : 0 });
  }
  return groups;
}

/** kafka.publishMessage: `{partition, offset, timestamp?}`; the topic is the input's. */
export function parsePublishResult(rawInput: unknown, rawOutput: unknown): PublishResult | null {
  const parsed = parseToolOutput(rawOutput);
  if (!isRecord(parsed) || !isInteger(parsed.partition) || !isInteger(parsed.offset)) return null;
  const topic = inputField(rawInput, "topic");
  return {
    topic: typeof topic === "string" ? topic : "",
    partition: parsed.partition,
    offset: parsed.offset,
    timestamp: isInteger(parsed.timestamp) ? parsed.timestamp : undefined,
  };
}
