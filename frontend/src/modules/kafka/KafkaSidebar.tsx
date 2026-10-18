import { BrandIcon } from "@/components/brand-icon/BrandIcon";
import { notify } from "@/lib/notify";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { FeedbackBanner, PendingStatus } from "@/components/ui/feedback";
import { ActionTooltip } from "@/components/ui/action-tooltip";
import { errorMessage } from "@/bridge/errors";
import { showModuleHome } from "@/workbench/showModule";
import { useWorkbenchTabsStore } from "@/stores/workbenchTabs";
import { NewClusterDialog } from "./NewClusterDialog";
import { useEffect, useState } from "react";
import { Plug, PlugZap, Plus, Trash2, Zap } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuTrigger,
} from "@/components/ui/context-menu";
import { SidebarHeader, SidebarSearch } from "@/components/ui/sidebar-shell";
import { TreeRowTag, TreeView, type TreeNodeData } from "@/components/tree-view";
import { EmptyState } from "@/components/ui/empty-state";
import { cn } from "@/lib/utils";
import { useKafkaStore } from "./kafkaStore";

/**
 * Figma `Kafka / Sidebar Content` (1134:2, 1147:859): one tree — the cluster,
 * and under the connected one its topics — over a search field, with the
 * connection status pinned to the bottom. The same rows and metrics as the
 * database rail: two modules that browse a server's contents should not need
 * two mental models.
 *
 * v1 holds one live connection (KafkaDisconnect takes no id), so exactly one
 * cluster ever unfolds. Selecting another one does not connect it — that is a
 * deliberate click in the pane, the way the vault gate treats unlock.
 */
