import { useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Check, Loader2, LogIn } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { errorMessage } from "@/bridge/errors";
import {
  CLAUDE_VENDOR,
  agentHubAuthMethods,
  agentHubStartSignIn,
  signInLabel,
  signInOutcome,
  type AuthMethod,
  type SignInOutcome,
} from "@/bridge/agentRuntime";
import { toast } from "sonner";
import { closeSettings } from "@/workbench/settingsRequest";
import { vendorInfo } from "./vendors";
import { useBottomSlotStore } from "@/stores/bottomSlot";
import { useTerminalTabsStore } from "@/stores/terminalTabs";

export const agentAuthMethodsQueryKey = (agent: string) => ["agentAuthMethods", agent] as const;

/**
 * Sign in to an agent that signs in with its own login in a terminal (Claude,
 * opencode), as a state its owner keeps. Claude unless `vendor` says otherwise.
 *
 * Owned above the buttons because the buttons do not outlive the sign-in: the
 * new-session dialog closes while the login runs (a modal would sit on top of
 * the terminal), and the exit still has to land somewhere and retry.
 *
 * `onDone` is read through a ref so it sees the render the exit arrives in,
 * not the one the button was pressed in.
 */
export function useClaudeSignIn(
  onDone?: (outcome: SignInOutcome) => void,
  vendor: string = CLAUDE_VENDOR,
  /**
   * Listing the methods can start the agent in its jail (the backend caches the
   * answer per install), so a vendor nobody is using yet leaves it off.
   */
  enabled = true,
) {
  const methods = useQuery({
    queryKey: agentAuthMethodsQueryKey(vendor),
    queryFn: () => agentHubAuthMethods(vendor),
    enabled,
    // The list only changes with the installed runtime; the backend caches the
    // probe per install anyway.
    staleTime: Infinity,
  });
  const [running, setRunning] = useState("");
  const [outcome, setOutcome] = useState<SignInOutcome | null>(null);
  const [startError, setStartError] = useState("");
  const onDoneRef = useRef(onDone);
  onDoneRef.current = onDone;

  function start(method: AuthMethod) {
    setRunning(method.id);
    setOutcome(null);
    setStartError("");
    useTerminalTabsStore.getState().openTab({
      title: method.label || signInLabel(method),
      start: (rows, cols) =>
        agentHubStartSignIn(vendor, method.id, rows, cols).catch((e: unknown) => {
          setRunning("");
          setStartError(errorMessage(e));
          throw e;
        }),
      onExit: (code) => {
        const result = signInOutcome(code);
        setRunning("");
        setOutcome(result);
        onDoneRef.current?.(result);
      },
    });
    useBottomSlotStore.getState().setOpen(true);
  }

  return { methods, running, outcome, startError, start };
}

export type ClaudeSignInController = ReturnType<typeof useClaudeSignIn>;

/**
 * One button per sign-in the agent offers, and the plain alternative.
 *
 * There is no credential field here and there must never be one: each button
 * runs Claude Code's own login, unmodified, in a terminal tab the user watches,
 * and sign-in completes through Anthropic's flow. DevKit sees an exit code.
 */
export function ClaudeSignIn({
  signIn,
  onStart,
  vendor = CLAUDE_VENDOR,
}: {
  signIn: ClaudeSignInController;
  /** Called as the terminal opens, e.g. to get a dialog out of its way. */
  onStart?: () => void;
  vendor?: string;
}) {
  const { methods, running, outcome, startError } = signIn;

  return (
    <div data-testid="claude-sign-in" className="flex w-full flex-col gap-2 text-left">
      {methods.isPending ? (
        <Skeleton className="h-7 w-56 rounded-control" />
      ) : methods.isError ? (
        <p className="text-body-12 text-status-danger">{errorMessage(methods.error)}</p>
      ) : (
        <div className="flex flex-wrap items-center gap-2">
          {methods.data.map((method, index) => (
            <Button
              key={method.id}
              type="button"
              size="sm"
              variant={index === 0 ? "default" : "outline"}
              disabled={Boolean(running)}
              title={method.description || undefined}
              data-testid={`sign-in-${method.id}`}
              onClick={() => {
                signIn.start(method);
                onStart?.();
              }}
            >
              {running === method.id ? <Loader2 className="animate-spin" /> : <LogIn />}
              {signInLabel(method)}
            </Button>
          ))}
        </div>
      )}
      <p className="text-caption-11 text-text-tertiary">
        {running ? "Finish signing in in the terminal. " : ""}
        {vendor === CLAUDE_VENDOR ? (
          <>
            Or set <code className="text-code-11">ANTHROPIC_API_KEY</code>. Sign-in runs Claude
            Code&apos;s own login; DevKit never sees your password.
          </>
        ) : (
          <>
            Sign-in runs {vendorInfo(vendor).name}&apos;s own login in DevKit&apos;s own copy of its
            settings, apart from any {vendorInfo(vendor).name} you use yourself; DevKit never sees your password.
          </>
        )}
      </p>
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

/**
 * Settings → Agents: sign-in with nothing to retry. The login runs in a terminal
 * tab under the modal, so starting one closes Settings; the section is gone when
 * the login ends, so the outcome is told in a toast instead of under the button.
 */
export function ClaudeSignInSection({ vendor = CLAUDE_VENDOR }: { vendor?: string } = {}) {
  const name = vendorInfo(vendor).name;
  const signIn = useClaudeSignIn(
    (outcome) => (outcome.signedIn ? toast.success(`${name}: ${outcome.message}`) : toast.error(`${name}: ${outcome.message}`)),
    vendor,
  );
  return <ClaudeSignIn signIn={signIn} vendor={vendor} onStart={closeSettings} />;
}
