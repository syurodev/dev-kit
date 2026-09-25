import { useEffect, useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Browser } from "@wailsio/runtime";
import { Check, Copy, ExternalLink, KeyRound, Loader2, LogIn, X } from "lucide-react";

import { Button } from "@/components/ui/button";
import { CODEX_VENDOR } from "@/bridge/agenthub";
import type { SignInOutcome } from "@/bridge/agentRuntime";
import {
  agentHubCancelSignIn,
  agentHubStartBrowserSignIn,
  agentHubStartDeviceSignIn,
  onAgentSignInCompleted,
  type SignInFlow,
  type SignInResult,
} from "@/bridge/agentSignIn";
import { copyText } from "@/bridge/clipboard";
import { errorMessage } from "@/bridge/errors";

export const agentSignInStatusQueryKey = (agent: string) => ["agentSignInStatus", agent] as const;

export type CodexSignInMethod = "browser" | "deviceCode";

/**
 * What the person has to do before a code works. Device code sign-in is off
 * by default for every ChatGPT account, and Codex does not say so: it hands
 * out a code as usual and the refusal appears only on OpenAI's page. So the
 * UI says it, next to the code, every time.
 */
export const DEVICE_CODE_HINT =
  "Turn on device code sign-in first: ChatGPT → Settings → Security → device code authorization for Codex.";

/** What a finished sign-in means for the action waiting on it. */
export function codexSignInOutcome(result: Pick<SignInResult, "success" | "error">): SignInOutcome {
  if (result.success) return { signedIn: true, message: "Signed in to Codex." };
  return { signedIn: false, message: result.error || "Sign-in did not finish." };
}

/**
 * Sign in to Codex, as a state its owner keeps — the same shape as
 * useClaudeSignIn, so Agent Hub can retry the action that needed it once the
 * sign-in lands.
 *
 * The flow runs in the backend (a short-lived Codex in the jail, holding the
 * login page's callback port) and ends with an agenthub:signInCompleted event.
 * Only the event of the flow this hook started counts: a flow it replaced or
 * cancelled reports too, and must not read as this one finishing.
 */
export function useCodexSignIn(onDone?: (outcome: SignInOutcome) => void) {
  const client = useQueryClient();
  const [flow, setFlow] = useState<SignInFlow | null>(null);
  const [starting, setStarting] = useState<CodexSignInMethod | "">("");
  const [outcome, setOutcome] = useState<SignInOutcome | null>(null);
  const [startError, setStartError] = useState("");
  // Read by the event handler and by cleanup, which see neither render's state.
  const flowRef = useRef<SignInFlow | null>(null);
  const onDoneRef = useRef(onDone);
  onDoneRef.current = onDone;

  useEffect(
    () =>
      onAgentSignInCompleted((result) => {
        const current = flowRef.current;
        if (result.agent !== CODEX_VENDOR || !current || result.loginId !== current.loginId) return;
        flowRef.current = null;
        setFlow(null);
        const next = codexSignInOutcome(result);
        setOutcome(next);
        void client.invalidateQueries({ queryKey: agentSignInStatusQueryKey(CODEX_VENDOR) });
        onDoneRef.current?.(next);
      }),
    [client],
  );

  // A flow nobody is looking at any more holds the callback port until it
  // times out. Leaving withdraws it.
  useEffect(
    () => () => {
      const current = flowRef.current;
      flowRef.current = null;
      if (current) void agentHubCancelSignIn(CODEX_VENDOR, current.loginId).catch(() => {});
    },
    [],
  );

  async function start(method: CodexSignInMethod) {
    setStarting(method);
    setOutcome(null);
    setStartError("");
    try {
      const next =
        method === "browser"
          ? await agentHubStartBrowserSignIn(CODEX_VENDOR)
          : await agentHubStartDeviceSignIn(CODEX_VENDOR);
      flowRef.current = next;
      setFlow(next);
      if (next.authUrl) void Browser.OpenURL(next.authUrl);
    } catch (e) {
      setStartError(errorMessage(e));
    } finally {
      setStarting("");
    }
  }

  async function cancel() {
    const current = flowRef.current;
    if (!current) return;
    flowRef.current = null;
    setFlow(null);
    try {
      await agentHubCancelSignIn(CODEX_VENDOR, current.loginId);
    } catch (e) {
      setStartError(errorMessage(e));
    }
  }

  return { flow, starting, outcome, startError, start, cancel };
}

export type CodexSignInController = ReturnType<typeof useCodexSignIn>;

/**
 * Codex's sign-in: ChatGPT in the browser by default, a code for a machine
 * that cannot open one. There is no credential field: the page is OpenAI's,
 * the token lands in the Codex home only DevKit uses, and DevKit sees whether
 * it worked.
 */
