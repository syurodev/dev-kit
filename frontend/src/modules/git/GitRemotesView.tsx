import { gitUI, useGitBusy, useGitOperation } from "./gitUI";
import { useModuleFolder } from "@/stores/workFolder";
import { bumpGitRevision, useGitRevision } from "./gitRevision";
import { FeedbackBanner, PendingStatus } from "@/components/ui/feedback";
import { useCallback, useEffect, useState } from "react";
import { Cloud, Download, RefreshCw, Upload } from "lucide-react";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import {
  bindRemote,
  gitFetch,
  gitPull,
  gitPush,
  gitRemotes,
  gitStatus,
  type GitStatus,
  type Remote,
} from "@/bridge/git";
import { isLocalAccount, listAccounts, type Account } from "@/bridge/accounts";
import { accountsForRemote } from "./remotes";
import { listEnvTags, type EnvTag } from "@/bridge/environments";

/**
 * Which account authenticates to which remote, and the three actions that
 * use it.
 *
 * `GitBindRemote` shipped with the module and had no caller, which made the
 * whole credential path unreachable: a push resolved nothing and fell back to
 * git's own configuration every time. Binding is still OPTIONAL and that is
 * the default — an unbound remote is authenticated by ssh keys or a
 * credential helper, exactly as it was before DevKit.
 *
 * Fetch is here rather than only on the Changes view because it is how you
 * find out whether a binding works: it goes over the network with the same
 * credential a push would use, and changes nothing if it is wrong.
 */

// Radix reserves the empty string for "nothing selected", so "no binding"
// needs a value of its own.
const NONE = "__none__";

