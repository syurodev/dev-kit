import { describe, expect, it } from "bun:test";
import { renderToString as renderRaw } from "react-dom/server";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { TooltipProvider } from "@/components/ui/tooltip";
import type { TranscriptItem } from "../types";
import { TranscriptStream } from "./TranscriptStream";

const renderToString = (node: React.ReactNode) =>
  renderRaw(
    <QueryClientProvider client={new QueryClient()}>
      <TooltipProvider>{node}</TooltipProvider>
    </QueryClientProvider>,
  );

const items: TranscriptItem[] = [
  { id: "u1", kind: "user_message_chunk", text: "which topics?" },
  {
    id: "t1",
    kind: "tool_call",
    toolCall: {
      id: "t1",
      name: "mcp__devkit__kafka_listTopics",
      status: "completed",
      content: [{ type: "content", content: { type: "text", text: JSON.stringify({ result: [{ name: "orders.created" }] }) } }],
    },
  },
  { id: "a1", kind: "agent_message_chunk", text: "Orders go to `orders.created`." },
];

describe("TranscriptStream: tables and topics", () => {
  it("hands the session's topics to its finished prose", () => {
    const html = renderToString(
      <TranscriptStream items={items} streamKey="s1" refContext={{ sessionId: "s1", workspaceId: "ws", projectPath: "/p" }} />,
    );
    expect(html).toContain('data-testid="agent-entity-ref"');
    expect(html).toContain('data-kind="topic"');
  });
});
