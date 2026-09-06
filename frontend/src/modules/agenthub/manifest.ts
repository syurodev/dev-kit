import { Bot } from "lucide-react";
import { lazyModuleView, type ModuleManifest } from "../registry";

export const agenthubModule: ModuleManifest = {
  id: "agenthub",
  title: "Agent Hub",
  version: "1.0.0",
  apiVersion: "1",
  icon: Bot,
  // Bleed, not padded. Figma `_Slot / App Content` 705:47988: "Bleed = module
  // owns the surface; Padded = shell owns the inset ... Registry default is
  // bleed", and Padded is meant for a module that is a list or a form. Agent
  // Hub is neither: AgentHubView draws its own full-bleed `bg-surface-base`
  // with a swarm rail and a content pane, so the shell's p-4 was stacking a
  // second 16px inset on top of the 8px window gutter and reading as a thick
  // dead border around the module. The card clips at radius/10 and this module
  // draws its own inner surfaces, which is exactly what bleed requires.
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
