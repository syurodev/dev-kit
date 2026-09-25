import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { CodexSignInPanel, DEVICE_CODE_HINT, codexSignInOutcome } from "./CodexSignIn";

const noop = () => {};
const panel = (over: Partial<Parameters<typeof CodexSignInPanel>[0]> = {}) =>
  renderToString(
    <CodexSignInPanel flow={null} starting="" outcome={null} startError="" onStart={noop} onCancel={noop} {...over} />,
  );

describe("Codex sign-in panel", () => {
  it("offers the browser first and a code as the alternative", () => {
    const html = panel();
    expect(html).toContain("Sign in with ChatGPT");
    expect(html).toContain("Use a code instead");
    expect(html.indexOf("Sign in with ChatGPT")).toBeLessThan(html.indexOf("Use a code instead"));
    // No field to type a credential into, ever.
    expect(html).not.toContain("<input");
  });

  it("while the browser is open, says where to finish and how to stop", () => {
    const html = panel({ flow: { loginId: "l1", method: "browser", authUrl: "https://auth.example/x" } });
    expect(html).toContain("Finish signing in in your browser.");
    expect(html).toContain("Open the page again");
    expect(html).toContain("Cancel");
    expect(html).not.toContain("Sign in with ChatGPT");
  });

  it("shows the code with the one step Codex never mentions", () => {
    const html = panel({
      flow: { loginId: "l2", method: "deviceCode", verificationUrl: "https://auth.example/device", userCode: "ABCD-1234" },
    });
    expect(html).toContain("ABCD-1234");
    expect(html).toContain("Open the page");
    expect(html).toContain("ChatGPT → Settings → Security");
    expect(DEVICE_CODE_HINT).toContain("device code");
  });

  it("says how it ended", () => {
    expect(panel({ outcome: { signedIn: true, message: "Signed in to Codex." } })).toContain("Signed in to Codex.");
    expect(panel({ outcome: { signedIn: false, message: "Sign-in timed out." } })).toContain("Sign-in timed out.");
    expect(panel({ startError: "Codex is not installed." })).toContain("Codex is not installed.");
  });
});

describe("codexSignInOutcome", () => {
  it("only a success retries what was waiting", () => {
    expect(codexSignInOutcome({ success: true, error: "" }).signedIn).toBe(true);
    expect(codexSignInOutcome({ success: false, error: "Sign-in cancelled." })).toEqual({
      signedIn: false,
      message: "Sign-in cancelled.",
    });
    expect(codexSignInOutcome({ success: false, error: "" }).message).toBe("Sign-in did not finish.");
  });
});
