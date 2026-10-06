import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import type { AgentSession } from "@/bridge/agenthub";
import { AgentSwarmStrip, railTabSessions } from "./AgentSwarmStrip";

function session(id: string, patch: Partial<AgentSession> = {}): AgentSession {
  return {
    id,
    agentType: "claude",
    projectPath: "/repo",
    status: "idle",
    parentSessionId: null,
    depth: 0,
    title: `Title ${id}`,
    acpSessionId: "",
    ...patch,
  };
}

describe("AgentSwarmStrip", () => {
  const sessions = [
    session("a"),
    session("b", { status: "running" }),
    session("c", { status: "awaiting_permission" }),
    session("d", { status: "closed" }),
    session("e", { parentSessionId: "a", depth: 1 }),
  ];

  it("tabs every open root session, not just the first two", () => {
    expect(railTabSessions(sessions, "a").map((s) => s.id)).toEqual(["a", "b", "c"]);
  });

  it("always tabs the selected session, even a closed one or a subagent", () => {
    expect(railTabSessions(sessions, "d").map((s) => s.id)).toEqual(["a", "b", "c", "d"]);
    expect(railTabSessions(sessions, "e").map((s) => s.id)).toEqual(["a", "b", "c", "e"]);
  });

  it("renders tabs that truncate, a waiting jump and a menu trigger instead of a dead +N", () => {
    const html = renderToString(<AgentSwarmStrip sessions={sessions} activeSessionId="d" />);
    expect(html).toContain('role="tablist"');
    expect(html.match(/data-testid="agent-swarm-chip"/g)?.length).toBe(4);
    expect(html).toContain('aria-selected="true"');
    expect(html).toContain("truncate");
    expect(html).toContain('data-testid="agent-swarm-waiting"');
    expect(html).toContain("1 waiting for you");
    expect(html).toMatch(/<button[^>]*data-testid="agent-swarm-more"/);
  });

  it("offers no waiting jump when nothing waits", () => {
    const html = renderToString(<AgentSwarmStrip sessions={[session("a"), session("b")]} activeSessionId="a" />);
    expect(html).not.toContain('data-testid="agent-swarm-waiting"');
  });
});
