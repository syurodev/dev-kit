import { Server, Workflow } from "lucide-react";
import { rank } from "@/workbench/palette/match";
import type { KafkaProfile, KafkaTopic } from "@/bridge/kafka";
import type { PaletteSearchGroup } from "../registry";

const LIMIT = 8;

export type KafkaPaletteData = {
  /** Only meaningful, and only ever non-empty, while connected. */
  topics: KafkaTopic[];
  connected: boolean;
  profiles: KafkaProfile[];
};

export type KafkaPaletteActions = {
  openTopic: (name: string) => void;
  openCluster: (id: string) => void;
};

/**
 * The pure half: topics and clusters already read out of the store, turned
 * into groups for `query`. Topics only exist for the connected cluster, so
 * they are left out entirely rather than shown as an empty heading when
 * nothing is connected.
 */
export function buildGroups(
  query: string,
  data: KafkaPaletteData,
  actions: KafkaPaletteActions,
): PaletteSearchGroup[] {
  const groups: PaletteSearchGroup[] = [];

  if (data.connected) {
    const topics = rank(query, data.topics, (t) => ({ title: t.name }), LIMIT);
    if (topics.length > 0) {
      groups.push({
        heading: "TOPICS",
        items: topics.map((t) => ({
          id: `topic:${t.name}`,
          title: t.name,
          icon: Workflow,
          run: () => actions.openTopic(t.name),
        })),
      });
    }
  }

  const clusters = rank(
    query,
    data.profiles,
    (p) => ({ title: p.name, others: [...p.bootstrapServers, p.env] }),
    LIMIT,
  );
  if (clusters.length > 0) {
    groups.push({
      heading: "CLUSTERS",
      items: clusters.map((p) => ({
        id: `cluster:${p.id}`,
        title: p.name,
        subtitle: p.bootstrapServers.join(", "),
        icon: Server,
        run: () => actions.openCluster(p.id),
      })),
    });
  }

  return groups;
}

/**
 * Kafka's contribution to the command palette: the connected cluster's
 * topics, and every saved cluster.
 *
 * Selecting a cluster (as the sidebar's own click does — KafkaSidebar.tsx)
 * does not connect it; that is a deliberate act in the pane, same as the
 * vault gate treats unlock. So "open" a cluster here means the same thing.
 */
export async function searchPalette(query: string, signal: AbortSignal): Promise<PaletteSearchGroup[]> {
  try {
    const [{ useKafkaStore }, { showModuleHome }] = await Promise.all([
      import("./kafkaStore"),
      import("@/workbench/showModule"),
    ]);

    if (useKafkaStore.getState().profiles.length === 0) {
      await useKafkaStore.getState().refreshProfiles();
    }
    if (signal.aborted) return [];

    const state = useKafkaStore.getState();
    return buildGroups(
      query,
      { topics: state.topics, connected: state.connection.connected, profiles: state.profiles },
      {
        openTopic: (name) => {
          void useKafkaStore.getState().selectTopic(name);
          useKafkaStore.getState().setActiveTab("topics");
          showModuleHome("kafka");
        },
        openCluster: (id) => {
          useKafkaStore.getState().selectProfile(id);
          showModuleHome("kafka");
        },
      },
    );
  } catch {
    return [];
  }
}
