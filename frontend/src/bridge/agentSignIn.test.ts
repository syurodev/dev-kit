import { describe, expect, test } from "bun:test";
import { parseSignInResult, signInStatusLine } from "./agentSignIn";

describe("parseSignInResult", () => {
  test("reads the event main.go emits", () => {
    expect(parseSignInResult({ agent: "codex", loginId: "l1", success: true, error: "" })).toEqual({
      agent: "codex",
      loginId: "l1",
      success: true,
      error: "",
      accountId: "",
    });
  });

  test("says which added account finished, and nothing for the built-in sign-in", () => {
    expect(parseSignInResult({ agent: "codex", loginId: "l9", success: true, accountId: "codex-work" })?.accountId).toBe("codex-work");
    expect(parseSignInResult({ agent: "codex", loginId: "l9", success: true, accountId: 7 })?.accountId).toBe("");
  });

  test("Wails may wrap the payload in an array", () => {
    expect(parseSignInResult([{ agent: "codex", loginId: "l2", success: false, error: "Sign-in timed out." }])).toEqual({
      agent: "codex",
      loginId: "l2",
      success: false,
      error: "Sign-in timed out.",
      accountId: "",
    });
  });

  test("anything else is not a result, and success must be said", () => {
    expect(parseSignInResult(null)).toBeNull();
    expect(parseSignInResult({ agent: "codex" })).toBeNull();
    expect(parseSignInResult({ agent: "codex", loginId: "l3", success: "yes" })?.success).toBe(false);
  });
});

describe("signInStatusLine", () => {
  test("ChatGPT with what Codex said about the account", () => {
    expect(signInStatusLine({ signedIn: true, method: "chatgpt", email: "a@b.c", plan: "plus" })).toBe(
      "ChatGPT · a@b.c · plus",
    );
    expect(signInStatusLine({ signedIn: true, method: "chatgpt" })).toBe("ChatGPT");
  });

  test("an API key is named by its account", () => {
    expect(signInStatusLine({ signedIn: true, method: "apiKey" }, "OpenAI work")).toBe("API key · OpenAI work");
    expect(signInStatusLine({ signedIn: true, method: "apiKey" })).toBe("API key");
  });

  test("not signed in", () => {
    expect(signInStatusLine({ signedIn: false })).toBe("Not signed in");
  });
});
