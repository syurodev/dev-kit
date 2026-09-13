import { describe, expect, test } from "bun:test";
import {
  formatBytes,
  hintCommands,
  isAgentNotInstalledError,
  isRuntimeRequirementError,
  runtimeBlocked,
  runtimeStatusLine,
  type AgentRuntimeStatus,
  type InstalledRuntime,
} from "./agentRuntime";
import { AppError } from "./errors";

function installed(version: string): InstalledRuntime {
  return { version, sdkVersion: "", installedAt: "", path: "", bundledClaude: "", nodePath: "", nodeVersion: "" };
}

function status(over: Partial<AgentRuntimeStatus>): AgentRuntimeStatus {
  return {
    agent: "claude-acp",
    testedVersion: "0.74.0",
    latestVersion: null,
    current: null,
    previous: null,
    installing: false,
    requirements: [],
    claudeBinary: { source: "bundled", machine: null },
    licenseUrl: null,
    diskBytes: 0,
    ...over,
  };
}

describe("runtimeStatusLine", () => {
  test("nothing installed", () => {
    expect(runtimeStatusLine(status({}))).toEqual({ headline: "Not installed", latest: null });
  });

  test("the tested version, registry agrees", () => {
    const line = runtimeStatusLine(status({ current: installed("0.74.0"), latestVersion: "0.74.0" }));
    expect(line).toEqual({ headline: "Claude Agent 0.74.0 · tested", latest: null });
  });

  test("a newer registry version is offered as untested", () => {
    const line = runtimeStatusLine(status({ current: installed("0.74.0"), latestVersion: "0.76.0" }));
    expect(line.latest).toBe("0.76.0 available (not tested by DevKit)");
  });

  test("already running the latest untested version: no offer, and it says so", () => {
    const line = runtimeStatusLine(status({ current: installed("0.76.0"), latestVersion: "0.76.0" }));
    expect(line).toEqual({ headline: "Claude Agent 0.76.0 · not tested by DevKit", latest: null });
  });
});

test("runtimeBlocked: no install, or an unmet requirement", () => {
  const node = { name: "node", minVersion: "22", met: true, found: "24", path: "/n", problem: "", installHint: "" };
  expect(runtimeBlocked(status({}))).toBe(true);
  expect(runtimeBlocked(status({ current: installed("0.74.0"), requirements: [node] }))).toBe(false);
  expect(runtimeBlocked(status({ current: installed("0.74.0"), requirements: [{ ...node, met: false }] }))).toBe(true);
});

test("error helpers read the code, not the message", () => {
  const coded = (code: string) => new AppError({ code, message: "Claude Agent is not installed.", recoverable: true });
  expect(isAgentNotInstalledError(coded("agent_not_installed"))).toBe(true);
  expect(isAgentNotInstalledError(coded("validation_failed"))).toBe(false);
  expect(isRuntimeRequirementError(coded("runtime_requirement"))).toBe(true);
  expect(isRuntimeRequirementError(new Error("runtime_requirement"))).toBe(false);
});

test("hintCommands pulls out backticked commands", () => {
  expect(hintCommands("Install Node.js 22 or newer, e.g. `brew install node` or `nvm install 22`.")).toEqual([
    "brew install node",
    "nvm install 22",
  ]);
  expect(hintCommands("npm ships with Node.js; reinstall Node.js if it is missing.")).toEqual([]);
});

test("formatBytes", () => {
  expect(formatBytes(0)).toBe("0 B");
  expect(formatBytes(512)).toBe("512 B");
  expect(formatBytes(1536)).toBe("1.5 KB");
  expect(formatBytes(250 * 1024 * 1024)).toBe("250 MB");
});
