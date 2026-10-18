import { describe, expect, test } from "bun:test";
import { AgentType } from "../../bindings/devkit/internal/agenthub/models";
import { CLAUDE_VENDOR, CODEX_VENDOR, OPENCODE_VENDOR } from "./agenthub";
import {
  CLAUDE_AGENT,
  CODEX_AGENT,
  OPENCODE_AGENT,
  formatBytes,
  hintCommands,
  isAgentAuthRequiredError,
  isAgentNotInstalledError,
  signInLabel,
  signInOutcome,
  isRuntimeRequirementError,
  runtimeBlocked,
  runtimeStatusLine,
  type AgentRuntimeStatus,
  type InstalledRuntime,
} from "./agentRuntime";
import { compareVersions } from "./agentRuntime";
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

  test("Codex offers the newer npm version", () => {
    const line = runtimeStatusLine(status({
      testedVersion: "0.147.0", current: installed("0.147.0"), latestVersion: "0.150.0",
    }), "Codex");
    expect(line).toEqual({ headline: "Codex 0.147.0 · tested", latest: "0.150.0 available (not tested by DevKit)" });
  });

  // opencode has no tested version: it is whatever npm's latest was when it was
  // installed, so "tested" and "not tested" would both be untrue of it.
  test("an agent with no tested version says neither tested nor untested", () => {
    const none = { testedVersion: "" };
    expect(runtimeStatusLine(status({ ...none }), "opencode")).toEqual({ headline: "Not installed", latest: null });
    expect(runtimeStatusLine(status({ ...none, current: installed("2.0.32"), latestVersion: "2.0.32" }), "opencode")).toEqual({
      headline: "opencode 2.0.32",
      latest: null,
    });
    expect(runtimeStatusLine(status({ ...none, current: installed("2.0.32"), latestVersion: "2.0.33" }), "opencode")).toEqual({
      headline: "opencode 2.0.32",
      latest: "2.0.33 available",
    });
    // Not installed yet: nothing to update from, so no offer next to "Install latest".
    expect(runtimeStatusLine(status({ ...none, latestVersion: "2.0.33" }), "opencode").latest).toBeNull();
    // A cached catalog older than what is installed is not an update.
    expect(runtimeStatusLine(status({ ...none, current: installed("2.0.40"), latestVersion: "2.0.33" }), "opencode").latest).toBeNull();
  });

  test("an older cached catalog does not offer a downgrade as an update", () => {
    expect(runtimeStatusLine(status({
      testedVersion: "0.147.0", current: installed("0.151.0"), latestVersion: "0.150.0",
    }), "Codex").latest).toBeNull();
    expect(runtimeStatusLine(status({
      testedVersion: "0.147.0", latestVersion: "0.146.0",
    }), "Codex").latest).toBeNull();
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

test("versions compare by number, not by text", () => {
  expect(compareVersions("0.10.0", "0.9.0")).toBe(1);
  expect(compareVersions("0.74.0", "0.76.0")).toBe(-1);
  expect(compareVersions("0.74", "0.74.0")).toBe(0);
});

describe("sign-in", () => {
  test("known methods get plain labels, new ones their own name", () => {
    expect(signInLabel({ id: "claude-ai-login", name: "Claude Subscription" })).toBe(
      "Sign in with Claude subscription",
    );
    expect(signInLabel({ id: "console-login", name: "Anthropic Console" })).toBe(
      "Sign in with Anthropic Console",
    );
    expect(signInLabel({ id: "sso-login", name: "Company SSO" })).toBe("Sign in with Company SSO");
  });

  test("only exit 0 is signed in, and so only exit 0 retries", () => {
    expect(signInOutcome(0)).toEqual({ signedIn: true, message: "Signed in." });
    expect(signInOutcome(1)).toEqual({ signedIn: false, message: "Sign-in exited with code 1." });
    expect(signInOutcome(-1).signedIn).toBe(false);
    expect(signInOutcome(-1).message).toBe("Sign-in stopped before it finished.");
  });

  test("auth required is recognised by its code", () => {
    const coded = (code: string) => new AppError({ code, message: "Claude isn't signed in.", recoverable: true });
    expect(isAgentAuthRequiredError(coded("agent_auth_required"))).toBe(true);
    expect(isAgentAuthRequiredError(coded("connection_failed"))).toBe(false);
  });
});

describe("the two Claude ids", () => {
  // The runtime id names the adapter DevKit installs; the vendor is the
  // agentType a session is created with. A draft once defaulted to the runtime
  // id, and every first message failed with "Unknown agent vendor".
  test("a session is created with the vendor the backend registers", () => {
    expect(CLAUDE_VENDOR).toBe(AgentType.AgentTypeClaude);
  });

  test("and that is not the runtime's id", () => {
    expect(CLAUDE_AGENT).not.toBe(CLAUDE_VENDOR);
  });
});

describe("opencode's ids", () => {
  test("a session is created with the vendor the backend registers", () => {
    expect(OPENCODE_VENDOR).toBe(AgentType.AgentTypeOpenCode);
  });

  test("the runtime id is the agentruntime registry's", () => {
    expect(OPENCODE_AGENT).toBe("opencode");
  });
});

describe("the two Codex ids", () => {
  test("a session is created with the vendor the backend registers", () => {
    expect(CODEX_VENDOR).toBe(AgentType.AgentTypeCodex);
  });

  test("the runtime id is the agentruntime registry's", () => {
    expect(CODEX_AGENT).toBe("codex");
  });
});

describe("runtimeStatusLine names the agent", () => {
  test("Codex's runtime is not called Claude Agent", () => {
    const line = runtimeStatusLine(status({ agent: "codex", testedVersion: "0.147.0", current: installed("0.147.0") }), "Codex");
    expect(line.headline).toBe("Codex 0.147.0 · tested");
  });
});
