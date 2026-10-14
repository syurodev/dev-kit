import type { createKafkaStore } from "./kafkaStore";

type KafkaStore = Pick<ReturnType<typeof createKafkaStore>, "getState" | "subscribe">;
type KafkaSnapshot = ReturnType<KafkaStore["getState"]>;

/** How long a reveal waits on Go before it answers "unavailable". */
export const REVEAL_TIMEOUT_MS = 12_000;

/** Something is loading that the answer depends on: the connection, or the topic list. */
function busy(state: KafkaSnapshot): boolean {
  return state.profilesLoading || state.topicsLoading || state.profileBusy;
}

/**
 * True once `work` has finished and nothing the answer depends on is still
 * loading; false if that has not happened by `deadline`. Every path leaves no
 * subscription behind, including `work` settling after the deadline.
 */
async function settle(store: KafkaStore, work: Promise<unknown>, deadline: number): Promise<boolean> {
  let done = false;
  let stop = () => {};
  let timer: ReturnType<typeof setTimeout> | undefined;
  const quiet = work
    .catch(() => undefined)
    .then(
      () =>
        new Promise<true>((resolve) => {
          if (done || !busy(store.getState())) return resolve(true);
          stop = store.subscribe((state) => {
            if (!busy(state)) resolve(true);
          });
        }),
    );
  const late = new Promise<false>((resolve) => {
    timer = setTimeout(() => resolve(false), Math.max(0, deadline - Date.now()));
  });
  try {
    return await Promise.race([quiet, late]);
  } finally {
    done = true;
    clearTimeout(timer);
    stop();
  }
}

function hasTopic(state: KafkaSnapshot, topic: string): boolean {
  return state.topics.some((t) => t.name === topic);
}

/**
 * `kafka.reveal` for a topic: the explorer, on that topic.
 *
 * Kafka comes on screen first, so Open answers at once; the topic follows.
 * An agent's Kafka tools connect Go's one shared connection, which the store
 * only learns about when something refreshes it — so this asks Go instead of
 * trusting `connection.connected`. A refresh already in flight (the sidebar
 * mounting) is waited for, not raced. A topic missing from the list is listed
 * once more: the agent may have just created it. The selection survives the
 * sidebar's own refresh, which keeps a selected topic that is on the cluster
 * (loadClusterData).
 *
 * "unavailable" — nothing selected — when there is no live connection, the
 * topic is not on it, the connection moved to another cluster meanwhile, or
 * Go has not answered within `timeoutMs`.
 */
export async function revealTopic(
  store: KafkaStore,
  topic: string,
  show: () => void,
  timeoutMs = REVEAL_TIMEOUT_MS,
): Promise<"ok" | "unavailable"> {
  show();
  const deadline = Date.now() + timeoutMs;
  if (!(await settle(store, store.getState().refreshProfiles(), deadline))) return "unavailable";
  const cluster = store.getState().connection.profileId;
  if (store.getState().connection.connected && !hasTopic(store.getState(), topic)) {
    if (!(await settle(store, store.getState().loadClusterData(), deadline))) return "unavailable";
  }
  const state = store.getState();
  if (!state.connection.connected || state.connection.profileId !== cluster || !hasTopic(state, topic)) {
    return "unavailable";
  }
  // selectTopic sets the selection at once; describing the topic is the view's to wait on.
  void state.selectTopic(topic);
  store.getState().setActiveTab("topics");
  return "ok";
}
