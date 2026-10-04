import { useEffect, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Plus, Search, Server, Star, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { FeedbackBanner, PendingStatus } from "@/components/ui/feedback";
import { useSSHWorkspaceState } from "./workspaceState";
import {
  listSSHProfiles,
  deleteSSHProfile,
  type SSHProfile,
} from "@/bridge/ssh";
import { listEnvTags } from "@/bridge/environments";
import { errorMessage } from "@/bridge/errors";
import { EMPTY_ARRAY } from "@/lib/stableEmpty";
import { cn } from "@/lib/utils";
import { useSSHStore } from "./sshStore";
import { ConnectionDialog } from "./ConnectionDialog";
import {
  ConnectionWorkspace,
  closeSSHConnectionTabs,
} from "./ConnectionWorkspace";

export function SSHView() {
  const client = useQueryClient();
  const { selectedId, openIds, favorites, select, close, favorite } =
    useSSHStore();
  const [search, setSearch] = useState("");
  const [editing, setEditing] = useState<SSHProfile | "new" | null>(null);
  const [deleting, setDeleting] = useState<SSHProfile | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const {
    data: profiles = EMPTY_ARRAY,
    isPending,
    error: profileError,
  } = useQuery({ queryKey: ["ssh", "profiles"], queryFn: listSSHProfiles });
  const { data: envs = EMPTY_ARRAY, error: envError } = useQuery({
    queryKey: ["environments", "tags"],
    queryFn: listEnvTags,
  });
  const selected = profiles.find((p) => p.id === selectedId);
  const opened = openIds.flatMap((id) => profiles.filter((p) => p.id === id));
  const initialized = useRef(false);
  useEffect(() => {
    if (!isPending && !profileError && !initialized.current) {
      initialized.current = true;
      if (!selectedId && profiles.length > 0) select(profiles[0].id);
    }
  }, [isPending, profileError, selectedId, profiles, select]);
  const query = search.trim().toLowerCase();
  const matches = profiles.filter((p) =>
    `${p.name} ${p.username} ${p.host} ${p.env}`.toLowerCase().includes(query),
  );
  const groups = [...new Set(matches.map((p) => p.env))].sort();
  async function refresh() {
    await client.invalidateQueries({ queryKey: ["ssh"] });
  }
  async function remove() {
    if (!deleting || busy) return;
    setBusy(true);
    setError("");
    try {
      await deleteSSHProfile(deleting.id);
      closeSSHConnectionTabs(deleting.id);
      close(deleting.id);
      useSSHWorkspaceState.getState().forget(deleting.id);
      if (favorites.includes(deleting.id)) favorite(deleting.id);
      setDeleting(null);
      await refresh();
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <section className="flex h-full min-h-0 w-full text-body-13">
      <aside
        aria-label="SSH connections"
        className="flex w-64 min-w-52 shrink-0 flex-col border-r border-border bg-surface-base"
      >
        <div className="flex items-center justify-between px-3 py-3">
          <h1 className="text-label-13">SSH connections</h1>
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label="New SSH connection"
            onClick={() => setEditing("new")}
          >
            <Plus className="size-4" />
          </Button>
        </div>
        <div className="relative mx-3 mb-3">
          <Search className="absolute left-2.5 top-2.5 size-3.5 text-text-tertiary" />
          <Input
            aria-label="Search SSH connections"
            className="pl-8"
            placeholder="Search name or host…"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
          />
        </div>
        <div className="min-h-0 flex-1 overflow-auto px-2 pb-3">
          {isPending && (
            <PendingStatus className="p-3">Loading connections…</PendingStatus>
          )}
          {profileError && (
            <FeedbackBanner tone="error" title="Could not load connections" action={<Button size="sm" variant="secondary" onClick={() => void refresh()}>Retry</Button>}>
              {errorMessage(profileError)}
            </FeedbackBanner>
          )}
          {!isPending && !profileError && matches.length === 0 && (
            <div className="p-3 text-body-12 text-text-tertiary">
              <p>{query ? "No matching connections." : "Add your first SSH connection."}</p>
              {query && <Button size="sm" variant="ghost" className="mt-2" onClick={() => setSearch("")}>Clear search</Button>}
            </div>
          )}
          {groups.map((env) => (
            <div key={env} className="mb-4">
              <h2 className="px-2 py-1 text-caption-11 uppercase text-text-tertiary">
                {env}
              </h2>
              {matches
                .filter((p) => p.env === env)
                .sort(
                  (a, b) =>
                    Number(favorites.includes(b.id)) -
                      Number(favorites.includes(a.id)) ||
                    a.name.localeCompare(b.name),
                )
                .map((p) => (
                  <div
                    key={p.id}
                    className={cn(
                      "flex items-center rounded-md",
                      p.id === selectedId
                        ? "bg-state-soft"
                        : "hover:bg-state-soft",
                    )}
                  >
                    <button
                      className="flex min-w-0 flex-1 items-center gap-2 px-2 py-2.5 text-left"
                      aria-pressed={p.id === selectedId}
                      onClick={() => select(p.id)}
                    >
                      <Server className="size-4 shrink-0 text-text-tertiary" />
                      <span className="min-w-0">
                        <span className="block truncate text-label-12">
                          {p.name}
                        </span>
                        <span className="block truncate text-caption-11 text-text-tertiary">
                          {p.username}@{p.host}
                        </span>
                      </span>
                    </button>
                    <Button
                      size="icon-xs"
                      variant="ghost"
                      aria-label={`${favorites.includes(p.id) ? "Unfavorite" : "Favorite"} ${p.name}`}
                      aria-pressed={favorites.includes(p.id)}
                      onClick={() => favorite(p.id)}
                    >
                      <Star
                        className={cn(
                          "size-3",
                          favorites.includes(p.id) &&
                            "fill-current text-status-warning",
                        )}
                      />
                    </Button>
                  </div>
                ))}
            </div>
          ))}
        </div>
        <div className="border-t border-border p-3 text-caption-11 text-text-tertiary">
          {profiles.length} connections · credentials encrypted locally
        </div>
      </aside>
      <main className="flex min-w-0 flex-1 flex-col">
        {opened.length > 0 && (
          <div
            aria-label="Open connections"
            className="flex shrink-0 gap-1 overflow-x-auto border-b border-border px-2 py-1.5"
          >
            {opened.map((p) => (
              <div
                key={p.id}
                className={cn(
                  "flex shrink-0 items-center rounded-md",
                  p.id === selectedId && "bg-state-soft",
                )}
              >
                <button
                  className="max-w-48 truncate px-3 py-1.5 text-label-12"
                  aria-pressed={p.id === selectedId}
                  onClick={() => select(p.id)}
                >
                  {p.name}
                </button>
                <Button
                  variant="ghost"
                  size="icon-xs"
                  aria-label={`Close connection ${p.name}`}
                  onClick={() => close(p.id)}
                >
                  <X className="size-3" />
                </Button>
              </div>
            ))}
          </div>
        )}
        {envError && (
          <FeedbackBanner tone="error" title="Could not load environments" className="m-3" action={<Button size="sm" variant="secondary" onClick={() => void client.invalidateQueries({ queryKey: ["environments", "tags"] })}>Retry</Button>}>
            {errorMessage(envError)}
          </FeedbackBanner>
        )}
        {selected ? (
          <ConnectionWorkspace
            key={`${selected.id}:${selected.host}:${selected.port}:${selected.username}:${selected.env}`}
            profile={selected}
            onEdit={() => setEditing(selected)}
            onDelete={() => {
              setError("");
              setDeleting(selected);
            }}
          />
        ) : (
          <div className="flex flex-1 flex-col items-center justify-center gap-3 p-6">
            <Server className="size-10 text-text-tertiary" />
            <h2 className="text-label-14">Your servers, in one workspace</h2>
            <p className="max-w-sm text-center text-body-13 text-text-secondary">
              Save an SSH connection to open remote terminals, run commands and
              manage tunnels.
            </p>
            <Button onClick={() => setEditing("new")}>
              <Plus className="size-4" />
              New connection
            </Button>
          </div>
        )}
      </main>
      {editing && (
        <ConnectionDialog
          profile={editing === "new" ? undefined : editing}
          envs={envs.map((env) => env.name)}
          onClose={() => setEditing(null)}
          onSaved={async (p) => {
            if (editing !== "new") {
              closeSSHConnectionTabs(p.id);
              useSSHWorkspaceState.getState().forget(p.id);
            }
            select(p.id);
            await refresh();
          }}
        />
      )}
      <ConfirmDialog open={Boolean(deleting)} onClose={() => setDeleting(null)}
        title={`Delete ${deleting?.name ?? "connection"}?`} confirmLabel="Delete connection"
        pendingLabel="Deleting connection…" pending={busy} error={error} onConfirm={() => void remove()}>
        This removes the saved connection and stops its terminals and tunnels. This does not delete anything on the remote server.
      </ConfirmDialog>
    </section>
  );
}
