import { describe, expect, it } from "bun:test";
import { renderToString as renderRaw } from "react-dom/server";
import { TooltipProvider } from "@/components/ui/tooltip";
import { AppError } from "../../bridge/errors";
import { signInStatusLine } from "../../bridge/agentSignIn";
import { createFailure } from "./session/sessionModel";
import { AgentSessionError } from "./surfaces/AgentSessionError";
import { chosenKeyLabel } from "./CodexAccountSection";
import type { Account } from "@/bridge/accounts";

const render = (node: React.ReactNode) => renderRaw(<TooltipProvider>{node}</TooltipProvider>);
const keyError = new AppError({ code: "agent_api_key_unusable", message: "Codex can't use the API key", recoverable: true });

// A chosen key that fails is fixed in Settings. A ChatGPT sign-in would
// succeed and change nothing, since the key wins at every spawn.
describe("a failing Codex API key", () => {
  it("does not offer a sign-in", () => {
    const failure = createFailure("codex", keyError);
    expect(failure.signIn).toBe(false);
    expect(failure.apiKey).toBe(true);
    expect(createFailure("codex", new AppError({ code: "agent_auth_required", message: "x", recoverable: true })).apiKey).toBe(false);
  });

  it("the error card sends the person to Settings", () => {
    const html = render(
      <AgentSessionError cause="apiKey" agentName="Codex" onRetry={() => {}} onOpenSettings={() => {}} />,
    );
    expect(html).toContain("Codex can&#x27;t use its API key");
    expect(html).toContain('data-testid="session-error-open-settings"');
    expect(html).not.toContain('data-testid="session-error-retry"');
  });

  it("Settings says the key is the problem, not the sign-in", () => {
    expect(signInStatusLine({ signedIn: false, method: "apiKey" }, "OpenAI")).toBe("API key can't be used");
  });
});

function account(id: string, over: Partial<Account> = {}): Account {
  return {
    id, provider: "openai", kind: "token", label: `key ${id}`, subject: "", scopes: [], env: [],
    scopeOwner: "", state: "valid", expiresAt: "", verifiedAt: "", ref: "", ...over,
  };
}

describe("the chosen key the list does not hold", () => {
  const usable = [account("a")];
  it("is nothing to add when the list has it, or none is chosen", () => {
    expect(chosenKeyLabel("a", usable, { pending: false, error: false, all: usable })).toBeNull();
    expect(chosenKeyLabel("", [], { pending: true, error: false, all: [] })).toBeNull();
  });

  it("never leaves the Select blank while accounts load or fail", () => {
    expect(chosenKeyLabel("a", [], { pending: true, error: false, all: [] })).toBe("Loading…");
    expect(chosenKeyLabel("a", [], { pending: false, error: true, all: [] })).toBe("The chosen account");
  });

  it("names a key that stopped being usable, and one that is gone", () => {
    const bound = account("b", { env: ["prod"] });
    expect(chosenKeyLabel("b", usable, { pending: false, error: false, all: [...usable, bound] })).toBe("key b · can't be used");
    expect(chosenKeyLabel("z", usable, { pending: false, error: false, all: usable })).toBe("Missing account");
  });
});
