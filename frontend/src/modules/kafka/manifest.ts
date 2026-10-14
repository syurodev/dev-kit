import { createElement, lazy } from "react";
import { Workflow } from "lucide-react";
import { INFRA_GROUP } from "../groups";
import { lazyModuleView, type ModuleManifest } from "../registry";
import { registerIntent } from "@/workbench/intents";
import { registerToolResultView } from "@/workbench/toolResultViews";
import {
  parseConsumerGroups,
  parsePeekMessages,
  parsePublishResult,
  parseTopicDetail,
  parseTopicList,
} from "./toolViews/parse";

export const kafkaModule: ModuleManifest = {
  id: "kafka",
  title: "Kafka",
  // One view, the cluster explorer: "nothing open" had nothing else to offer. Its home costs nothing to open, so it opens
  // rather than showing "has nothing open" (ModuleManifest.opensHome).
  opensHome: true,
  sidebar: lazy(() => import("./KafkaSidebar").then(({ KafkaSidebar }) => ({ default: KafkaSidebar }))),
  version: "1.0.0",
  apiVersion: "1",
  icon: Workflow,
  group: INFRA_GROUP,
  // The explorer owns its responsive inset; the manifest owns its rail.
  padded: false,
  capabilities: ["kafka:connect", "kafka:produce"],
  commands: [
    {
      id: "kafka.publish",
      title: "Publish message",
    },
  ],
  views: [
    {
      id: "kafka.explorer",
      title: "Kafka",
      ...lazyModuleView(() =>
        import("./KafkaView").then(({ KafkaView }) => ({ default: KafkaView })),
      ),
    },
  ],
  paletteSearch: {
    noun: "topics and clusters",
    search: (query, signal) => import("./paletteSearch").then((m) => m.searchPalette(query, signal)),
  },
};

/**
 * `kafka.reveal` — a topic named elsewhere (a tool result card, a reference in
 * agent prose) opened in the explorer, the way the palette opens one. Kafka has
 * one view and no documents, so `placement` has nothing to choose between. A
 * topic is selected only on a live connection: its topics are that cluster's,
 * and selecting one with nothing connected would only fail to describe it.
 */
registerIntent("kafka.reveal", async (ref) => {
  if (ref.kind !== "topic" || !ref.id) return "unavailable";
  const [{ useKafkaStore }, { showModuleHome }] = await Promise.all([
    import("./kafkaStore"),
    import("@/workbench/showModule"),
  ]);
  const store = useKafkaStore.getState();
  if (store.connection.connected) {
    void store.selectTopic(ref.id);
    store.setActiveTab("topics");
  }
  showModuleHome("kafka");
  return "ok";
});

/**
 * ToolResultViews for the kafka.* tools (docs/dev-kit/tool-result-view.mdx).
 * Each shape is checked synchronously before the cards' chunk loads; one that
 * does not parse falls back to the generic block.
 */
const cards = () => import("./toolViews/KafkaResultCards");
const LazyPeekResultCard = lazy(() => cards().then(({ PeekResultCard }) => ({ default: PeekResultCard })));
const LazyTopicsResultCard = lazy(() => cards().then(({ TopicsResultCard }) => ({ default: TopicsResultCard })));
const LazyTopicDetailResultCard = lazy(() =>
  cards().then(({ TopicDetailResultCard }) => ({ default: TopicDetailResultCard })),
);
const LazyConsumerGroupsResultCard = lazy(() =>
  cards().then(({ ConsumerGroupsResultCard }) => ({ default: ConsumerGroupsResultCard })),
);
const LazyPublishResultCard = lazy(() => cards().then(({ PublishResultCard }) => ({ default: PublishResultCard })));

registerToolResultView("kafka.peekMessages", ({ rawInput, rawOutput }) => {
  const peek = parsePeekMessages(rawInput, rawOutput);
  return peek ? createElement(LazyPeekResultCard, { peek }) : null;
});
registerToolResultView("kafka.listTopics", ({ rawOutput }) => {
  const topics = parseTopicList(rawOutput);
  return topics ? createElement(LazyTopicsResultCard, { topics }) : null;
});
registerToolResultView("kafka.describeTopic", ({ rawOutput }) => {
  const detail = parseTopicDetail(rawOutput);
  return detail ? createElement(LazyTopicDetailResultCard, { detail }) : null;
});
registerToolResultView("kafka.listConsumerGroups", ({ rawOutput }) => {
  const groups = parseConsumerGroups(rawOutput);
  return groups ? createElement(LazyConsumerGroupsResultCard, { groups }) : null;
});
registerToolResultView("kafka.publishMessage", ({ rawInput, rawOutput }) => {
  const result = parsePublishResult(rawInput, rawOutput);
  return result ? createElement(LazyPublishResultCard, { result }) : null;
});
