import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import type { TranscriptItem } from "./types";
import { SubagentPanel, isSubagentRunning, sessionRunningSubagents } from "./subagents";

const spawned = (id: string, name: string): TranscriptItem => ({ id: `spawn-${id}`, kind: "subagent_spawned", subagent: { id, name } });
const step = (id: string, origin: string): TranscriptItem => ({
  id,
  kind: "tool_call",
  origin,
  toolCall: { id, kind: "read", title: "Read a.go", status: "completed" },
});

const items: TranscriptItem[] = [spawned("a1", "Review whitelist"), step("r1", "a1"), spawned("a2", "Java fixes")];

describe("running subagents", () => {
  it("are the tracked ones still running, after the lead agent's turn ended to wait for them", () => {
    const running = sessionRunningSubagents(items, [{ id: "a1", name: "Review whitelist" }, { id: "a2", state: "completed" }], false);
    expect(running.map((r) => r.subagent.id)).toEqual(["a1"]);
    // What it has done so far comes with it.
    expect(running[0].activity.map((row) => row.id)).toEqual(["r1"]);
  });

  it("add what the transcript shows while a turn runs, but never one tracked as ended", () => {
    const running = sessionRunningSubagents(items, [{ id: "a2", state: "failed" }], true);
    expect(running.map((r) => r.subagent.id)).toEqual(["a1"]);
  });

  it("trust nothing untracked once the turn is over", () => {
    expect(sessionRunningSubagents(items, undefined, false)).toEqual([]);
  });

  it("read no state as still working", () => {
    expect([undefined, "", "running", "completed", "failed"].map(isSubagentRunning)).toEqual([true, true, true, false, false]);
  });
});

describe("SubagentPanel", () => {
  it("names the subagent and says when there is nothing to show yet", () => {
    const html = renderToString(<SubagentPanel sessionId="s-none" subagentId="a9" />);
    expect(html).toContain('data-testid="subagent-panel"');
    expect(html).toContain("Subagent");
    expect(html).toContain("Starting…");
  });
});
