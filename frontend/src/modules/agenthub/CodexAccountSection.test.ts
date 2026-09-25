import { describe, expect, test } from "bun:test";
import type { Account } from "@/bridge/accounts";
import { CHATGPT_CHOICE, codexApiKeyChoices } from "./CodexAccountSection";

function account(over: Partial<Account>): Account {
  return {
    id: "0123456789abcdef01234567",
    provider: "openai",
    kind: "token",
    label: "OpenAI",
    subject: "",
    scopes: [],
    env: [],
    scopeOwner: "",
    state: "valid",
    expiresAt: "",
    verifiedAt: "",
    ref: "",
    ...over,
  };
}

describe("which accounts can be Codex's API key", () => {
  test("a shared token with no environment restriction", () => {
    const ok = account({ id: "a".repeat(24) });
    const list = [
      ok,
      account({ id: "b".repeat(24), kind: "basic" }),
      account({ id: "c".repeat(24), kind: "external", label: "Codex (ChatGPT)" }),
      account({ id: "d".repeat(24), env: ["prod"] }),
      account({ id: "e".repeat(24), kind: "secret", scopeOwner: "f".repeat(24) }),
    ];
    expect(codexApiKeyChoices(list)).toEqual([ok]);
  });

  test("any provider's token may be one: the key is what matters", () => {
    expect(codexApiKeyChoices([account({ provider: "generic" })])).toHaveLength(1);
  });

  test("'none' is a value Radix accepts and no account id can be", () => {
    expect(CHATGPT_CHOICE).not.toBe("");
    expect(CHATGPT_CHOICE).not.toMatch(/^[0-9a-f]{24}$/);
  });
});
