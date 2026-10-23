import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import type { AgentAccount } from "@/bridge/agentAccounts";
import { closeSettings } from "@/workbench/settingsRequest";
import { ClaudeSignIn, useClaudeSignIn } from "../ClaudeSignIn";
import { CodexSignIn, useCodexSignIn } from "../CodexSignIn";
import { vendorInfo } from "../vendors";
import { AGENT_ACCOUNTS_KEY } from "./useAgentAccounts";

/** The line under a sign-in button: what this login is, and that the one the person uses themselves is left alone. */
export function accountSignInNote(vendor: string): string {
  const name = vendorInfo(vendor).name;
  return `Signs in as a separate ${name} login for DevKit. The login you use yourself is left alone, and DevKit never sees your password.`;
}

/** What a signed-out built-in account says instead of a sign-in: Go refuses one here, Runtimes owns it. */
export const BUILTIN_SIGN_IN_LINE = "Sign in to this account under Settings → Agents → Runtimes.";

/**
 * Claude's login runs in a terminal tab, as it does under Runtimes: the tab is
 * in the bottom slot behind Settings, so starting it closes Settings, and how it
 * ended is told in a toast. Nothing reopens Settings afterwards — no sign-in does.
 */
function ClaudeAccountSignIn({ account }: { account: AgentAccount }) {
  const client = useQueryClient();
  const signIn = useClaudeSignIn(
    (outcome) => {
      void client.invalidateQueries({ queryKey: AGENT_ACCOUNTS_KEY });
      if (outcome.signedIn) toast.success(`${account.label}: ${outcome.message}`);
      else toast.error(`${account.label}: ${outcome.message}`);
    },
    account.vendor,
    true,
    account.id,
  );
  return <ClaudeSignIn signIn={signIn} vendor={account.vendor} onStart={closeSettings} note={accountSignInNote(account.vendor)} />;
}

/** Codex signs in inside DevKit, so the page stays open and the row turns "Signed in" when the event lands. */
function CodexAccountSignIn({ account }: { account: AgentAccount }) {
  const signIn = useCodexSignIn((outcome) => {
    if (outcome.signedIn) toast.success(`${account.label}: ${outcome.message}`);
  }, account.id);
  return <CodexSignIn signIn={signIn} note={accountSignInNote(account.vendor)} />;
}

/**
 * The sign-in for a signed-out account, by how its vendor signs in. The built-in
 * account signs in from Runtimes (the backend refuses it here), so it gets one
 * line and none of the sign-in controls, which would start their own probes.
 */
export function AccountSignIn({ account }: { account: AgentAccount }) {
  if (account.builtin) return <p className="text-caption-11 text-text-tertiary">{BUILTIN_SIGN_IN_LINE}</p>;
  return vendorInfo(account.vendor).signIn === "inApp" ? (
    <CodexAccountSignIn account={account} />
  ) : (
    <ClaudeAccountSignIn account={account} />
  );
}
