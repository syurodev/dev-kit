import { create } from "zustand";
import {
  connectKafka,
  createKafkaProfile,
  deleteKafkaProfile,
  describeKafkaTopic,
  disconnectKafka,
  getKafkaConnectionStatus,
  listKafkaConsumerGroups,
  listKafkaProfiles,
  listKafkaTopics,
  peekKafkaMessages,
  publishKafkaMessage,
  testKafkaConnection,
  type CreateKafkaProfileInput,
  type KafkaConnectionStatus,
  type KafkaConsumerGroup,
  type KafkaMessage,
  type KafkaProfile,
  type KafkaTopic,
  type KafkaTopicDetail,
  type PublishOutcome,
} from "@/bridge/kafka";
import { listEnvTags, type EnvTag } from "@/bridge/environments";
import { errorMessage } from "@/bridge/errors";
import { parsePartition } from "./kafkaForm";

export type KafkaTab = "topics" | "publish" | "groups";
/** What the content pane shows when nothing is connected. */
export type KafkaPhase = "idle" | "connecting" | "error";

type PublishForm = { topic: string; key: string; payload: string; partition: string };

type KafkaState = {
  profiles: KafkaProfile[];
  envTags: EnvTag[];
  selectedId: string;
  connection: KafkaConnectionStatus;
  phase: KafkaPhase;
  /** The broker's or bridge's own words, verbatim — never paraphrased. */
  error: string;
  testing: boolean;
  testResult: { ok: boolean; message: string } | null;

  topics: KafkaTopic[];
  selectedTopic: string;
  topicDetail: KafkaTopicDetail | null;

  peekPartition: number;
  peekLimit: number;
  /** -1 means "latest": the bridge reads the last `limit` records. */
  peekOffset: number;
  messages: KafkaMessage[];
  peeking: boolean;
  /** A peek has run since the topic was picked — an empty table before that
   *  is "not asked yet", after it is a real answer. */
  peeked: boolean;
  peekDurationMs: number | null;
  peekError: string;

  groups: KafkaConsumerGroup[];
  groupsLoading: boolean;

  publish: PublishForm;
  publishing: boolean;
  publishResult: PublishOutcome | null;
  publishError: string;

  activeTab: KafkaTab;
  inspected: KafkaMessage | null;
  showNewCluster: boolean;

  refreshProfiles: () => Promise<void>;
  selectProfile: (id: string) => void;
  connect: (id: string) => Promise<void>;
  disconnect: () => Promise<void>;
  testConnection: (id: string) => Promise<void>;
  deleteProfile: (id: string) => Promise<void>;
  createProfile: (input: CreateKafkaProfileInput) => Promise<void>;
  loadClusterData: () => Promise<void>;
  selectTopic: (name: string) => Promise<void>;
  setPeek: (patch: Partial<Pick<KafkaState, "peekPartition" | "peekLimit" | "peekOffset">>) => void;
  peek: () => Promise<void>;
  refreshGroups: () => Promise<void>;
  setPublish: (patch: Partial<PublishForm>) => void;
  publishMessage: () => Promise<void>;
  setActiveTab: (tab: KafkaTab) => void;
  inspect: (message: KafkaMessage | null) => void;
  setShowNewCluster: (open: boolean) => void;
};

const EMPTY_CLUSTER = {
  topics: [] as KafkaTopic[],
  selectedTopic: "",
  topicDetail: null as KafkaTopicDetail | null,
  messages: [] as KafkaMessage[],
  peeked: false,
  peekDurationMs: null as number | null,
  peekError: "",
  groups: [] as KafkaConsumerGroup[],
  publishResult: null as PublishOutcome | null,
  publishError: "",
  inspected: null as KafkaMessage | null,
};

/**
 * Kafka's view state lives outside the component for the same reason
 * CodeGraph's does: the rail is registered through useSidebarContent and the
 * content pane is a separate subtree, and both have to agree on which cluster
 * is open. Deliberately not persisted — the Go side holds the one live
 * connection, and a remembered "connected" that Go no longer has would lie.
 */
