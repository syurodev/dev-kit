import { Workflow } from "lucide-react";
import { INFRA_GROUP } from "../groups";
import { lazyModuleView, type ModuleManifest } from "../registry";

export const kafkaModule: ModuleManifest = {
  id: "kafka",
  title: "Kafka",
  version: "1.0.0",
  apiVersion: "1",
  icon: Workflow,
  group: INFRA_GROUP,
  // The view draws its own 24px inset (Figma 1137:17).
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
