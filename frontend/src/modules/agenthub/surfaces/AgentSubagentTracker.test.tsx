import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";

import type { TranscriptItem } from "../types";
import { AgentSubagentTracker } from "./AgentSubagentTracker";

const items: TranscriptItem[] = [
  { id: "s1", kind: "subagent_spawned", subagent: { id: "a1", name: "Write the handler" } },
  { id: "t1", kind: "tool_call", origin: "a1", toolCall: { id: "t1", kind: "read", title: "Read kong/handler.lua" } },
  { id: "s2", kind: "subagent_spawned", subagent: { id: "a2", name: "Vectors", state: "completed" } },
];

describe("AgentSubagentTracker", () => {
  it("names each running subagent with the step it is on", () => {
    const html = renderToString(<AgentSubagentTracker items={items} live />);
    expect(html).toContain("1 subagent running");
    expect(html).toContain("Write the handler");
    expect(html).toContain("Read handler.lua");
    expect(html).toContain("1 step");
    // A finished one is the transcript's to show, not this.
    expect(html).not.toContain("Vectors");
  });

  it("draws nothing once the turn is over, whatever the last state said", () => {
    // A subagent cut off with its session never gets a state update.
    expect(renderToString(<AgentSubagentTracker items={items} live={false} />)).toBe("");
    expect(renderToString(<AgentSubagentTracker items={[]} live />)).toBe("");
  });
});
