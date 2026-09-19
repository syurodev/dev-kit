import { describe, expect, it } from "bun:test";
import { buildGroups, sessionLabel } from "./paletteSearch";
import type { AgentSession } from "@/bridge/agenthub";

function session(overrides: Partial<AgentSession> = {}): AgentSession {
  return {
    id: "abcdef1234567890",
    agentType: "claude",
    projectPath: "/Users/me/dev/acme-refunds",
    status: "idle",
    parentSessionId: null,
    depth: 0,
    title: null,
    acpSessionId: "",
    ...overrides,
  };
}

describe("sessionLabel", () => {
  it("prefers a trimmed title", () => {
    expect(sessionLabel(session({ title: "  Refund flow  " }))).toBe("Refund flow");
  });

  it("falls back to agent type and a short id when there is no title", () => {
    expect(sessionLabel(session({ title: null }))).toBe("claude · abcdef12");
  });
});

describe("agenthub buildGroups", () => {
  it("returns every session for an empty query, up to the limit", () => {
    const sessions = [session({ id: "s1" }), session({ id: "s2", title: "Refund flow" })];
    const groups = buildGroups("", sessions, { openSession: () => undefined });
    expect(groups[0].heading).toBe("SESSIONS");
    expect(groups[0].items.map((i) => i.id)).toEqual(["session:s1", "session:s2"]);
  });

  it("shows the project folder and status as the subtitle", () => {
    const groups = buildGroups(
      "",
      [session({ id: "s1", projectPath: "/Users/me/dev/acme-refunds", status: "running" })],
      { openSession: () => undefined },
    );
    expect(groups[0].items[0].subtitle).toBe("acme-refunds · running");
  });

  it("matches by title, agent type or project folder", () => {
    const s = session({ id: "s1", title: "Refund flow", projectPath: "/Users/me/dev/acme-refunds" });
    expect(buildGroups("refund", [s], { openSession: () => undefined })).toHaveLength(1);
    expect(buildGroups("claude", [s], { openSession: () => undefined })).toHaveLength(1);
    expect(buildGroups("acme-refunds", [s], { openSession: () => undefined })).toHaveLength(1);
    expect(buildGroups("no-such-thing", [s], { openSession: () => undefined })).toEqual([]);
  });

  it("runs the caller's action with the picked session", () => {
    const opened: AgentSession[] = [];
    const s = session();
    buildGroups("", [s], { openSession: (session) => opened.push(session) })[0].items[0].run();
    expect(opened).toEqual([s]);
  });
});
