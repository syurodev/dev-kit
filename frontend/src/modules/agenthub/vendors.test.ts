import { describe, expect, test } from "bun:test";
import { AgentType } from "../../../bindings/devkit/internal/agenthub/models";
import { CLAUDE_AGENT, CODEX_AGENT, OPENCODE_AGENT } from "@/bridge/agentRuntime";
import { CLAUDE_INFO, CODEX_INFO, OPENCODE_INFO, SHIPPED_VENDORS, vendorInfo } from "./vendors";

describe("vendor table", () => {
  test("every shipped vendor is one the backend registers", () => {
    const registered = Object.values(AgentType) as string[];
    for (const id of SHIPPED_VENDORS) expect(registered).toContain(id);
    expect(SHIPPED_VENDORS).not.toContain(AgentType.AgentTypeMock);
  });

  test("each vendor names the runtime DevKit installs for it", () => {
    expect(vendorInfo("claude")).toBe(CLAUDE_INFO);
    expect(vendorInfo("claude").runtimeId).toBe(CLAUDE_AGENT);
    expect(vendorInfo("codex")).toBe(CODEX_INFO);
    expect(vendorInfo("codex").runtimeId).toBe(CODEX_AGENT);
    expect(vendorInfo("opencode")).toBe(OPENCODE_INFO);
    expect(vendorInfo("opencode").runtimeId).toBe(OPENCODE_AGENT);
  });

  test("Claude and opencode sign in in a terminal, Codex inside DevKit", () => {
    expect(vendorInfo("claude").signIn).toBe("terminal");
    expect(vendorInfo("codex").signIn).toBe("inApp");
    expect(vendorInfo("opencode").signIn).toBe("terminal");
  });

  test("opencode has no skills/MCP overrides of its own to manage", () => {
    expect(vendorInfo("opencode").customization).toBeUndefined();
    expect(vendorInfo("opencode").configCatalog).toBe(true);
  });

  test("an unknown vendor is itself, never Claude", () => {
    const mock = vendorInfo("mock");
    expect(mock.name).toBe("Mock");
    expect(mock.runtimeId).toBe("");
    expect(mock.signIn).toBe("none");
    expect(vendorInfo("").name).toBe("Agent");
    expect(vendorInfo(undefined).signIn).toBe("none");
  });

  test("names are what the person reads", () => {
    expect(vendorInfo("claude").name).toBe("Claude");
    expect(vendorInfo("codex").name).toBe("Codex");
    expect(vendorInfo("opencode").name).toBe("opencode");
  });
});
