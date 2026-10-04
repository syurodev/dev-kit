import { lazy } from "react";
import { Workflow } from "lucide-react";
import { INFRA_GROUP } from "../groups";
import { lazyModuleView, type ModuleManifest } from "../registry";

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
