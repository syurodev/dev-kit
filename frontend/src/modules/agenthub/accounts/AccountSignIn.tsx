import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import type { AgentAccount } from "@/bridge/agentAccounts";
import { closeSettings } from "@/workbench/settingsRequest";
import { ClaudeSignIn, useClaudeSignIn } from "../ClaudeSignIn";
import { CodexSignIn, useCodexSignIn } from "../CodexSignIn";
import { vendorInfo } from "../vendors";
import { AGENT_ACCOUNTS_KEY } from "./useAgentAccounts";

/** The line under a sign-in button: where this login goes, and that the machine's own is left alone. */
export function accountSignInNote(vendor: string): string {
  const name = vendorInfo(vendor).name;
  return vendor === "claude"
    ? "Signs in to this account's own folder. The Claude login you use yourself in ~/.claude is left alone, and DevKit never sees your password."
    : `Signs in to this account's own ${name} folder. Your ~/.codex is left alone, and DevKit never sees your password.`;
}

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

/** The sign-in for a signed-out account, by how its vendor signs in. */
export function AccountSignIn({ account }: { account: AgentAccount }) {
  return vendorInfo(account.vendor).signIn === "inApp" ? (
    <CodexAccountSignIn account={account} />
  ) : (
    <ClaudeAccountSignIn account={account} />
  );
}