export const useKafkaStore = create<KafkaState>((set, get) => ({
  profiles: [],
  envTags: [],
  selectedId: "",
  connection: { connected: false },
  phase: "idle",
  error: "",
  testing: false,
  testResult: null,
  ...EMPTY_CLUSTER,
  peekPartition: 0,
  peekLimit: 50,
  peekOffset: -1,
  peeking: false,
  groupsLoading: false,
  publish: { topic: "", key: "", payload: "", partition: "" },
  publishing: false,
  activeTab: "topics",
  showNewCluster: false,

  refreshProfiles: async () => {
    const [profiles, envTags, connection] = await Promise.all([
      listKafkaProfiles(),
      listEnvTags(),
      getKafkaConnectionStatus(),
    ]);
    const { selectedId } = get();
    // The connected profile wins the selection: the rail must point at the
    // cluster whose topics the pane is showing.
    const next =
      (connection.connected && connection.profileId) ||
      (profiles.some((p) => p.id === selectedId) ? selectedId : (profiles[0]?.id ?? ""));
    set({ profiles, envTags, connection, selectedId: next });
    if (connection.connected && get().topics.length === 0) void get().loadClusterData();
  },

  selectProfile: (id) => set({ selectedId: id, error: "", testResult: null, phase: "idle" }),

  connect: async (id) => {
    set({ selectedId: id, phase: "connecting", error: "", testResult: null });
    try {
      await connectKafka(id);
      const connection = await getKafkaConnectionStatus();
      set({ connection, phase: "idle" });
      await get().loadClusterData();
    } catch (e) {
      set({ phase: "error", error: errorMessage(e), connection: { connected: false } });
    }
  },

  disconnect: async () => {
    try {
      await disconnectKafka();
    } catch (e) {
      set({ error: errorMessage(e) });
    }
    set({ connection: { connected: false }, phase: "idle", ...EMPTY_CLUSTER });
  },

  testConnection: async (id) => {
    set({ testing: true, testResult: null });
    try {
      await testKafkaConnection(id);
      set({ testResult: { ok: true, message: "Connection OK" } });
    } catch (e) {
      set({ testResult: { ok: false, message: errorMessage(e) } });
    } finally {
      set({ testing: false });
    }
  },

  deleteProfile: async (id) => {
    await deleteKafkaProfile(id);
    if (get().selectedId === id) set({ selectedId: "" });
    await get().refreshProfiles();
  },

  createProfile: async (input) => {
    const created = await createKafkaProfile(input);
    set({ showNewCluster: false, selectedId: created.id });
    await get().refreshProfiles();
  },

  loadClusterData: async () => {
    if (!get().connection.connected) return;
    set({ groupsLoading: true });
    try {
      const [topics, groups] = await Promise.all([listKafkaTopics(), listKafkaConsumerGroups()]);
      set({ topics, groups, groupsLoading: false });
      if (!get().selectedTopic && topics.length > 0) await get().selectTopic(topics[0].name);
    } catch (e) {
      set({ error: errorMessage(e), groupsLoading: false });
    }
  },

  selectTopic: async (name) => {
    set({
      selectedTopic: name,
      messages: [],
      peeked: false,
      peekError: "",
      peekDurationMs: null,
      inspected: null,
      topicDetail: null,
      publish: { ...get().publish, topic: name },
    });
    try {
      const detail = await describeKafkaTopic(name);
      // The topic changed under us while describe was in flight.
      if (get().selectedTopic !== name) return;
      set({ topicDetail: detail, peekPartition: detail.partitions[0] ?? 0 });
    } catch (e) {
      set({ peekError: errorMessage(e) });
    }
  },

  setPeek: (patch) => set(patch),

  peek: async () => {
    const { selectedTopic, peekPartition, peekLimit, peekOffset } = get();
    if (!selectedTopic) return;
    set({ peeking: true, peekError: "", inspected: null });
    const startedAt = Date.now();
    try {
      const messages = await peekKafkaMessages(selectedTopic, peekPartition, peekLimit, peekOffset);
      set({ messages, peeked: true, peekDurationMs: Date.now() - startedAt });
    } catch (e) {
      set({ messages: [], peeked: true, peekError: errorMessage(e) });
    } finally {
      set({ peeking: false });
    }
  },

  refreshGroups: async () => {
    set({ groupsLoading: true });
    try {
      set({ groups: await listKafkaConsumerGroups() });
    } catch (e) {
      set({ error: errorMessage(e) });
    } finally {
      set({ groupsLoading: false });
    }
  },

  setPublish: (patch) => set({ publish: { ...get().publish, ...patch } }),

  publishMessage: async () => {
    const { publish } = get();
    if (!publish.topic || !publish.payload) return;
    set({ publishing: true, publishError: "", publishResult: null });
    try {
      const result = await publishKafkaMessage({
        topic: publish.topic,
        key: publish.key || undefined,
        value: publish.payload,
        partition: parsePartition(publish.partition),
      });
      set({ publishResult: result });
    } catch (e) {
      set({ publishError: errorMessage(e) });
    } finally {
      set({ publishing: false });
    }
  },

  setActiveTab: (tab) => set({ activeTab: tab }),
  inspect: (message) => set({ inspected: message }),
  setShowNewCluster: (open) => set({ showNewCluster: open }),
}));