export function KafkaSidebar() {
  const profiles = useKafkaStore((s) => s.profiles);
  const selectedId = useKafkaStore((s) => s.selectedId);
  const connection = useKafkaStore((s) => s.connection);
  const testing = useKafkaStore((s) => s.testing);
  const topics = useKafkaStore((s) => s.topics);
  const selectedTopic = useKafkaStore((s) => s.selectedTopic);
  const selectProfile = useKafkaStore((s) => s.selectProfile);
  const connect = useKafkaStore((s) => s.connect);
  const disconnect = useKafkaStore((s) => s.disconnect);
  const testConnection = useKafkaStore((s) => s.testConnection);
  const deleteProfile = useKafkaStore((s) => s.deleteProfile);
  const selectTopic = useKafkaStore((s) => s.selectTopic);
  const setActiveTab = useKafkaStore((s) => s.setActiveTab);
  const setShowNewCluster = useKafkaStore((s) => s.setShowNewCluster);

  const profilesLoading = useKafkaStore(s => s.profilesLoading);
  const profilesError = useKafkaStore(s => s.profilesError);
  const topicsError = useKafkaStore(s => s.topicsError);
  const topicsLoading = useKafkaStore(s => s.topicsLoading);
  const profileBusy = useKafkaStore(s => s.profileBusy || s.publishing);
  const refreshProfiles = useKafkaStore(s => s.refreshProfiles);
  const visible = useWorkbenchTabsStore(s => s.activeModuleId === "kafka");
  useEffect(() => { void refreshProfiles(); }, [refreshProfiles]);
  useEffect(() => useKafkaStore.subscribe((next, previous) => {
    const scope = previous.publishing && !next.publishing ? "publish"
      : previous.peeking && !next.peeking && next.peeked && next.peekRunId === previous.peekRunId ? "topics" : null;
    if (!scope || (useWorkbenchTabsStore.getState().activeModuleId === "kafka" && next.activeTab === scope)) return;
    const failed = scope === "publish" ? !!next.publishError : !!next.peekError;
    notify({ id: `kafka:${next.connection.profileId}:${scope}`, tone: failed ? "error" : "success",
      title: `${scope === "publish" ? "Kafka publish" : "Kafka peek"} ${failed ? "failed" : "completed"}`,
      action: { label: "Open Kafka", onClick: () => { setActiveTab(scope); showModuleHome("kafka"); } } });
  }), [setActiveTab]);
  const [deleting, setDeleting] = useState<(typeof profiles)[number] | null>(null);
  const [deletePending, setDeletePending] = useState(false);
  const [deleteError, setDeleteError] = useState("");
  async function confirmDelete() {
    if (!deleting || deletePending) return;
    setDeletePending(true); setDeleteError("");
    try { await deleteProfile(deleting.id); setDeleting(null); }
    catch (e) { setDeleteError(errorMessage(e)); }
    finally { setDeletePending(false); }
  }
  const [query, setQuery] = useState("");
  const needle = query.trim().toLowerCase();
  const matches = (s: string) => !needle || s.toLowerCase().includes(needle);

  const connectedProfile = profiles.find((p) => p.id === connection.profileId);

  const nodes: TreeNodeData[] = profiles
    .filter((p) => matches(p.name) || p.bootstrapServers.some(matches) || (connection.profileId === p.id && topics.some((t) => matches(t.name))))
    .map((p) => {
      const isConnected = connection.connected && connection.profileId === p.id;
      const env = (p.env || "").toLowerCase();
      const prod = env === "prod" || env === "production";
      const visibleTopics = isConnected ? topics.filter((t) => matches(t.name)) : [];
      return {
        id: `cluster:${p.id}`,
        label: p.name,
        title: p.bootstrapServers.join(", "),
        // Kafka's mark, and the connection is its colour (it was a dot): the
        // state is still said, by the label and by the colour.
        icon: (
          <span
            className={cn("inline-flex", isConnected ? "text-status-success" : "text-text-tertiary")}
            role="img"
            aria-label={isConnected ? "connected" : "not connected"}
          >
            <BrandIcon brand="kafka" />
          </span>
        ),
        trailing: p.env ? (
          <TreeRowTag danger={prod}>{env}</TreeRowTag>
        ) : null,
        selected: p.id === selectedId,
        onSelect: () => { selectProfile(p.id); showModuleHome("kafka"); },
        // The connected cluster is the open one; there is no fold state to
        // keep because there is nothing else it could show.
        expandable: isConnected,
        expanded: isConnected,
        children: visibleTopics.map((t) => ({
          id: `topic:${t.name}`,
          label: t.name,
          selected: isConnected && t.name === selectedTopic,
          onSelect: () => {
            selectProfile(p.id);
            showModuleHome("kafka");
            void selectTopic(t.name);
            setActiveTab("topics");
          },
        })),
        emptyLabel: topicsLoading ? "Loading topics…" : topicsError ? "Topics unavailable" : needle ? "No topics match" : "No topics in this cluster",
        wrap: (row) => (
          <ContextMenu key={`cluster:${p.id}`}>
            <ContextMenuTrigger asChild>{row}</ContextMenuTrigger>
            <ContextMenuContent>
              {isConnected ? (
                <ContextMenuItem icon={<Plug />} disabled={profileBusy} onSelect={() => void disconnect()}>
                  Disconnect
                </ContextMenuItem>
              ) : (
                <ContextMenuItem icon={<PlugZap />} disabled={profileBusy} onSelect={() => void connect(p.id)}>
                  Connect
                </ContextMenuItem>
              )}
              <ContextMenuItem icon={<Zap />} onSelect={() => { selectProfile(p.id); showModuleHome("kafka"); void testConnection(p.id); }} disabled={testing || profileBusy}>
                Test connection
              </ContextMenuItem>
              <ContextMenuSeparator />
              <ContextMenuItem icon={<Trash2 />} variant="destructive" disabled={profileBusy} onSelect={() => { setDeleteError(""); setDeleting(p); }}>
                Delete cluster
              </ContextMenuItem>
            </ContextMenuContent>
          </ContextMenu>
        ),
      };
    });

  return (
    <div className="flex h-full flex-col select-none">
      <SidebarHeader>
        <SidebarSearch
          value={query}
          onChange={setQuery}
          onClear={() => setQuery("")}
          placeholder="Search topics…"
          aria-label="Search clusters and topics"
        />
        <ActionTooltip label="New cluster"><Button variant="ghost" size="icon-xs" aria-label="New cluster" onClick={() => setShowNewCluster(true)}>
          <Plus className="size-3" />
        </Button></ActionTooltip>
      </SidebarHeader>

      {/* The tree is its own scroller; the empty states are not in it. */}
      {profilesLoading && profiles.length === 0 ? <div className="flex-1 p-3"><PendingStatus>Loading clusters…</PendingStatus></div>
        : profilesError && profiles.length === 0 ? <div className="flex-1 p-2"><FeedbackBanner tone="error" title="Could not load clusters" action={<Button variant="outline" onClick={() => void refreshProfiles()}>Retry</Button>}>{profilesError}</FeedbackBanner></div>
        : profiles.length === 0 ? (
        <div className="min-h-0 flex-1">
          <EmptyState
            size="rail"
            title="No clusters yet"
            action={{ label: "Add cluster", icon: Plus, onClick: () => setShowNewCluster(true) }}
          />
        </div>
      ) : nodes.length === 0 ? (
        <div className="min-h-0 flex-1">
          <EmptyState size="rail" body={`Nothing matches “${needle}”.`} action={{ label: "Clear search", onClick: () => setQuery("") }} />
        </div>
      ) : (
        <TreeView nodes={nodes} className="flex-1 scrollbar-gutter-stable" />
      )}

      {profilesError && profiles.length > 0 && <div className="p-2"><FeedbackBanner tone="error" title="Cluster refresh failed" action={<Button variant="outline" onClick={() => void refreshProfiles()}>Retry</Button>}>{profilesError}</FeedbackBanner></div>}
      <NewClusterDialog visible={visible} />
      <ConfirmDialog open={!!deleting && visible} onClose={() => setDeleting(null)} title={`Delete ${deleting?.name ?? "cluster"}?`} confirmLabel="Delete cluster" pendingLabel="Deleting…" pending={deletePending} error={deleteError} onConfirm={() => void confirmDelete()}>
        This removes the saved profile. An active connection to this cluster will close. Broker topics and messages remain unchanged.
      </ConfirmDialog>
      {/* The one fact the rail must always answer: am I connected, and to what. */}
      <div className="flex h-10 shrink-0 items-center gap-1.5 px-2 text-code-11 text-text-tertiary">
        <span
          className={cn("size-1.5 shrink-0 rounded-full", connection.connected ? "bg-status-success" : "bg-text-tertiary")}
        />
        <span className="truncate">
          {connection.connected
            ? `connected · ${connectedProfile?.bootstrapServers[0] ?? connection.profileId}`
            : "not connected"}
        </span>
      </div>
    </div>
  );
}
