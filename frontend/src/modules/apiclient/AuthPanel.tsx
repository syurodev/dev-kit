import { useEffect, useState } from "react";
import { KeyRound, ExternalLink, RefreshCw } from "lucide-react";
import { accountsForHost, hostOf, listAccounts, type Account, type HostMatch } from "@/bridge/accounts";
import { showModuleHome } from "@/workbench/showModule";
import { useModuleVisibility } from "@/components/ui/module-visibility";
import { errorMessage } from "@/bridge/errors";
import type { Auth } from "@/bridge/apiclient";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { NativeSelect, NativeSelectOptGroup, NativeSelectOption } from "@/components/ui/native-select";

/**
 * The Auth tab. There is no field for a token on purpose: a request binds an
 * account from Accounts, and the value is resolved inside Go when it is sent —
 * never stored on the request, never in Copy as cURL, never across the bridge.
 * Rotating the token is then a one-place edit that leaves every request
 * working, which is the trade Postman's per-request token field cannot make.
 *
 * Accounts that cover the request's host are offered first. Two accounts for
 * one host is a question, never a guess: the suggestion is only made when
 * Accounts has an answer for the host (it is the only one, or it was chosen
 * before).
 */

const KIND_LABEL: Record<Auth["kind"], string> = {
  "": "No auth",
  bearer: "Bearer token",
  basic: "Basic auth",
  header: "API key header",
};

// The presentation follows the account, not a default: a basic account sent
// as a bearer token is a 401 that looks like a bad credential.
function kindFor(account: Account, current: Auth["kind"]): Auth["kind"] {
  if (account.kind === "basic") return "basic";
  return current === "header" ? "header" : "bearer";
}

