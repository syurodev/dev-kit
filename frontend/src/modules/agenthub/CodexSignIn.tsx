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
import { agentAccountsStartBrowserSignIn, agentAccountsStartDeviceSignIn } from "@/bridge/agentAccounts";
import { copyText } from "@/bridge/clipboard";
import { errorMessage } from "@/bridge/errors";
import { AGENT_ACCOUNTS_KEY } from "./accounts/useAgentAccounts";

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

/** How many ended flows the tracker remembers for a start still in flight. */
const EARLY_RESULTS = 8;

/**
 * Which agenthub:signInCompleted events a sign-in panel acts on, kept apart
 * from React so the rules can be tested.
 *
 * - Its own flow's end, success or not.
 * - Any other Codex sign-in that succeeded: the backend runs one flow at a
 *   time, so a sign-in finished elsewhere (Settings, while the hub waits to
 *   retry) is the account every session will use, and the action waiting on
 *   it can go ahead. Its own flow, if it had one, was replaced by that one.
 * - An end that arrives before the start that made it has answered (Codex
 *   exiting at once): remembered, and taken when the start resolves, so the
 *   panel does not wait for an event that already came.
 *
 * `accountId` is the added account this tracker signs in ("" for the built-in
 * sign-in). Every rule above holds only among events about that account: the
 * built-in panel ignores an added account's flow and the other way round.
 */
export function createSignInTracker(accountId = "") {
  let current: SignInFlow | null = null;
  // Results of flows not known to be this tracker's, newest last.
  const early: SignInResult[] = [];
  return {
    get current() {
      return current;
    },
    /**
     * A start answered. Returns how that flow already ended, or null while it
     * runs; "reported" when it ended in a success event() already acted on.
     */
    started(flow: SignInFlow): SignInResult | "reported" | null {
      const index = early.findIndex((r) => r.loginId === flow.loginId);
      const ended = index >= 0 ? early[index] : null;
      early.length = 0;
      if (!ended) {
        current = flow;
        return null;
      }
      current = null;
      return ended.success ? "reported" : ended;
    },
    /** An event. Returns the result to act on, or null to ignore it. */
    event(result: SignInResult): SignInResult | null {
      if (result.agent !== CODEX_VENDOR || (result.accountId ?? "") !== accountId) return null;
      if (current && result.loginId === current.loginId) {
        current = null;
        return result;
      }
      early.push(result);
      if (early.length > EARLY_RESULTS) early.shift();
      if (!result.success) return null;
      current = null;
      return result;
    },
    /** Withdraws the flow in progress; returns it for the backend cancel. */
    cancel(): SignInFlow | null {
      const flow = current;
      current = null;
      return flow;
    },
  };
}

/**
 * Sign in to Codex, as a state its owner keeps — the same shape as
 * useClaudeSignIn, so Agent Hub can retry the action that needed it once the
 * sign-in lands.
 *
 * The flow runs in the backend (a short-lived Codex in the jail, holding the
 * login page's callback port) and ends with an agenthub:signInCompleted event;
 * createSignInTracker decides which events count.
 *
 * With an `accountId` it signs in that added account instead (its own folder),
 * and finishing refreshes the accounts lists rather than the built-in status.
 * The id is fixed for the hook's life: give a panel for another account its own
 * mount (a `key`).
 */
export function useCodexSignIn(onDone?: (outcome: SignInOutcome) => void, accountId = "") {
  const client = useQueryClient();
  const [flow, setFlow] = useState<SignInFlow | null>(null);
  const [starting, setStarting] = useState<CodexSignInMethod | "">("");
  const [outcome, setOutcome] = useState<SignInOutcome | null>(null);
  const [startError, setStartError] = useState("");
  // Read by the event handler and by cleanup, which see neither render's state.
  const [tracker] = useState(() => createSignInTracker(accountId));
  const alive = useRef(true);
  const onDoneRef = useRef(onDone);
  onDoneRef.current = onDone;

  function finish(result: SignInResult) {
    setFlow(null);
    const next = codexSignInOutcome(result);
    setOutcome(next);
    void client.invalidateQueries({
      queryKey: accountId ? AGENT_ACCOUNTS_KEY : agentSignInStatusQueryKey(CODEX_VENDOR),
    });
    onDoneRef.current?.(next);
  }
  const finishRef = useRef(finish);
  finishRef.current = finish;

  useEffect(
    () =>
      onAgentSignInCompleted((result) => {
        const act = tracker.event(result);
        if (act) finishRef.current(act);
      }),
    [tracker],
  );

  // A flow nobody is looking at any more holds the callback port until it
  // times out. Leaving withdraws it — including one whose start is still in
  // flight, which start() withdraws when it answers.
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
      const current = tracker.cancel();
      if (current) void agentHubCancelSignIn(CODEX_VENDOR, current.loginId).catch(() => {});
    };
  }, [tracker]);

  async function start(method: CodexSignInMethod) {
    setStarting(method);
    setOutcome(null);
    setStartError("");
    try {
      const browser = method === "browser";
      const next = accountId
        ? await (browser ? agentAccountsStartBrowserSignIn(accountId) : agentAccountsStartDeviceSignIn(accountId))
        : await (browser ? agentHubStartBrowserSignIn(CODEX_VENDOR) : agentHubStartDeviceSignIn(CODEX_VENDOR));
      if (!alive.current) {
        void agentHubCancelSignIn(CODEX_VENDOR, next.loginId).catch(() => {});
        return;
      }
      const ended = tracker.started(next);
      if (ended === null) {
        setFlow(next);
        if (next.authUrl) void Browser.OpenURL(next.authUrl);
      } else if (ended !== "reported") {
        finish(ended);
      }
    } catch (e) {
      setStartError(errorMessage(e));
    } finally {
      setStarting("");
    }
  }

  async function cancel() {
    const current = tracker.cancel();
    if (!current) return;
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
  note,
}: {
  signIn: CodexSignInController;
  /** Called as a sign-in starts, e.g. to get a dialog out of its way. */
  onStart?: () => void;
  /** Replaces the line under the buttons, for a sign-in that is not into DevKit's own Codex home. */
  note?: React.ReactNode;
}) {
  return (
    <CodexSignInPanel
      note={note}
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
  note,
}: {
  note?: React.ReactNode;
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
            {note ?? (
              <>
                Codex&apos;s own ChatGPT login, into a Codex home only DevKit uses — your ~/.codex is
                left alone. DevKit never sees your password.
              </>
            )}
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
