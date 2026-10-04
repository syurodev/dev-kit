import { useState } from "react";
import { useModuleVisibility } from "@/components/ui/module-visibility";
import { useShallow } from "zustand/react/shallow";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Plus, Server } from "lucide-react";
import { EmptyState } from "@/components/ui/empty-state";
import { Button } from "@/components/ui/button";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { FeedbackBanner } from "@/components/ui/feedback";
import { useSSHWorkspaceState } from "./workspaceState";
import {
  listSSHProfiles,
  deleteSSHProfile,
  type SSHProfile,
} from "@/bridge/ssh";
import { listEnvTags } from "@/bridge/environments";
import { errorMessage } from "@/bridge/errors";
import { EMPTY_ARRAY } from "@/lib/stableEmpty";
import { closeSSHConnection, renameSSHConnection } from "./documents";
import { useSSHStore } from "./sshStore";
import { ConnectionDialog } from "./ConnectionDialog";
import {
  ConnectionWorkspace,
  closeSSHConnectionTabs,
} from "./ConnectionWorkspace";

export function SSHView({ params }: { params?: Record<string, unknown> }) {
  const client = useQueryClient();
  const visible = useModuleVisibility();
  const { favorites, favorite, setShowNewConnection } = useSSHStore(useShallow(s => ({ favorites: s.favorites, favorite: s.favorite, setShowNewConnection: s.setShowNewConnection })));
  const [editing, setEditing] = useState<SSHProfile | null>(null);
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
  const profileId = typeof params?.profileId === "string" ? params.profileId : "";
  const selected = profiles.find(p => p.id === profileId);
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
      closeSSHConnection(deleting.id);
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
    <section className="flex h-full min-h-0 min-w-0 w-full flex-col text-body-13">

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
        ) : isPending ? (
          <EmptyState busy title="Loading connections" body="Fetching your saved SSH connections." />
        ) : profileError ? (
          <EmptyState icon={Server} tone="danger" title="Connections unavailable" body="Retry loading connections from the sidebar." />
        ) : profileId ? (
          <EmptyState icon={Server} tone="danger" title="Connection unavailable" body="This saved connection no longer exists. Open another connection from the sidebar." />
        ) : (
          <EmptyState icon={Server} title="Your servers, in one workspace" body="Save an SSH connection to open remote terminals, run commands and manage tunnels.">
            <Button onClick={() => setShowNewConnection(true)}>
              <Plus className="size-4" />
              New connection
            </Button>
          </EmptyState>
        )}
      {editing && (
        <ConnectionDialog
          profile={editing}
          visible={visible}
          envs={envs.map((env) => env.name)}
          onClose={() => setEditing(null)}
          onSaved={async (p) => {
            closeSSHConnectionTabs(p.id);
            useSSHWorkspaceState.getState().forget(p.id);
            renameSSHConnection(p);
            await refresh();
          }}
        />
      )}
      <ConfirmDialog open={Boolean(deleting) && visible} onClose={() => setDeleting(null)}
        title={`Delete ${deleting?.name ?? "connection"}?`} confirmLabel="Delete connection"
        pendingLabel="Deleting connection…" pending={busy} error={error} onConfirm={() => void remove()}>
        This removes the saved connection and stops its terminals and tunnels. This does not delete anything on the remote server.
      </ConfirmDialog>
    </section>
  );
}
