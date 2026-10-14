import { describe, expect, it } from "bun:test";
import type { CancellableCall } from "@/bridge/client";
import type { KafkaConnectionStatus, KafkaMessage } from "@/bridge/kafka";
import { createKafkaStore } from "./kafkaStore";
import { revealTopic } from "./reveal";

/**
 * Go's side as an agent leaves it: gateKafka connected the shared connection,
 * and the frontend store — fresh, never refreshed — still says disconnected.
 */
function goSide({ connected = true, topics = ["orders", "payments"] }: { connected?: boolean; topics?: string[] } = {}) {
  const state = { connected, topics, statusCalls: 0 };
  let status: () => Promise<KafkaConnectionStatus> = async () =>
    state.connected ? { connected: true, profileId: "a" } : { connected: false };
  const api: NonNullable<Parameters<typeof createKafkaStore>[0]> = {
    connectKafka: async () => {},
    disconnectKafka: async () => {},
    getKafkaConnectionStatus: () => {
      state.statusCalls++;
      return status();
    },
    listKafkaProfiles: async () => [{ id: "a", name: "local", bootstrapServers: ["localhost:9092"] } as never],
    listEnvTags: async () => [],
    createKafkaProfile: async () => {
      throw new Error("unused");
    },
    deleteKafkaProfile: async () => {},
    testKafkaConnection: async () => {},
    listKafkaTopics: async () => state.topics.map((name) => ({ name })),
    describeKafkaTopic: async (name) => ({ name, replicationFactor: 1, partitions: [0] }),
    listKafkaConsumerGroups: async () => [],
    publishKafkaMessage: async () => ({ partition: 0, offset: 1 }),
    peekKafkaMessages: () => Object.assign(Promise.resolve([]), { cancel() {} }) as CancellableCall<KafkaMessage[]>,
  };
  const store = createKafkaStore(api);
  return {
    state,
    store,
    holdStatus(next: () => Promise<KafkaConnectionStatus>) {
      status = next;
    },
  };
}

function shown() {
  const calls: number[] = [];
  return { show: () => calls.push(1), calls };
}

describe("revealTopic (kafka.reveal)", () => {
  it("selects the topic when Go is connected but the store has not heard yet", async () => {
    const { store } = goSide();
    store.getState().setActiveTab("publish");
    const { show, calls } = shown();
    expect(store.getState().connection.connected).toBe(false);

    expect(await revealTopic(store, "payments", show)).toBe("ok");

    expect(store.getState().selectedTopic).toBe("payments");
    expect(store.getState().activeTab).toBe("topics");
    expect(calls).toHaveLength(1);
  });

  it("keeps the selection when the sidebar mounts and refreshes afterwards", async () => {
    const { store } = goSide();
    await revealTopic(store, "payments", () => {});
    await store.getState().refreshProfiles();
    expect(store.getState().selectedTopic).toBe("payments");
  });

  it("waits for a refresh already in flight rather than reading its half-done state", async () => {
    const { store, holdStatus } = goSide();
    let release!: () => void;
    holdStatus(() => new Promise((resolve) => (release = () => resolve({ connected: true, profileId: "a" }))));
    const sidebar = store.getState().refreshProfiles();
    const reveal = revealTopic(store, "payments", () => {});
    await Promise.resolve();
    release();
    await sidebar;
    expect(await reveal).toBe("ok");
    expect(store.getState().selectedTopic).toBe("payments");
  });

  it("finds a topic created since the store last listed them", async () => {
    const { store, state } = goSide({ topics: ["orders"] });
    await store.getState().refreshProfiles();
    state.topics = ["orders", "fresh"];
    expect(await revealTopic(store, "fresh", () => {})).toBe("ok");
    expect(store.getState().selectedTopic).toBe("fresh");
  });

  it("is unavailable with nothing connected, and selects nothing — but still shows Kafka", async () => {
    const { store } = goSide({ connected: false });
    const { show, calls } = shown();
    expect(await revealTopic(store, "payments", show)).toBe("unavailable");
    expect(store.getState().selectedTopic).toBe("");
    expect(calls).toHaveLength(1);
  });

  it("is unavailable for a topic the live cluster does not have", async () => {
    const { store } = goSide();
    const { show, calls } = shown();
    expect(await revealTopic(store, "missing", show)).toBe("unavailable");
    expect(store.getState().selectedTopic).not.toBe("missing");
    expect(calls).toHaveLength(1);
  });
});
