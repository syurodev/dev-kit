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

/** The store as revealTopic sees it, counting the subscriptions it leaves open. */
function counted(store: ReturnType<typeof createKafkaStore>) {
  let open = 0;
  return {
    view: {
      getState: store.getState,
      subscribe: (listener: Parameters<typeof store.subscribe>[0]) => {
        open++;
        const stop = store.subscribe(listener);
        let stopped = false;
        return () => {
          if (!stopped) open--;
          stopped = true;
          stop();
        };
      },
    },
    open: () => open,
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

  it("shows Kafka before asking Go anything", async () => {
    const { store, holdStatus } = goSide();
    let release!: () => void;
    holdStatus(() => new Promise((resolve) => (release = () => resolve({ connected: true, profileId: "a" }))));
    const { show, calls } = shown();
    const reveal = revealTopic(store, "payments", show);
    expect(calls).toHaveLength(1);
    release();
    expect(await reveal).toBe("ok");
    expect(calls).toHaveLength(1);
  });

  it("gives up as unavailable when Go never answers, and leaves no subscription behind", async () => {
    const { store, holdStatus } = goSide();
    let release!: () => void;
    holdStatus(() => new Promise((resolve) => (release = () => resolve({ connected: true, profileId: "a" }))));
    const { view, open } = counted(store);
    expect(await revealTopic(view, "payments", () => {}, 20)).toBe("unavailable");
    expect(open()).toBe(0);
    expect(store.getState().selectedTopic).not.toBe("payments");
    // Go answering late neither selects the topic nor subscribes again.
    release();
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(open()).toBe(0);
    expect(store.getState().selectedTopic).not.toBe("payments");
  });

  it("gives up when a refresh someone else started never settles", async () => {
    const { store, holdStatus } = goSide();
    holdStatus(() => new Promise(() => {}));
    void store.getState().refreshProfiles();
    const { view, open } = counted(store);
    expect(await revealTopic(view, "payments", () => {}, 20)).toBe("unavailable");
    expect(open()).toBe(0);
  });

  it("selects nothing when the connection moved to another cluster on the way", async () => {
    const { store, state } = goSide({ topics: ["orders"] });
    await store.getState().refreshProfiles();
    // Looking for "fresh" lists again — and that listing lands after a reconnect to cluster b.
    const list = store.getState().loadClusterData;
    let listed = 0;
    store.setState({
      loadClusterData: async () => {
        listed++;
        state.topics = ["orders", "fresh"];
        store.setState({ connection: { connected: true, profileId: "b" } });
        await list();
      },
    });
    expect(await revealTopic(store, "fresh", () => {})).toBe("unavailable");
    expect(listed).toBe(1);
    expect(store.getState().topics.map((t) => t.name)).toContain("fresh");
    expect(store.getState().selectedTopic).not.toBe("fresh");
  });
});
