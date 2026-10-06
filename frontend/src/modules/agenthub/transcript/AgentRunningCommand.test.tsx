import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import type { TranscriptItem } from "../types";
import { StepGroup } from "../surfaces/AgentTurn";
import { AgentRunningCommand, formatElapsed, isRunningCommand } from "./AgentRunningCommand";

const ago = (ms: number) => new Date(Date.now() - ms).toISOString();

function command(status: string, startedMsAgo: number, rawInput: unknown = { command: "npm run build", description: "Build the app" }): TranscriptItem {
  return {
    id: `cmd-${status}-${startedMsAgo}`,
    kind: "tool_call",
    createdAt: ago(startedMsAgo),
    toolCall: { id: "c", kind: "execute", title: "npm run build", status, rawInput },
  };
}

describe("running commands", () => {
  it("formats a clock people read at a glance", () => {
    expect(formatElapsed(800)).toBe("0s");
    expect(formatElapsed(8_000)).toBe("8s");
    expect(formatElapsed(125_000)).toBe("2m 05s");
    expect(formatElapsed(3_780_000)).toBe("1h 03m");
  });

  it("are execute calls still under way", () => {
    expect(isRunningCommand(command("in_progress", 0))).toBe(true);
    expect(isRunningCommand(command("pending", 0))).toBe(true);
    expect(isRunningCommand(command("completed", 0))).toBe(false);
    expect(isRunningCommand({ id: "r", kind: "tool_call", toolCall: { id: "r", kind: "read", status: "in_progress" } })).toBe(false);
  });

  it("show the command and its clock once they have taken a while", () => {
    const html = renderToString(<AgentRunningCommand item={command("in_progress", 65_000)} />);
    expect(html).toContain('data-testid="running-command"');
    expect(html).toContain("Build the app");
    expect(html).toContain("npm run build");
    expect(html).toMatch(/1m 0[5-6]s/);
  });

  it("never flash for a quick one, and go when they finish", () => {
    expect(renderToString(<AgentRunningCommand item={command("in_progress", 500)} />)).toBe("");
    expect(renderToString(<AgentRunningCommand item={command("completed", 65_000)} />)).toBe("");
  });

  it("stay on screen under a folded run", () => {
    const read: TranscriptItem = { id: "r", kind: "tool_call", toolCall: { id: "r", kind: "read", title: "Read a.go", status: "completed" } };
    const html = renderToString(<StepGroup items={[read, command("in_progress", 30_000)]} />);
    expect(html).toContain('data-state="collapsed"');
    expect(html).toContain('data-testid="running-command"');
    expect(html).not.toContain("Read a.go");
  });
});
