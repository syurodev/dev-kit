import { Workflow } from "lucide-react";
import { lazyModuleView, type ModuleManifest } from "../registry";

export const kafkaModule: ModuleManifest = {
  id: "kafka",
  title: "Kafka",
  version: "1.0.0",
  apiVersion: "1",
  icon: Workflow,
  // Keeps the shell's padding until this module owns its own chrome.
  padded: true,
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
};
