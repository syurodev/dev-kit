import { describe, expect, it } from "bun:test";
import { buildGroups } from "./paletteSearch";
import type { KafkaProfile, KafkaTopic } from "@/bridge/kafka";

function profile(overrides: Partial<KafkaProfile> = {}): KafkaProfile {
  return {
    id: "c1",
    name: "Staging",
    bootstrapServers: ["broker1:9092", "broker2:9092"],
    securityProtocol: "PLAINTEXT",
    saslMechanism: "",
    username: "",
    env: "staging",
    ...overrides,
  };
}

const TOPICS: KafkaTopic[] = [{ name: "orders.created" }, { name: "orders.cancelled" }];

function actions() {
  const openedTopics: string[] = [];
  const openedClusters: string[] = [];
  return {
    openedTopics,
    openedClusters,
    actions: {
      openTopic: (name: string) => openedTopics.push(name),
      openCluster: (id: string) => openedClusters.push(id),
    },
  };
}

describe("kafka buildGroups", () => {
  it("omits topics entirely when nothing is connected, even if some are cached", () => {
    const { actions: acts } = actions();
    const groups = buildGroups("", { topics: TOPICS, connected: false, profiles: [profile()] }, acts);
    expect(groups.map((g) => g.heading)).toEqual(["CLUSTERS"]);
  });

  it("shows topics once connected", () => {
    const { actions: acts } = actions();
    const groups = buildGroups("", { topics: TOPICS, connected: true, profiles: [] }, acts);
    expect(groups.map((g) => g.heading)).toEqual(["TOPICS"]);
    expect(groups[0].items).toHaveLength(2);
  });

  it("matches a cluster by its bootstrap servers or environment", () => {
    const { actions: acts } = actions();
    const p = profile();
    expect(buildGroups("broker1", { topics: [], connected: false, profiles: [p] }, acts)).toHaveLength(1);
    expect(buildGroups("staging", { topics: [], connected: false, profiles: [p] }, acts)).toHaveLength(1);
    expect(buildGroups("no-such-cluster", { topics: [], connected: false, profiles: [p] }, acts)).toEqual([]);
  });

  it("runs the caller's action for a topic or a cluster", () => {
    const { actions: acts, openedTopics, openedClusters } = actions();
    const groups = buildGroups("", { topics: TOPICS, connected: true, profiles: [profile()] }, acts);
    groups.find((g) => g.heading === "TOPICS")!.items[0].run();
    expect(openedTopics).toEqual(["orders.created"]);

    const clusterGroups = buildGroups("", { topics: [], connected: false, profiles: [profile()] }, acts);
    clusterGroups[0].items[0].run();
    expect(openedClusters).toEqual(["c1"]);
  });
});
