import { Bot } from "lucide-react";
import { lazyModuleView, type ModuleManifest } from "../registry";

export const agenthubModule: ModuleManifest = {
  id: "agenthub",
  title: "Agent Hub",
  version: "1.0.0",
  apiVersion: "1",
  icon: Bot,
  // Keeps the shell's padding until this module owns its own chrome.
  padded: true,
  capabilities: ["agenthub:read", "agenthub:execute"],
  commands: [
    { id: "agenthub.create", title: "New agent session" },
    { id: "agenthub.invoke", title: "Send prompt" },
    { id: "agenthub.cancel", title: "Cancel turn" },
  ],
  views: [
    {
      id: "agenthub.sessions",
      title: "Agents",
      ...lazyModuleView(() =>
        import("./AgentHubView").then(({ AgentHubView }) => ({ default: AgentHubView })),
      ),
    },
  ],
};
