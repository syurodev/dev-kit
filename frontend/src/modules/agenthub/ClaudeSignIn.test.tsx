import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { ClaudeSignIn, type ClaudeSignInController } from "./ClaudeSignIn";

const controller = (over: Partial<Record<string, unknown>> = {}) =>
  ({
    methods: { isPending: false, isError: false, data: [{ id: "claude-ai-login", label: "", description: "" }] },
    running: "",
    outcome: null,
    startError: "",
    start: () => {},
    ...over,
  }) as unknown as ClaudeSignInController;

describe("ClaudeSignIn", () => {
  it("offers the agent's own login and says it is the machine's own", () => {
    const html = renderToString(<ClaudeSignIn signIn={controller()} />);
    expect(html).toContain("Sign in with Claude subscription");
    expect(html).toContain("ANTHROPIC_API_KEY");
  });

  it("an added account says where its login goes instead", () => {
    const html = renderToString(
      <ClaudeSignIn signIn={controller()} note="Signs in to this account's own folder; your ~/.claude login is left alone." />,
    );
    expect(html).toContain("Sign in with Claude subscription");
    expect(html).toContain("this account&#x27;s own folder");
    expect(html).not.toContain("ANTHROPIC_API_KEY");
  });

  it("says how the login ended", () => {
    expect(renderToString(<ClaudeSignIn signIn={controller({ outcome: { signedIn: true, message: "Signed in." } })} />)).toContain(
      "Signed in.",
    );
    expect(renderToString(<ClaudeSignIn signIn={controller({ startError: "Claude is not installed." })} />)).toContain(
      "Claude is not installed.",
    );
  });
});