export function CodexSignIn({
  signIn,
  onStart,
}: {
  signIn: CodexSignInController;
  /** Called as a sign-in starts, e.g. to get a dialog out of its way. */
  onStart?: () => void;
}) {
  return (
    <CodexSignInPanel
      flow={signIn.flow}
      starting={signIn.starting}
      outcome={signIn.outcome}
      startError={signIn.startError}
      onStart={(method) => {
        void signIn.start(method);
        onStart?.();
      }}
      onCancel={() => void signIn.cancel()}
    />
  );
}

/** The panel itself, stateless so every state renders the same in a test. */
export function CodexSignInPanel({
  flow,
  starting,
  outcome,
  startError,
  onStart,
  onCancel,
}: {
  flow: SignInFlow | null;
  starting: CodexSignInMethod | "";
  outcome: SignInOutcome | null;
  startError: string;
  onStart: (method: CodexSignInMethod) => void;
  onCancel: () => void;
}) {
  return (
    <div data-testid="codex-sign-in" className="flex w-full flex-col gap-2 text-left">
      {flow ? (
        <CodexSignInWaiting flow={flow} onCancel={onCancel} />
      ) : (
        <>
          <div className="flex flex-wrap items-center gap-2">
            <Button
              type="button"
              size="sm"
              disabled={Boolean(starting)}
              data-testid="codex-sign-in-browser"
              onClick={() => onStart("browser")}
            >
              {starting === "browser" ? <Loader2 className="animate-spin" /> : <LogIn />}
              Sign in with ChatGPT
            </Button>
            <Button
              type="button"
              variant="ghost"
              size="sm"
              disabled={Boolean(starting)}
              data-testid="codex-sign-in-code"
              onClick={() => onStart("deviceCode")}
            >
              {starting === "deviceCode" ? <Loader2 className="animate-spin" /> : <KeyRound />}
              Use a code instead
            </Button>
          </div>
          <p className="text-caption-11 text-text-tertiary">
            Codex&apos;s own ChatGPT login, into a Codex home only DevKit uses — your ~/.codex is
            left alone. DevKit never sees your password.
          </p>
        </>
      )}
      {outcome?.signedIn ? (
        <p className="flex items-center gap-1.5 text-body-12 text-text-primary">
          <Check className="size-4 shrink-0 text-status-success" />
          {outcome.message}
        </p>
      ) : outcome ? (
        <p className="text-body-12 text-status-danger">{outcome.message}</p>
      ) : null}
      {startError ? <p className="text-body-12 text-status-danger">{startError}</p> : null}
    </div>
  );
}

function CodexSignInWaiting({ flow, onCancel }: { flow: SignInFlow; onCancel: () => void }) {
  const [copied, setCopied] = useState(false);
  const cancel = (
    <Button type="button" variant="ghost" size="sm" data-testid="codex-sign-in-cancel" onClick={onCancel}>
      <X />
      Cancel
    </Button>
  );

  if (flow.userCode) {
    return (
      <div data-testid="codex-sign-in-code-flow" className="flex flex-col gap-2">
        <p className="text-body-12 text-text-secondary">
          Open the page, sign in, and enter this code:
        </p>
        <div className="flex flex-wrap items-center gap-2">
          <code
            data-testid="codex-sign-in-user-code"
            className="rounded-md border border-border-subtle bg-bg-canvas px-2.5 py-1 text-code-12 tracking-widest text-text-primary"
          >
            {flow.userCode}
          </code>
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => {
              copyText(flow.userCode ?? "")
                .then(() => {
                  setCopied(true);
                  window.setTimeout(() => setCopied(false), 1500);
                })
                // Clipboard can be unavailable in a webview; the code is selectable.
                .catch(() => {});
            }}
          >
            {copied ? <Check className="text-status-success" /> : <Copy />}
            {copied ? "Copied" : "Copy"}
          </Button>
          {flow.verificationUrl ? (
            <Button
              type="button"
              size="sm"
              onClick={() => void Browser.OpenURL(flow.verificationUrl ?? "")}
            >
              <ExternalLink />
              Open the page
            </Button>
          ) : null}
          {cancel}
        </div>
        <p className="text-caption-11 text-text-tertiary">{DEVICE_CODE_HINT}</p>
      </div>
    );
  }

  return (
    <div data-testid="codex-sign-in-browser-flow" className="flex flex-col gap-2">
      <p className="flex items-center gap-1.5 text-body-12 text-text-secondary">
        <Loader2 className="size-3.5 animate-spin" />
        Finish signing in in your browser.
      </p>
      <div className="flex flex-wrap items-center gap-2">
        {flow.authUrl ? (
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => void Browser.OpenURL(flow.authUrl ?? "")}
          >
            <ExternalLink />
            Open the page again
          </Button>
        ) : null}
        {cancel}
      </div>
    </div>
  );
}
