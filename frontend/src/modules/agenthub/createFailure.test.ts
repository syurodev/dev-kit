import { describe, expect, it } from "bun:test";
import { createFailure } from "./session/sessionModel";
import { AppError } from "../../bridge/errors";

const coded = (code: string, message = "x") => new AppError({ code, message, recoverable: true });

describe("a failed create, per vendor", () => {
  it("a signed-out Codex shows Codex's sign-in and retries, like Claude", () => {
    for (const vendor of ["claude", "codex"]) {
      const failure = createFailure(vendor, coded("agent_auth_required"));
      expect(failure.signIn).toBe(true);
      expect(failure.refreshRuntime).toBe("");
    }
  });

  it("an agent with nothing to sign in to gets the error card", () => {
    expect(createFailure("mock", coded("agent_auth_required")).signIn).toBe(false);
  });

  it("not installed re-reads that vendor's runtime, not Claude's", () => {
    const codex = createFailure("codex", coded("agent_not_installed"));
    expect(codex.refreshRuntime).toBe("codex");
    expect(codex.shownByInstallPanel).toBe(true);
    expect(createFailure("claude", coded("agent_not_installed")).refreshRuntime).toBe("claude-acp");
    const requirement = createFailure("codex", coded("runtime_requirement"));
    expect(requirement.refreshRuntime).toBe("codex");
    expect(requirement.shownByInstallPanel).toBe(false);
    // No runtime to install: the card says it.
    expect(createFailure("mock", coded("agent_not_installed")).shownByInstallPanel).toBe(false);
  });

  it("the sandbox hint is for any jailed vendor", () => {
    expect(createFailure("codex", coded("connection_failed", "Agent sandbox is unavailable.")).sandboxHint).toBe(true);
    expect(createFailure("mock", coded("connection_failed", "Agent sandbox is unavailable.")).sandboxHint).toBe(false);
  });

  it("every account at its limit is neither a sign-in nor a missing runtime", () => {
    for (const vendor of ["claude", "codex", "mock"]) {
      const failure = createFailure(vendor, coded("agent_usage_limit", "Every account of this vendor has reached its limit."));
      expect(failure.signIn).toBe(false);
      expect(failure.refreshRuntime).toBe("");
      expect(failure.shownByInstallPanel).toBe(false);
      expect(failure.sandboxHint).toBe(false);
    }
  });
});
