import type { createKafkaStore } from "./kafkaStore";

type KafkaStore = ReturnType<typeof createKafkaStore>;
type KafkaSnapshot = ReturnType<KafkaStore["getState"]>;

/** Something is loading that the answer depends on: the connection, or the topic list. */
function busy(state: KafkaSnapshot): boolean {
  return state.profilesLoading || state.topicsLoading || state.profileBusy;
}

/** Resolves once nothing the answer depends on is still loading. */
function idle(store: KafkaStore): Promise<void> {
  if (!busy(store.getState())) return Promise.resolve();
  return new Promise((resolve) => {
    const stop = store.subscribe((state) => {
      if (busy(state)) return;
      stop();
      resolve();
    });
  });
}

function hasTopic(state: KafkaSnapshot, topic: string): boolean {
  return state.topics.some((t) => t.name === topic);
}

/**
 * `kafka.reveal` for a topic: the explorer, on that topic.
 *
 * An agent's Kafka tools connect Go's one shared connection, which the store
 * only learns about when something refreshes it — so this asks Go first
 * instead of trusting `connection.connected`. A refresh already in flight (the
 * sidebar mounting) is waited for, not raced. A topic missing from the list is
 * listed once more: the agent may have just created it. The selection survives
 * the sidebar's own refresh on mount, which keeps a selected topic that is on
 * the cluster (loadClusterData).
 *
 * Kafka is shown either way; "unavailable" when there is no live connection or
 * the topic is not on it, so the caller can say why nothing is selected.
 */
export async function revealTopic(store: KafkaStore, topic: string, show: () => void): Promise<"ok" | "unavailable"> {
  await store.getState().refreshProfiles();
  await idle(store);
  if (store.getState().connection.connected && !hasTopic(store.getState(), topic)) {
    await store.getState().loadClusterData();
    await idle(store);
  }
  const state = store.getState();
  if (!state.connection.connected || !hasTopic(state, topic)) {
    show();
    return "unavailable";
  }
  await state.selectTopic(topic);
  store.getState().setActiveTab("topics");
  show();
  return "ok";
}
