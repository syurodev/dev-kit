import { Workflow } from "lucide-react";
import type { ModuleManifest } from "../registry";
import { KafkaView } from "./KafkaView";

export const kafkaModule: ModuleManifest = {
  id: "kafka",
  title: "Kafka",
  version: "1.0.0",
  apiVersion: "1",
  icon: Workflow,
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
      component: KafkaView,
    },
  ],
};
