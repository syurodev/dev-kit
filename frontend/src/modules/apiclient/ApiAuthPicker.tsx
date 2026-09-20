import { useCallback, useEffect, useState } from "react";
import { KeyRound } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  accountsForHost,
  chooseAccountForHost,
  forgetHostChoice,
  hostOf,
  type Account,
  type HostMatch,
} from "@/bridge/accounts";
import { resolveUrl, saveRequest, type ApiRequest } from "@/bridge/apiclient";

/**
 * Which account this request authenticates with.
 *
 * Two accounts covering one host is a question, never a guess: two GitHub
 * accounts are usually a personal one and a company one, and sending a request
 * as the wrong identity is worse than asking once. The answer is remembered
 * per (host, environment) — the same host on prod is a different question, and
 * that is the ordinary case rather than an edge one.
 *
 * The host is read from the URL with the environment's variables filled in, so
 * the question is about the server the request would reach rather than about
 * the template. An unfilled variable means there is no host to ask about yet,
 * and saying so is better than matching a credential to `{{baseUrl}}`.
 */

// The presentation follows the account, not a default: a basic account sent as
// a bearer token is a 401 that looks like a bad credential.
function kindFor(account: Account): ApiRequest["auth"]["kind"] {
  return account.kind === "basic" ? "basic" : "bearer";
}

export function ApiAuthPicker({
  request,
  env,
  vars,
  onSaved,
}: {
  request: ApiRequest;
  /** The environment tag, which is half the key the answer is stored under. */
  env: string;
  vars: Record<string, string>;
  onSaved: () => void;
}) {
  const [match, setMatch] = useState<HostMatch | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const host = hostOf(resolveUrl(request.url, vars));

  const load = useCallback(() => {
    if (!host) {
      setMatch(null);
      return;
    }
    accountsForHost(host, env)
      .then(setMatch)
      .catch((e: Error) => setError(e.message));
  }, [host, env]);

  useEffect(load, [load]);

  function bind(account: Account, remember: boolean) {
    setBusy(true);
    setError("");
    const work = remember ? chooseAccountForHost(host, env, account.id) : Promise.resolve();
    work
      .then(() =>
        saveRequest({
          ...request,
          auth: { kind: kindFor(account), accountId: account.id, header: request.auth.header },
        }),
      )
      .then(() => {
        load();
        onSaved();
      })
      .catch((e: Error) => setError(e.message))
      .finally(() => setBusy(false));
  }

  if (!host) {
    return null;
  }
  if (error) {
    return <p className="pt-1 text-xs text-red-500">{error}</p>;
  }
  if (!match) {
    return null;
  }

  const options = match.options ?? [];
  if (options.length === 0) {
    return (
      <p className="pt-1 text-xs text-fg-muted">
        No saved account covers {host}.
      </p>
    );
  }

  const bound = options.find((option) => option.id === request.auth.accountId) ?? null;

  if (!match.decided) {
    return (
      <div className="flex flex-wrap items-center gap-2 pt-1 text-xs">
        <KeyRound className="size-3.5 text-amber-500" aria-hidden />
        <span className="text-amber-500">
          {options.length} accounts cover {host}. Choose one — DevKit will not guess.
        </span>
        {options.map((option) => (
          <Button
            key={option.id}
            variant="ghost"
            size="sm"
            disabled={busy}
            onClick={() => bind(option, true)}
          >
            {option.label}
          </Button>
        ))}
      </div>
    );
  }

  return (
    <div className="flex flex-wrap items-center gap-2 pt-1 text-xs text-fg-muted">
      <KeyRound className="size-3.5" aria-hidden />
      {bound ? (
        <span>
          Authenticates as {bound.label}
          {match.remembered ? ` · remembered for ${host} on ${env || "no environment"}` : ""}
        </span>
      ) : (
        <>
          <span>
            {match.account.label} is the {options.length === 1 ? "only" : "chosen"} account for{" "}
            {host}, and this request is not using it.
          </span>
          <Button variant="ghost" size="sm" disabled={busy} onClick={() => bind(match.account, false)}>
            Use it
          </Button>
        </>
      )}
      {options.length > 1 ? (
        <Button
          variant="ghost"
          size="sm"
          disabled={busy}
          title="Ask again which account covers this host on this environment"
          onClick={() => {
            setBusy(true);
            forgetHostChoice(host, env)
              .then(load)
              .catch((e: Error) => setError(e.message))
              .finally(() => setBusy(false));
          }}
        >
          Change
        </Button>
      ) : null}
    </div>
  );
}