export function GitRemotesView() {
  const [remotes, setRemotes] = useState<Remote[] | null>(null);
  const [accounts, setAccounts] = useState<Account[] | null>(null);
  const [tags, setTags] = useState<EnvTag[] | null>(null);
  const [status, setStatus] = useState<GitStatus | null>(null);
  const { folder } = useModuleFolder("git");
  const busy = useGitBusy();
  const operation = useGitOperation(folder);
  const revision = useGitRevision((state) => state.revision);
  const [error, setError] = useState("");

  const reload = useCallback(() => {
    gitRemotes()
      .then((next) => {
        setRemotes(next);
        setError("");
      })
      .catch((e: Error) => {
        setError(e.message);
      });
    gitStatus()
      .then(setStatus)
      .catch(() => setStatus(null));
  }, []);

  useEffect(() => {
    // A connection's scoped account belongs to that connection, never to a
    // remote, so it is not offered here. A local one — an SSH key kept on this
    // machine — is the person's own and is.
    listAccounts()
      .then((all) => setAccounts(all.filter((account) => account.scopeOwner === "" || isLocalAccount(account))))
      .catch(() => setAccounts([]));
    listEnvTags()
      .then(setTags)
      .catch(() => setTags([]));
  }, [reload]);

  useEffect(reload, [reload, revision]);

  function act(key: string, work: () => Promise<unknown>, done: string) {
    setError("");
    void gitUI.run(
      folder,
      `Updating ${key}…`,
      async () => {
        await work();
        bumpGitRevision();
      },
      done,
    );
  }

  if (!remotes || !accounts || !tags) {
    return (
      <div className="p-4">
        {error ? (
          <FeedbackBanner
            tone="error"
            title="Could not read remotes"
            action={
              <Button variant="outline" onClick={reload}>
                Retry
              </Button>
            }
          >
            {error}
          </FeedbackBanner>
        ) : (
          <PendingStatus>Reading remotes…</PendingStatus>
        )}
      </div>
    );
  }
  if (remotes.length === 0) {
    return (
      <EmptyState
        icon={Cloud}
        title="No remotes"
        body={
          error ||
          "This repository has no remote configured. Add one with git remote add, then bind it to an account here."
        }
      />
    );
  }

  const branch = status?.branch ?? "";

  return (
    <div className="flex h-full min-h-0 flex-col">
      <header className="flex items-center gap-2 border-b border-border-subtle px-3 py-2">
        <Cloud className="size-4 text-text-tertiary" aria-hidden />
        <h2 className="text-body-13">Remotes ({remotes.length})</h2>
        <div className="flex-1" />
        <Button variant="ghost" size="icon" aria-label="Reload" onClick={reload}>
          <RefreshCw className="size-4" aria-hidden />
        </Button>
      </header>

      <div className="min-h-0 flex-1 overflow-auto">
        {remotes.map((remote) => (
          <section key={remote.name} className="border-b border-border-subtle px-3 py-2">
            <div className="flex items-baseline gap-2">
              <span className="text-body-13 font-medium">{remote.name}</span>
              <span className="min-w-0 flex-1 truncate font-mono text-code-12 text-text-tertiary">{remote.url}</span>
            </div>

            <div className="flex flex-wrap items-center gap-2 pt-2">
              <Select
                disabled={busy}
                value={remote.accountId || NONE}
                onValueChange={(value) =>
                  act(
                    remote.name,
                    () => bindRemote(remote.name, value === NONE ? "" : value, remote.env).then(() => undefined),
                    value === NONE ? `${remote.name} uses git's own configuration` : `${remote.name} bound`,
                  )
                }
              >
                <SelectTrigger size="sm" aria-label={`Account for ${remote.name}`}>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value={NONE}>No account · git's own configuration</SelectItem>
                  {accountsForRemote(accounts, remote).map((account) => (
                    <SelectItem key={account.id} value={account.id}>
                      {account.label} · {account.kind}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>

              <Select
                disabled={busy}
                value={remote.env || NONE}
                onValueChange={(value) =>
                  act(
                    remote.name,
                    () => bindRemote(remote.name, remote.accountId, value === NONE ? "" : value).then(() => undefined),
                    value === NONE ? `${remote.name} is untagged` : `${remote.name} tagged ${value}`,
                  )
                }
              >
                <SelectTrigger size="sm" aria-label={`Environment for ${remote.name}`}>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value={NONE}>No environment</SelectItem>
                  {tags.map((tag) => (
                    <SelectItem key={tag.id} value={tag.name}>
                      {tag.name}
                      {tag.requiresConfirmation ? " · asks before push" : ""}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>

              <div className="flex-1" />

              <Button
                variant="ghost"
                size="sm"
                disabled={busy}
                title="Goes over the network with this remote's credential and changes nothing — the way to find out whether a binding works"
                onClick={() => act(remote.name, () => gitFetch(remote.name), `Fetched ${remote.name}`)}
              >
                <Download className="size-4" aria-hidden />
                Fetch
              </Button>
              <Button
                variant="ghost"
                size="sm"
                disabled={busy}
                title="Fast-forward only"
                onClick={() => act(remote.name, () => gitPull(remote.name), `Pulled ${remote.name}`)}
              >
                Pull
              </Button>
              <Button
                size="sm"
                disabled={busy || branch === ""}
                onClick={() =>
                  act(
                    remote.name,
                    // Without an upstream the push has to set one, or the
                    // branch is pushed and still reports as untracked.
                    () => gitPush(remote.name, branch, !status?.upstream),
                    `Pushed ${branch} to ${remote.name}`,
                  )
                }
              >
                <Upload className="size-4" aria-hidden />
                Push {branch || "…"}
              </Button>
            </div>
          </section>
        ))}
      </div>

      <footer className="border-t border-border-subtle px-3 py-2 text-code-12">
        {busy && <PendingStatus>{operation.label || "Updating repository…"}</PendingStatus>}
        {error || operation.error ? (
          <FeedbackBanner tone="error" title="Git action failed">
            {error || operation.error}
          </FeedbackBanner>
        ) : operation.notice ? (
          <FeedbackBanner tone="success" title={operation.notice} />
        ) : (
          <span className="text-text-tertiary">
            An unbound remote uses git's own configuration. An environment that asks requires confirmation before each
            push.
          </span>
        )}
      </footer>
    </div>
  );
}
