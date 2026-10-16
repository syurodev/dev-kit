/**
 * What `agenthub.sendMessage` answers (internal/agenthub/peer.go:
 * SendMessageResult), read into what its card draws. Pure, no React; null for
 * anything unreadable, so the transcript falls back to the generic block.
 */

import { parseToolOutput } from "@/workbench/toolResultViews";

export type MessageDelivery = "started" | "steered" | "queued" | "refused";

export type SentMessage = {
  messageId: string;
  delivery: MessageDelivery;
  /** Why it was refused, or why it waits. */
  reason: string;
  /** A refusal's code (not_found, loop, rate, …). */
  code: string;
  /** The recipient, when the sender may know it. */
  sessionId: string;
  title: string;
  hop: number;
};

const DELIVERIES = new Set<string>(["started", "steered", "queued", "refused"]);

export function parseSentMessage(rawOutput: unknown): SentMessage | null {
  const value = parseToolOutput(rawOutput);
  if (value === null || typeof value !== "object" || Array.isArray(value)) return null;
  const record = value as Record<string, unknown>;
  if (typeof record.delivery !== "string" || !DELIVERIES.has(record.delivery)) return null;
  const text = (key: string) => (typeof record[key] === "string" ? (record[key] as string) : "");
  return {
    messageId: text("messageId"),
    delivery: record.delivery as MessageDelivery,
    reason: text("reason"),
    code: text("code"),
    sessionId: text("sessionId"),
    title: text("title"),
    hop: typeof record.hop === "number" ? record.hop : 0,
  };
}

/** One line on what became of the message, in the sender's terms. */
export function deliveryLine(message: SentMessage): string {
  switch (message.delivery) {
    case "started":
      return "It was idle and started a turn on the message";
    case "steered":
      return "It was working; the message went into that turn";
    case "queued":
      return "Waiting for it to be able to take the message";
    default:
      return "Not delivered";
  }
}

/** What the card puts beside its title. */
export function deliveryLabel(delivery: MessageDelivery): string {
  return delivery === "refused" ? "Not delivered" : delivery[0].toUpperCase() + delivery.slice(1);
}