export function AuthPanel({
  auth,
  onChange,
  resolvedUrl,
  env,
}: {
  auth: Auth;
  onChange: (auth: Auth) => void;
  /** The URL with plain variables filled, for working out the host. */
  resolvedUrl: string;
  /** The environment's tag — half the key a host answer is stored under. */
  env: string;
}) {
  const visible = useModuleVisibility();
  const [refresh, setRefresh] = useState(0);
  const [accounts, setAccounts] = useState<Account[] | null>(null);
  const [answer, setAnswer] = useState<{ host: string; env: string; value: HostMatch } | null>(null);
  const [readError, setReadError] = useState("");
  const host = hostOf(resolvedUrl);

  useEffect(() => {
    if (!visible) return;
    let active = true;
    setReadError("");
    listAccounts().then((items) => { if (active) setAccounts(items); })
      .catch((cause) => { if (active) setReadError(errorMessage(cause)); });
    return () => { active = false; };
  }, [visible, refresh]);

  useEffect(() => {
    if (!host || !visible) return;
    let active = true;
    // URL typing can change the host repeatedly; only ask for the settled one.
    const timer = setTimeout(() => {
      accountsForHost(host, env)
        .then((value) => { if (active) setAnswer({ host, env, value }); })
        .catch(() => { if (active) setAnswer(null); });
    }, 150);
    return () => { active = false; clearTimeout(timer); };
  }, [host, env, visible, refresh]);

  // A new host must never display the old host's account, even before the effect runs.
  const match = answer?.host === host && answer.env === env ? answer.value : null;

  const matching = new Set((match?.options ?? []).map((account) => account.id));
  const usable = accounts ?? [];
  const forHost = usable.filter((account) => matching.has(account.id));
  const others = usable.filter((account) => !matching.has(account.id));
  const bound = usable.find((account) => account.id === auth.accountId) ?? null;
  const suggestion = match?.decided && match.account.id !== auth.accountId ? match.account : null;

  return (
    <div className="flex max-w-xl flex-col gap-4 rounded-lg border border-border-subtle bg-surface-base p-4 text-body-13">
      <div className="flex flex-wrap items-center gap-2"><h3 className="flex-1 text-body-13-medium text-text-primary">Authentication</h3><Button variant="ghost" size="icon-xs" onClick={() => setRefresh((value) => value + 1)} aria-label="Refresh accounts"><RefreshCw /></Button><Button variant="ghost" size="xs" onClick={() => showModuleHome("accounts")}><ExternalLink />Manage accounts</Button></div>
      <label className="flex flex-wrap items-center gap-3">
        <span className="w-24 shrink-0 text-text-secondary">Type</span>
        <NativeSelect
          size="sm"
          value={auth.kind}
          aria-label="Auth type"
          onChange={(event) => {
            const kind = event.target.value as Auth["kind"];
            onChange(kind === "" ? { kind: "", accountId: "" } : { ...auth, kind });
          }}
        >
          {(Object.keys(KIND_LABEL) as Auth["kind"][]).map((kind) => (
            <NativeSelectOption key={kind} value={kind}>
              {KIND_LABEL[kind]}
            </NativeSelectOption>
          ))}
        </NativeSelect>
      </label>

      {auth.kind !== "" ? (
        <>
          <label className="flex flex-wrap items-center gap-3">
            <span className="w-24 shrink-0 text-text-secondary">Account</span>
            <NativeSelect
              size="sm"
              value={auth.accountId}
              aria-label="Account"
              disabled={!accounts}
              onChange={(event) => {
                const account = usable.find((candidate) => candidate.id === event.target.value);
                onChange({ ...auth, accountId: event.target.value, kind: account ? kindFor(account, auth.kind) : auth.kind });
              }}
            >
              <NativeSelectOption value="">{accounts ? "Choose an account…" : readError ? "Accounts unavailable" : "Loading accounts…"}</NativeSelectOption>
              {forHost.length ? (
                <NativeSelectOptGroup label={`Covers ${host}`}>
                  {forHost.map((account) => (
                    <NativeSelectOption key={account.id} value={account.id}>
                      {account.label}
                    </NativeSelectOption>
                  ))}
                </NativeSelectOptGroup>
              ) : null}
              {others.length ? (
                <NativeSelectOptGroup label={forHost.length ? "Other accounts" : "Accounts"}>
                  {others.map((account) => (
                    <NativeSelectOption key={account.id} value={account.id}>
                      {account.label}
                    </NativeSelectOption>
                  ))}
                </NativeSelectOptGroup>
              ) : null}
            </NativeSelect>
          </label>
          {auth.kind === "header" ? (
            <label className="flex flex-wrap items-center gap-3">
              <span className="w-24 shrink-0 text-text-secondary">Header</span>
              <Input
                inputSize="sm"
                value={auth.header ?? ""}
                placeholder="X-API-Key"
                onChange={(event) => onChange({ ...auth, header: event.target.value })}
                className="font-mono"
              />
            </label>
          ) : null}
        </>
      ) : null}

      {readError ? <p role="alert" className="text-caption-11 text-status-danger">{readError}</p> : null}
      {suggestion ? (
        <div className="flex flex-wrap items-center gap-2 text-caption-11 text-text-secondary">
          <KeyRound className="size-(--icon-12)" aria-hidden />
          <span>
            {suggestion.label} is the {match?.remembered ? "chosen" : "only"} account for {host}.
          </span>
          <Button
            variant="ghost"
            size="xs"
            onClick={() => onChange({ kind: kindFor(suggestion, auth.kind), accountId: suggestion.id, header: auth.header })}
          >
            Use it
          </Button>
        </div>
      ) : null}
      {match && !match.decided && (match.options?.length ?? 0) > 1 && !bound ? (
        <p className="text-caption-11 text-status-warning">
          {match.options?.length} accounts cover {host}. Choose one — DevKit will not guess.
        </p>
      ) : null}
      {accounts && accounts.length === 0 && auth.kind !== "" ? (
        <p className="text-caption-11 text-text-tertiary">
          No accounts yet. Add the token in Accounts, then bind it here.
        </p>
      ) : null}
      <p className="text-caption-11 text-text-tertiary">
        The credential is resolved inside DevKit when the request is sent. It is never stored on the request,
        never shown here, and Copy as cURL writes <code className="font-mono">$DEVKIT_TOKEN</code> in its place.
        A token typed into a header row works too, but lives on the request.
      </p>
    </div>
  );
}
