import { useState } from "react";
import { Plug, PlugZap, Plus, Search, Trash2, Zap } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Chip } from "@/components/ui/chip";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuTrigger,
} from "@/components/ui/context-menu";
import { Input } from "@/components/ui/input";
import { TreeView, type TreeNodeData } from "@/components/tree-view";
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

  const [query, setQuery] = useState("");
  const needle = query.trim().toLowerCase();
  const matches = (s: string) => !needle || s.toLowerCase().includes(needle);

  const connectedProfile = profiles.find((p) => p.id === connection.profileId);

  const nodes: TreeNodeData[] = profiles
    .filter((p) => matches(p.name) || p.bootstrapServers.some(matches) || topics.some((t) => matches(t.name)))
    .map((p) => {
      const isConnected = connection.connected && connection.profileId === p.id;
      const env = (p.env || "").toLowerCase();
      const prod = env === "prod" || env === "production";
      const visibleTopics = isConnected ? topics.filter((t) => matches(t.name)) : [];
      return {
        id: `cluster:${p.id}`,
        label: p.name,
        title: p.bootstrapServers.join(", "),
        icon: (
          <span
            className={cn("size-1.5 shrink-0 rounded-full", isConnected ? "bg-status-success" : "bg-text-tertiary")}
            aria-label={isConnected ? "connected" : "not connected"}
          />
        ),
        trailing: p.env ? (
          <Chip tone={prod ? "danger" : "default"} className={cn("h-5 px-1.5", !prod && "text-text-tertiary")}>
            {env}
          </Chip>
        ) : null,
        selected: p.id === selectedId,
        onSelect: () => selectProfile(p.id),
        // The connected cluster is the open one; there is no fold state to
        // keep because there is nothing else it could show.
        expandable: isConnected,
        expanded: isConnected,
        children: visibleTopics.map((t) => ({
          id: `topic:${t.name}`,
          label: t.name,
          selected: isConnected && t.name === selectedTopic,
          onSelect: () => {
            void selectTopic(t.name);
            setActiveTab("topics");
          },
        })),
        emptyLabel: needle ? "No topics match" : "No topics in this cluster",
        wrap: (row) => (
          <ContextMenu key={`cluster:${p.id}`}>
            <ContextMenuTrigger asChild>{row}</ContextMenuTrigger>
            <ContextMenuContent>
              {isConnected ? (
                <ContextMenuItem onSelect={() => void disconnect()}>
                  <Plug />
                  Disconnect
                </ContextMenuItem>
              ) : (
                <ContextMenuItem onSelect={() => void connect(p.id)}>
                  <PlugZap />
                  Connect
                </ContextMenuItem>
              )}
              <ContextMenuItem onSelect={() => void testConnection(p.id)} disabled={testing}>
                <Zap />
                Test connection
              </ContextMenuItem>
              <ContextMenuSeparator />
              <ContextMenuItem variant="destructive" onSelect={() => void deleteProfile(p.id)}>
                <Trash2 />
                Delete cluster
              </ContextMenuItem>
            </ContextMenuContent>
          </ContextMenu>
        ),
      };
    });

  return (
    <div className="flex h-full flex-col select-none">
      <div className="flex items-center gap-0.5 p-1.5 pb-1">
        <div className="relative min-w-0 flex-1">
          <Search className="pointer-events-none absolute top-1/2 left-2 size-3 -translate-y-1/2 text-text-tertiary" />
          <Input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search topics…"
            aria-label="Search clusters and topics"
            className="h-7 border-border-subtle bg-surface-well pl-7 text-body-12"
          />
        </div>
        <Button variant="ghost" size="icon-xs" title="New cluster" onClick={() => setShowNewCluster(true)}>
          <Plus className="size-3" />
        </Button>
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto px-1.5 pb-2">
        {profiles.length === 0 ? (
          <div className="px-4 py-6 text-center text-body-12 text-text-tertiary">
            <p>No clusters saved.</p>
            <Button variant="outline" size="sm" className="mt-2" onClick={() => setShowNewCluster(true)}>
              <Plus className="size-3" /> Add cluster
            </Button>
          </div>
        ) : nodes.length === 0 ? (
          <p className="px-4 py-6 text-center text-body-12 text-text-tertiary">Nothing matches “{needle}”.</p>
        ) : (
          <TreeView nodes={nodes} />
        )}
      </div>

      {/* The one fact the rail must always answer: am I connected, and to what. */}
      <div className="flex h-10 shrink-0 items-center gap-1.5 px-3 text-code-11 text-text-tertiary">
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
