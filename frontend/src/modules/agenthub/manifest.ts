import { Bot } from "lucide-react";
import type { ModuleManifest } from "../registry";
import { AgentHubView } from "./AgentHubView";

export const agenthubModule: ModuleManifest = {
  id: "agenthub",
  title: "Agent Hub",
  version: "1.0.0",
  apiVersion: "1",
  icon: Bot,
  capabilities: ["agenthub:read", "agenthub:execute"],
  commands: [
    { id: "agenthub.create", title: "New agent session" },
    { id: "agenthub.invoke", title: "Send prompt" },
    { id: "agenthub.cancel", title: "Cancel turn" },
  ],
  views: [{ id: "agenthub.sessions", title: "Agents", component: AgentHubView }],
};
