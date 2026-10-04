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
import { errorMessage, isCancelError } from "@/bridge/errors";
import type { CancellableCall } from "@/bridge/client";

import { parsePartition, validatePeek, validatePublish } from "./kafkaForm";

const kafkaAPI = { connectKafka, createKafkaProfile, deleteKafkaProfile, describeKafkaTopic,
  disconnectKafka, getKafkaConnectionStatus, listKafkaConsumerGroups, listKafkaProfiles,
  listKafkaTopics, peekKafkaMessages, publishKafkaMessage, testKafkaConnection, listEnvTags };

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
  profileBusy: boolean;
  profilesLoading: boolean;
  profilesError: string;
  topicsLoading: boolean;
  topicsError: string;
  groupsError: string;
  testing: boolean;
  testResult: { ok: boolean; message: string } | null;

  topics: KafkaTopic[];
  selectedTopic: string;
  topicDetail: KafkaTopicDetail | null;

  peekPartition: number;
  peekLimit: number | string;
  /** -1 means "latest": the bridge reads the last `limit` records. */
  peekOffset: number | string;
  messages: KafkaMessage[];
  peeking: boolean;
  /** A peek has run since the topic was picked — an empty table before that
   *  is "not asked yet", after it is a real answer. */
  peeked: boolean;
  peekDurationMs: number | null;
  resultPartition: number | null;
  peekError: string;
  /** Guards against a slow peek landing after a newer one (smoothness 5). */
  peekRunId: number;

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
  cancelPeek: () => void;
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
  resultPartition: null as number | null,
  peekError: "",
  groups: [] as KafkaConsumerGroup[],
  publishResult: null as PublishOutcome | null,
  publishError: "",
  inspected: null as KafkaMessage | null,
};

/**
 * Kafka's view state lives outside the component for the same reason
 * CodeGraph's does: the manifest rail and content pane are separate subtrees
 * that have to agree on the active cluster. Deliberately not persisted — the Go side holds the one live
 * connection, and a remembered "connected" that Go no longer has would lie.
 */
