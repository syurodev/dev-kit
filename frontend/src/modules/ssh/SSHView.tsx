import { useEffect, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Plus, Server, Star, X } from "lucide-react";
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs";
import { EmptyState } from "@/components/ui/empty-state";
import { Empty, EmptyHeader, EmptyTitle, EmptyDescription, EmptyContent } from "@/components/ui/empty";
import { ActionTooltip } from "@/components/ui/action-tooltip";
import { Button } from "@/components/ui/button";
import { useSidebarContent, SidebarHeader, SidebarSearch, SidebarSection } from "@/components/ui/sidebar-shell";
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
  const openTabsRef = useRef<HTMLDivElement>(null);
  const searchRef = useRef<HTMLInputElement>(null);
  function closeConnection(id: string) {
    close(id);
    requestAnimationFrame(() => {
      const active = openTabsRef.current?.querySelector<HTMLElement>('[role="tab"][aria-selected="true"]');
      (active ?? searchRef.current)?.focus();
    });
  }
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
  useSidebarContent(
      <div
        aria-label="SSH connections"
        className="flex h-full min-h-0 flex-col text-body-13"
      >
        <SidebarHeader>
          <SidebarSearch ref={searchRef} aria-label="Search SSH connections"
            placeholder="Search name or host…" value={search} onChange={setSearch} onClear={() => setSearch("")} />
          <ActionTooltip label="New SSH connection"><Button variant="ghost" size="icon-sm" onClick={() => setEditing("new")}><Plus className="size-4" /></Button></ActionTooltip>
        </SidebarHeader>
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
            <Empty className="p-3">
              <EmptyHeader><EmptyTitle>{query ? "No matching connections" : "No saved connections"}</EmptyTitle><EmptyDescription>{query ? "Try another name, host or environment." : "Create a connection to get started."}</EmptyDescription></EmptyHeader>
              <EmptyContent><Button size="sm" variant="secondary" onClick={() => query ? setSearch("") : setEditing("new")}>{query ? "Clear search" : "New connection"}</Button></EmptyContent>
            </Empty>
          )}
          {groups.map((env, index) => (
            <div key={env} className="mb-4">
              <SidebarSection label={env} count={matches.filter((p) => p.env === env).length} first={index === 0} />
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
                    <Button variant="ghost"
                      className="h-auto min-w-0 flex-1 justify-start gap-2 px-2 py-2.5 text-left"
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
                    </Button>
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
      </div>
  );
  return (
    <section className="flex h-full min-h-0 min-w-0 w-full text-body-13">

      <Tabs value={selectedId} onValueChange={select} className="min-h-0 min-w-0 flex-1 gap-0">
        {opened.length > 0 && (
          <div className="shrink-0 overflow-x-auto border-b border-border px-2 py-1.5">
            <TabsList ref={openTabsRef} aria-label="Open connections" variant="line" className="justify-start gap-1">
              {opened.map((p) => (
                <div key={p.id} className="flex shrink-0 items-center rounded-md">
                  <TabsTrigger value={p.id} className="max-w-48 px-3 text-label-12" onKeyDown={(event) => {
                    if (event.key === "Delete") { event.preventDefault(); closeConnection(p.id); }
                  }}><span className="truncate">{p.name}</span></TabsTrigger>
                  <ActionTooltip label={`Close connection ${p.name}`}>
                    <Button variant="ghost" size="icon-xs" onClick={() => closeConnection(p.id)}><X className="size-3" /></Button>
                  </ActionTooltip>
                </div>
              ))}
            </TabsList>
          </div>
        )}
        {envError && (
          <FeedbackBanner tone="error" title="Could not load environments" className="m-3" action={<Button size="sm" variant="secondary" onClick={() => void client.invalidateQueries({ queryKey: ["environments", "tags"] })}>Retry</Button>}>
            {errorMessage(envError)}
          </FeedbackBanner>
        )}
        {selected ? (
          <TabsContent value={selected.id} className="min-h-0 flex-1 flex flex-col">
          <ConnectionWorkspace
            key={`${selected.id}:${selected.host}:${selected.port}:${selected.username}:${selected.env}`}
            profile={selected}
            onEdit={() => setEditing(selected)}
            onDelete={() => {
              setError("");
              setDeleting(selected);
            }}
          />
          </TabsContent>
        ) : isPending ? (
          <EmptyState busy title="Loading connections" body="Fetching your saved SSH connections." />
        ) : profileError ? (
          <EmptyState icon={Server} tone="danger" title="Connections unavailable" body="Retry loading connections from the sidebar." />
        ) : (
          <EmptyState icon={Server} title="Your servers, in one workspace" body="Save an SSH connection to open remote terminals, run commands and manage tunnels.">
            <Button onClick={() => setEditing("new")}>
              <Plus className="size-4" />
              New connection
            </Button>
          </EmptyState>
        )}
      </Tabs>
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
