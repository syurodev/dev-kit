import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { CodexSignInPanel, DEVICE_CODE_HINT, codexSignInOutcome, createSignInTracker } from "./CodexSignIn";

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

  it("says its own line under the buttons when it is not the built-in sign-in", () => {
    expect(panel()).toContain("Codex&#x27;s own ChatGPT login");
    const html = panel({ note: "Signs in to this account's own folder." });
    expect(html).toContain("Signs in to this account&#x27;s own folder.");
    expect(html).not.toContain("your ~/.codex is");
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

describe("which sign-in events a panel acts on", () => {
  const flow = (loginId: string) => ({ loginId, method: "browser", authUrl: "https://auth.example/x" });
  const result = (loginId: string, success: boolean, error = "") => ({ agent: "codex", loginId, success, error });

  it("its own flow's end, success or not", () => {
    const t = createSignInTracker();
    expect(t.started(flow("a"))).toBeNull();
    expect(t.event(result("a", false, "Sign-in timed out."))?.error).toBe("Sign-in timed out.");
    expect(t.current).toBeNull();
  });

  it("another flow failing is not news", () => {
    const t = createSignInTracker();
    t.started(flow("a"));
    expect(t.event(result("b", false, "Sign-in cancelled."))).toBeNull();
    expect(t.current?.loginId).toBe("a");
  });

  it("a sign-in finished elsewhere lets the waiting action go ahead", () => {
    // The hub waits to retry; the person signs in from Settings instead.
    const hub = createSignInTracker();
    expect(hub.event(result("settings-1", true))?.success).toBe(true);
    // And if the hub had a flow, Settings replaced it: first its failure,
    // then the success that counts.
    const busy = createSignInTracker();
    busy.started(flow("a"));
    expect(busy.event(result("a", false, "Replaced by a new sign-in."))?.success).toBe(false);
    expect(busy.event(result("b", true))?.success).toBe(true);
  });

  it("an end that beats its own start is taken when the start answers", () => {
    const t = createSignInTracker();
    expect(t.event(result("a", false, "Codex exited before the sign-in finished."))).toBeNull();
    const ended = t.started(flow("a"));
    expect(ended !== null && ended !== "reported" && ended.error).toBe("Codex exited before the sign-in finished.");
    expect(t.current).toBeNull();
  });

  it("an early success is acted on once, not again at start", () => {
    const t = createSignInTracker();
    expect(t.event(result("a", true))?.success).toBe(true);
    expect(t.started(flow("a"))).toBe("reported");
    expect(t.current).toBeNull();
  });

  it("an added account's sign-in is not the built-in one's news", () => {
    const hub = createSignInTracker();
    hub.started(flow("a"));
    const work = { ...result("w1", true), accountId: "codex-work" };
    expect(hub.event(work)).toBeNull();
    expect(hub.event({ ...result("w2", false, "Sign-in cancelled."), accountId: "codex-work" })).toBeNull();
    expect(hub.current?.loginId).toBe("a");
    expect(hub.event(result("a", true))?.success).toBe(true);
  });

  it("an account's tracker acts on its own account's events only", () => {
    const work = createSignInTracker("codex-work");
    expect(work.started(flow("w1"))).toBeNull();
    // The built-in flow and another account's flow are not this panel's.
    expect(work.event(result("a", true))).toBeNull();
    expect(work.event({ ...result("s1", true), accountId: "codex-spare" })).toBeNull();
    expect(work.current?.loginId).toBe("w1");
    expect(work.event({ ...result("w1", true), accountId: "codex-work" })?.success).toBe(true);
    expect(work.current).toBeNull();
  });

  it("an account's sign-in finished elsewhere lets the waiting panel go ahead", () => {
    const work = createSignInTracker("codex-work");
    work.started(flow("w1"));
    expect(work.event({ ...result("w1", false, "Replaced by a new sign-in."), accountId: "codex-work" })?.success).toBe(false);
    expect(work.event({ ...result("w2", true), accountId: "codex-work" })?.success).toBe(true);
  });

  it("other vendors are ignored, and cancel hands back the flow to withdraw", () => {
    const t = createSignInTracker();
    t.started(flow("a"));
    expect(t.event({ agent: "claude", loginId: "a", success: true, error: "" })).toBeNull();
    expect(t.cancel()?.loginId).toBe("a");
    expect(t.cancel()).toBeNull();
    expect(t.event(result("a", false))).toBeNull();
  });
});