export function createKafkaStore(api = kafkaAPI) {
  let inFlightPeek: CancellableCall<KafkaMessage[]> | null = null;
  let revision = 0;
  let describeRunId = 0;
  const drafts = new Map<string, PublishForm>();
  const emptyDraft = (): PublishForm => ({ topic: "", key: "", payload: "", partition: "" });
  return create<KafkaState>((set, get) => {
    const adoptConnection = (connection: KafkaConnectionStatus) => {
      const old = get().connection;
      if (old.connected === connection.connected && old.profileId === connection.profileId) {
        set({ connection }); return;
      }
      if (old.profileId) drafts.set(old.profileId, { ...get().publish });
      revision++;
      get().cancelPeek();
      set({ connection, ...EMPTY_CLUSTER, topicsError: "", groupsError: "", topicsLoading: false,
        groupsLoading: false, publish: drafts.get(connection.profileId ?? "") ?? emptyDraft() });
    };
    return {
      profiles: [],
      envTags: [],
      selectedId: "",
      connection: { connected: false },
      phase: "idle",
      error: "",
      profileBusy: false,
      profilesLoading: false,
      profilesError: "",
      topicsLoading: false,
      topicsError: "",
      groupsError: "",
      testing: false,
      testResult: null,
      ...EMPTY_CLUSTER,
      peekPartition: 0,
      peekLimit: 50,
      peekOffset: -1,
      peeking: false,
      peekRunId: 0,
      groupsLoading: false,
      publish: { topic: "", key: "", payload: "", partition: "" },
      publishing: false,
      activeTab: "topics",
      showNewCluster: false,

      refreshProfiles: async () => {
        if (get().profilesLoading || get().profileBusy) return;
        set({ profilesLoading: true, profilesError: "" });
        try {
          const [profiles, envTags, connection] = await Promise.all([
            api.listKafkaProfiles(), api.listEnvTags(), api.getKafkaConnectionStatus(),
          ]);
          const selectedId = profiles.some(p => p.id === get().selectedId) ? get().selectedId
            : connection.profileId || profiles[0]?.id || "";
          adoptConnection(connection);
          set({ profiles, envTags, selectedId });
          if (connection.connected && !get().topics.length) await get().loadClusterData();
        } catch (e) { set({ profilesError: errorMessage(e) }); }
        finally { set({ profilesLoading: false }); }
      },

      selectProfile: (id) => set({ selectedId: id, error: "", testResult: null }),

      connect: async (id) => {
        if (get().profileBusy || get().publishing || get().profilesLoading) return;
        set({ profileBusy: true, selectedId: id, phase: "connecting", error: "", testResult: null });
        try {
          await api.connectKafka(id);
          adoptConnection({ connected: true, profileId: id });
          set({ phase: "idle" });
          await get().loadClusterData();
        } catch (e) {
          // A failed replacement leaves the old broker alive on the Go side.
          set({ phase: "error", error: errorMessage(e) });
        } finally { set({ profileBusy: false }); }
      },

      disconnect: async () => {
        if (get().profileBusy || get().publishing) return;
        set({ profileBusy: true, error: "" });
        try { await api.disconnectKafka(); }
        catch (e) { set({ error: errorMessage(e) }); }
        finally {
          try { adoptConnection(await api.getKafkaConnectionStatus()); }
          catch (e) { adoptConnection({ connected: false }); set({ error: errorMessage(e) }); }
          set({ phase: "idle", profileBusy: false });
        }
      },

      testConnection: async (id) => {
        if (get().testing || get().profileBusy) return;
        set({ testing: true, testResult: null });
        try {
          await api.testKafkaConnection(id);
          drafts.delete(id);
        if (get().selectedId === id) set({ testResult: { ok: true, message: "Connection OK" } });
        } catch (e) {
          drafts.delete(id);
        if (get().selectedId === id) set({ testResult: { ok: false, message: errorMessage(e) } });
        } finally { set({ testing: false }); }
      },

      deleteProfile: async (id) => {
        if (get().profileBusy || get().publishing || get().profilesLoading) throw new Error("Wait for the current operation to finish.");
        set({ profileBusy: true });
        try { await api.deleteKafkaProfile(id); }
        finally {
          try { adoptConnection(await api.getKafkaConnectionStatus()); }
          catch (e) { adoptConnection({ connected: false }); throw e; }
          finally { set({ profileBusy: false }); }
        }
        drafts.delete(id);
        if (get().selectedId === id) set({ selectedId: "" });
        await get().refreshProfiles();
      },

      createProfile: async (input) => {
        const created = await api.createKafkaProfile(input);
        set({ showNewCluster: false, selectedId: created.id });
        await get().refreshProfiles();
      },

      loadClusterData: async () => {
        if (!get().connection.connected || get().topicsLoading) return;
        const epoch = revision;
        set({ topicsLoading: true, topicsError: "" });
        void get().refreshGroups();
        try {
          const topics = await api.listKafkaTopics();
          if (epoch !== revision) return;
          set({ topics });
          if (!topics.some(t => t.name === get().selectedTopic)) await get().selectTopic(topics[0]?.name ?? "");
        } catch (e) { if (epoch === revision) set({ topicsError: errorMessage(e) }); }
        finally { if (epoch === revision) set({ topicsLoading: false }); }
      },

      selectTopic: async (name) => {
        get().cancelPeek();
        const epoch = revision;
        const describeId = ++describeRunId;
        set({
          selectedTopic: name,
          messages: [],
          peeked: false,
          peekError: "",
          peekDurationMs: null,
          resultPartition: null,
          inspected: null,
          topicDetail: null,
          publish: get().publish.topic || get().publish.payload ? get().publish : { ...get().publish, topic: name },
        });
        if (!name) return;
        try {
          const detail = await api.describeKafkaTopic(name);
          // The topic changed under us while describe was in flight.
          if (epoch !== revision || describeId !== describeRunId || get().selectedTopic !== name) return;
          set({ topicDetail: detail, peekPartition: detail.partitions[0] ?? 0 });
        } catch (e) {
          if (epoch === revision && describeId === describeRunId && get().selectedTopic === name) set({ peekError: errorMessage(e) });
        }
      },

      setPeek: (patch) => set({ ...patch, peekError: "" }),

      peek: async () => {
        const { selectedTopic, peekPartition, peekLimit, peekOffset } = get();
        if (!selectedTopic || !get().topicDetail || !get().connection.connected || get().profileBusy || get().peeking) return;
        const errors = validatePeek({ partition: peekPartition, limit: peekLimit, offset: peekOffset });
        if (Object.keys(errors).length) { set({ peekError: Object.values(errors).join(" ") }); return; }
        const runId = get().peekRunId + 1;
        // messages and peekDurationMs survive: the previous table stays on screen,
        // dimmed, until this peek answers (smoothness 5).
        set({ peeking: true, peekError: "", inspected: null, peekRunId: runId });
        const startedAt = Date.now();
        const call = api.peekKafkaMessages(selectedTopic, peekPartition, Number(peekLimit), peekOffset === "" ? -1 : Number(peekOffset));
        inFlightPeek = call;
        try {
          const messages = await call;
          if (get().peekRunId !== runId) return;
          set({ messages, peeked: true, resultPartition: peekPartition, peekDurationMs: Date.now() - startedAt, peeking: false });
        } catch (e) {
          if (get().peekRunId !== runId) return;
          if (isCancelError(e)) {
            set({ peeking: false });
            return;
          }
          // The rows already on screen came out of the broker and are still true;
          // the failure belongs beside them, not in place of them.
          set({ peeked: true, peekError: errorMessage(e), peeking: false });
        } finally {
          if (inFlightPeek === call) inFlightPeek = null;
        }
      },

      /** Stops the peek at the broker: the bridge call carries a Go context. */
      cancelPeek: () => {
        const call = inFlightPeek;
        inFlightPeek = null;
        set({ peeking: false, peekRunId: get().peekRunId + 1 });
        call?.cancel();
      },

      refreshGroups: async () => {
        if (!get().connection.connected || get().groupsLoading) return;
        const epoch = revision;
        set({ groupsLoading: true, groupsError: "" });
        try {
          const groups = await api.listKafkaConsumerGroups();
          if (epoch === revision) set({ groups });
        } catch (e) { if (epoch === revision) set({ groupsError: errorMessage(e) }); }
        finally { if (epoch === revision) set({ groupsLoading: false }); }
      },

      setPublish: (patch) => {
        if (get().publishing) return;
        set({ publish: { ...get().publish, ...patch }, publishError: "", publishResult: null });
      },

      publishMessage: async () => {
        const { publish } = get();
        if (!get().connection.connected || get().profileBusy || get().publishing) return;
        const errors = validatePublish(publish);
        if (Object.keys(errors).length) { set({ publishError: Object.values(errors).join(" ") }); return; }
        set({ publishing: true, publishError: "", publishResult: null });
        try {
          const result = await api.publishKafkaMessage({
            topic: publish.topic.trim(),
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
    };
  });
}

export const useKafkaStore = createKafkaStore();
