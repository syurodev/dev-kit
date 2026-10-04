import { useState } from "react";
import { useShallow } from "zustand/react/shallow";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Plus, Server, Star } from "lucide-react";
import { SidebarHeader, SidebarSearch, SidebarSection } from "@/components/ui/sidebar-shell";
import { Button } from "@/components/ui/button";
import { ActionTooltip } from "@/components/ui/action-tooltip";
import { Empty, EmptyHeader, EmptyTitle, EmptyDescription, EmptyContent } from "@/components/ui/empty";
import { FeedbackBanner, PendingStatus } from "@/components/ui/feedback";
import { listSSHProfiles } from "@/bridge/ssh";
import { listEnvTags } from "@/bridge/environments";
import { errorMessage } from "@/bridge/errors";
import { EMPTY_ARRAY } from "@/lib/stableEmpty";
import { cn } from "@/lib/utils";
import { useWorkbenchTabsStore } from "@/stores/workbenchTabs";
import { useSSHStore } from "./sshStore";
import { openSSHConnection, activeSSHProfileId } from "./documents";
import { ConnectionDialog } from "./ConnectionDialog";

export function SSHSidebar() {
  const client = useQueryClient();
  const { favorites, favorite, showNewConnection, setShowNewConnection } = useSSHStore(useShallow(s => ({ favorites: s.favorites, favorite: s.favorite, showNewConnection: s.showNewConnection, setShowNewConnection: s.setShowNewConnection })));
  const selectedId = useWorkbenchTabsStore(activeSSHProfileId);
  const visible = useWorkbenchTabsStore(state => state.activeModuleId === "ssh");
  const [search, setSearch] = useState("");
  const { data: profiles = EMPTY_ARRAY, isPending, error: profileError } = useQuery({ queryKey: ["ssh", "profiles"], queryFn: listSSHProfiles });
  const { data: envs = EMPTY_ARRAY } = useQuery({ queryKey: ["environments", "tags"], queryFn: listEnvTags });
  const query = search.trim().toLowerCase();
  const matches = profiles.filter(p => `${p.name} ${p.username} ${p.host} ${p.env}`.toLowerCase().includes(query));
  const groups = [...new Set(matches.map(p => p.env))].sort();
  async function refresh() { await client.invalidateQueries({ queryKey: ["ssh"] }); }
  return <>
      <div
        aria-label="SSH connections"
        className="flex h-full min-h-0 flex-col text-body-13"
      >
        <SidebarHeader>
          <SidebarSearch aria-label="Search SSH connections"
            placeholder="Search name or host…" value={search} onChange={setSearch} onClear={() => setSearch("")} />
          <ActionTooltip label="New SSH connection"><Button variant="ghost" size="icon-sm" onClick={() => setShowNewConnection(true)}><Plus className="size-4" /></Button></ActionTooltip>
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
              <EmptyContent><Button size="sm" variant="secondary" onClick={() => query ? setSearch("") : setShowNewConnection(true)}>{query ? "Clear search" : "New connection"}</Button></EmptyContent>
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
                      onClick={() => openSSHConnection(p)}
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
    {showNewConnection && <ConnectionDialog visible={visible} envs={envs.map(env => env.name)} onClose={() => setShowNewConnection(false)} onSaved={async profile => {
      openSSHConnection(profile);
      await refresh();
    }} />}
  </>;
}
