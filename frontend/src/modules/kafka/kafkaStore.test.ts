import { describe, expect, it } from "bun:test";
import { createKafkaStore } from "./kafkaStore";
import type { CancellableCall } from "@/bridge/client";
import type { KafkaMessage, KafkaTopicDetail, PublishOutcome } from "@/bridge/kafka";

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason: unknown) => void;
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}
function fixture(overrides: Partial<Parameters<typeof createKafkaStore>[0]> = {}) {
  let id = "a";
  const api: NonNullable<Parameters<typeof createKafkaStore>[0]> = {
    connectKafka: async next => { id = next; },
    disconnectKafka: async () => { id = ""; },
    getKafkaConnectionStatus: async () => ({ connected: !!id, profileId: id || undefined }),
    listKafkaProfiles: async () => [], listEnvTags: async () => [],
    createKafkaProfile: async () => { throw new Error("unused"); },
    deleteKafkaProfile: async () => {}, testKafkaConnection: async () => {},
    listKafkaTopics: async () => [{ name: "orders" }],
    describeKafkaTopic: async name => ({ name, replicationFactor: 1, partitions: [0] }),
    listKafkaConsumerGroups: async () => [],
    publishKafkaMessage: async () => ({ partition: 0, offset: 42 }),
    peekKafkaMessages: () => Object.assign(Promise.resolve([]), { cancel() {} }) as CancellableCall<KafkaMessage[]>,
    ...overrides,
  };
  const store = createKafkaStore(api);
  store.setState({ connection: { connected: true, profileId: "a" }, selectedId: "a", selectedTopic: "orders", topicDetail: { name: "orders", replicationFactor: 1, partitions: [0] } });
  return store;
}

describe("Kafka async lifecycle", () => {
  it("does not publish twice while the first request is pending", async () => {
    const pending = deferred<PublishOutcome>(); let calls = 0;
    const store = fixture({ publishKafkaMessage: () => { calls++; return pending.promise; } });
    store.getState().setPublish({ topic: "orders", payload: "hello" });
    const first = store.getState().publishMessage();
    await store.getState().publishMessage();
    store.getState().setPublish({ payload: "edited" });
    await store.getState().connect("b");
    expect(calls).toBe(1);
    expect(store.getState().connection.profileId).toBe("a");
    expect(store.getState().publish.payload).toBe("hello");
    pending.resolve({ offset: 3, partition: 0 }); await first;
    expect(store.getState().publishResult?.offset).toBe(3);
  });
  it("ignores a canceled peek even if transport resolves afterward", async () => {
    const pending = deferred<KafkaMessage[]>(); let canceled = false;
    const store = fixture({ peekKafkaMessages: () => Object.assign(pending.promise, { cancel() { canceled = true; } }) as CancellableCall<KafkaMessage[]> });
    const peek = store.getState().peek();
    await store.getState().selectTopic("payments");
    pending.resolve([{ offset: 1, value: "old" }]); await peek;
    expect(canceled).toBe(true); expect(store.getState().messages).toEqual([]);
    expect(store.getState().selectedTopic).toBe("payments");
    expect(store.getState().peeking).toBe(false);
  });
  it("ignores failures from the previous topic's describe", async () => {
    const pending = deferred<KafkaTopicDetail>();
    const store = fixture({ describeKafkaTopic: name => name === "old" ? pending.promise : Promise.resolve({ name, replicationFactor: 1, partitions: [2] }) });
    const old = store.getState().selectTopic("old");
    await store.getState().selectTopic("new");
    pending.reject(new Error("old topic missing")); await old;
    expect(store.getState().peekError).toBe(""); expect(store.getState().peekPartition).toBe(2);
  });
  it("keeps the live connection and previous result after a replacement fails", async () => {
    const store = fixture({ connectKafka: async () => { throw new Error("unreachable"); } });
    store.setState({ messages: [{ offset: 4, value: "kept" }] });
    await store.getState().connect("b");
    expect(store.getState().connection.profileId).toBe("a");
    expect(store.getState().selectedId).toBe("b");
    expect(store.getState().messages[0].value).toBe("kept");
    expect(store.getState().error).toBe("unreachable");
  });
  it("keeps drafts per cluster and does not retarget a payload on topic selection", async () => {
    const store = fixture();
    store.getState().setPublish({ topic: "orders", payload: "draft a", partition: "2" });
    await store.getState().selectTopic("payments");
    expect(store.getState().publish.topic).toBe("orders");
    await store.getState().connect("b");
    expect(store.getState().publish.payload).toBe("");
    store.getState().setPublish({ payload: "draft b" });
    await store.getState().connect("a");
    expect(store.getState().publish).toEqual({ topic: "orders", payload: "draft a", partition: "2", key: "" });
  });
  it("does not let old cluster metadata land after reconnect", async () => {
    const pending = deferred<{ name: string }[]>(); let calls = 0;
    const store = fixture({ listKafkaTopics: () => ++calls === 1 ? pending.promise : Promise.resolve([{ name: "new" }]) });
    const old = store.getState().loadClusterData();
    await store.getState().connect("b");
    pending.resolve([{ name: "old" }]); await old;
    expect(store.getState().topics).toEqual([{ name: "new" }]);
  });
  it("loads topics independently of failed consumer groups", async () => {
    const store = fixture({ listKafkaConsumerGroups: async () => { throw new Error("groups denied"); } });
    await store.getState().loadClusterData();
    expect(store.getState().topics[0].name).toBe("orders");
    expect(store.getState().groupsError).toBe("groups denied"); expect(store.getState().topicsError).toBe("");
  });
  it("retains previous rows on peek failure and blocks invalid publish partition", async () => {
    let publishes = 0;
    const store = fixture({ peekKafkaMessages: () => Object.assign(Promise.reject(new Error("broker unavailable")), { cancel() {} }) as CancellableCall<KafkaMessage[]>, publishKafkaMessage: async () => { publishes++; return { partition: 0, offset: 0 }; } });
    store.setState({ messages: [{ offset: 8, value: "previous" }], peeked: true });
    await store.getState().peek();
    expect(store.getState().messages[0].value).toBe("previous");
    expect(store.getState().peekError).toBe("broker unavailable");
    store.getState().setPublish({ topic: "orders", payload: "body", partition: "oops" });
    await store.getState().publishMessage(); expect(publishes).toBe(0);
    expect(store.getState().publishError).toContain("partition");
  });
  it("reconciles disconnects caused by a failed profile deletion", async () => {
    const store = fixture({ deleteKafkaProfile: async () => { throw new Error("vault write failed"); }, getKafkaConnectionStatus: async () => ({ connected: false }) });
    await expect(store.getState().deleteProfile("a")).rejects.toThrow("vault write failed");
    expect(store.getState().connection.connected).toBe(false); expect(store.getState().profileBusy).toBe(false);
  });
});
