import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Check, CircleDashed, LogOut, TriangleAlert } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { CODEX_VENDOR } from "@/bridge/agenthub";
import { listAccounts, type Account } from "@/bridge/accounts";
import {
  agentHubSignInStatus,
  agentHubSignOut,
  getCodexApiKeyAccount,
  saveCodexApiKeyAccount,
  signInStatusLine,
} from "@/bridge/agentSignIn";
import { errorMessage } from "@/bridge/errors";
import { CodexSignIn, agentSignInStatusQueryKey, useCodexSignIn } from "./CodexSignIn";

const codexApiKeyQueryKey = ["codexApiKeyAccount"] as const;

/** The Select's value for "no API key": Radix refuses an empty item value. */
export const CHATGPT_CHOICE = "chatgpt";

/**
 * The accounts that can be Codex's API key: a token, shared rather than one
 * connection's secret, and not limited to an environment — a session has none
 * to offer, so the backend would refuse it at every spawn.
 */
export function codexApiKeyChoices(accounts: Account[]): Account[] {
  return accounts.filter((a) => a.kind === "token" && !a.scopeOwner && a.env.length === 0);
}

/**
 * The Select's label for a chosen id the list of usable tokens does not hold,
 * or null when it holds it. Without an item for it the Select shows nothing —
 * a blank box reads as "no key", which is the one thing it is not.
 */
export function chosenKeyLabel(
  chosenId: string,
  tokens: Account[],
  accounts: { pending: boolean; error: boolean; all: Account[] },
): string | null {
  if (!chosenId || tokens.some((a) => a.id === chosenId)) return null;
  if (accounts.pending) return "Loading…";
  if (accounts.error) return "The chosen account";
  const chosen = accounts.all.find((a) => a.id === chosenId);
  return chosen ? `${chosen.label} · can't be used` : "Missing account";
}

/**
 * Settings → Agents → Codex: who Codex signs in as, and how. Kept beside the
 * sign-in it wraps, as Claude's is (ClaudeSignInSection).
 *
 * Two ways, and the API key wins when one is chosen: Codex's own ChatGPT
 * sign-in, kept in the Codex home only DevKit uses, or a token from Accounts,
 * chosen here by id and resolved when a session starts. Signing out of ChatGPT
 * and unlinking the key are different buttons because they are different
 * things — one clears Codex's home, the other only this choice.
 */
export function CodexAccountSection() {
  const client = useQueryClient();
  const status = useQuery({
    queryKey: agentSignInStatusQueryKey(CODEX_VENDOR),
    // Each read starts a short-lived Codex in the jail: once per visit.
    queryFn: () => agentHubSignInStatus(CODEX_VENDOR),
    staleTime: 60_000,
  });
  const chosen = useQuery({ queryKey: codexApiKeyQueryKey, queryFn: getCodexApiKeyAccount });
  const accounts = useQuery({ queryKey: ["accounts"], queryFn: listAccounts });
  const signIn = useCodexSignIn();

  const refresh = () => {
    void client.invalidateQueries({ queryKey: agentSignInStatusQueryKey(CODEX_VENDOR) });
    void client.invalidateQueries({ queryKey: codexApiKeyQueryKey });
  };
  const choose = useMutation({ mutationFn: saveCodexApiKeyAccount, onSuccess: refresh });
  const signOut = useMutation({ mutationFn: () => agentHubSignOut(CODEX_VENDOR), onSuccess: refresh });

  const tokens = codexApiKeyChoices(accounts.data ?? []);
  const chosenId = chosen.data ?? "";
  const chosenAccount = (accounts.data ?? []).find((a) => a.id === chosenId);
  const extraLabel = chosenKeyLabel(chosenId, tokens, {
    pending: accounts.isPending,
    error: accounts.isError,
    all: accounts.data ?? [],
  });
  // An id with no usable account behind it: deleted on another device, or
  // changed since (an environment restriction added, say).
  const chosenUnusable = extraLabel !== null && accounts.isSuccess;
  const s = status.data;
  const onChatGPT = Boolean(s?.signedIn && s.method !== "apiKey");

  return (
    <div data-testid="codex-account" className="flex flex-col gap-3">
      {status.isPending ? (
        <Skeleton className="h-5 w-56 rounded-control" />
      ) : status.isError ? (
        <p className="text-body-12 text-status-danger">{errorMessage(status.error)}</p>
      ) : s ? (
        <div className="flex flex-wrap items-center gap-2">
          <span data-testid="codex-account-status" className="flex items-center gap-1.5 text-body-13 text-text-primary">
            {s.signedIn ? (
              <Check className="size-4 shrink-0 text-status-success" />
            ) : (
              <CircleDashed className="size-4 shrink-0 text-text-tertiary" />
            )}
            {signInStatusLine(s, chosenAccount?.label)}
          </span>
          {onChatGPT ? (
            <Button
              type="button"
              variant="ghost"
              size="sm"
              disabled={signOut.isPending}
              data-testid="codex-sign-out"
              onClick={() => signOut.mutate()}
            >
              <LogOut />
              Sign out
            </Button>
          ) : null}
        </div>
      ) : null}
      {signOut.error ? <p className="text-body-12 text-status-danger">{errorMessage(signOut.error)}</p> : null}

      {/* A failing key is fixed below, not by a sign-in: the key wins. */}
      {s && !s.signedIn && s.method !== "apiKey" ? <CodexSignIn signIn={signIn} /> : null}

      <label className="flex flex-col gap-1">
        <span className="text-caption-11 text-text-tertiary">API key instead of ChatGPT</span>
        <Select
          value={chosenId || CHATGPT_CHOICE}
          disabled={choose.isPending || chosen.isPending}
          onValueChange={(value) => choose.mutate(value === CHATGPT_CHOICE ? "" : value)}
        >
          <SelectTrigger aria-label="codex-api-key" className="w-full">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={CHATGPT_CHOICE}>None — use the ChatGPT sign-in</SelectItem>
            {tokens.map((a) => (
              <SelectItem key={a.id} value={a.id}>
                {`${a.label} · ${a.provider}`}
              </SelectItem>
            ))}
            {extraLabel !== null ? <SelectItem value={chosenId}>{extraLabel}</SelectItem> : null}
          </SelectContent>
        </Select>
        <span className="text-caption-11 text-text-tertiary">
          A token from Accounts. Codex gets it when a session starts; it is never written to
          Codex&apos;s home. Add one in Accounts first.
        </span>
      </label>
      {chosenUnusable ? (
        <p className="flex items-center gap-1.5 text-body-12 text-status-warning">
          <TriangleAlert className="size-4 shrink-0" />
          {chosenAccount
            ? "The chosen API key can't be used: it must be a token with no environment restriction. Choose another, or go back to ChatGPT."
            : "The chosen API key is no longer in Accounts. Choose another, or go back to ChatGPT."}
        </p>
      ) : null}
      {choose.error ? <p className="text-body-12 text-status-danger">{errorMessage(choose.error)}</p> : null}
    </div>
  );
}
