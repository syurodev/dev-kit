import { lazy } from "react";
import { Bot } from "lucide-react";
import { lazyModuleView, type ModuleManifest } from "../registry";
import { AgentHubBadge, AgentHubPeek } from "./AgentHubSwitcher";
import { useAgentSessionsStore } from "../../stores/agentSessions";
import { registerAttachIntent } from "@/workbench/intents";
import { openNewAgentSession } from "./documents";
import { registerPdfViews } from "./toolViews/registerPdfViews";
import { registerMessageViews } from "./toolViews/registerMessageViews";

function requestAgentHubCommand(command: "create" | "cancel" | "focus") {
  useAgentSessionsStore.getState().requestAgentHubCommand(command);
}

export const agenthubModule: ModuleManifest = {
  id: "agenthub",
  title: "Agent Hub",
  // The session list is where every Agent Hub visit starts. Its home costs nothing to open, so it opens
  // rather than showing "has nothing open" (ModuleManifest.opensHome).
  opensHome: true,
  // No strip: the sidebar is how you move between sessions. The session on
  // screen is a preview tab that the next one replaces (documents.ts), so the
  // module never piles up tabs nobody can see; the home tab is where a new
  // session is drafted. `newTab` stays for the module's own "new" entry points
  // (the title-bar menu, the nothing-open state), which have no strip to share.
  tabs: false,
  newTab: () => openNewAgentSession(),
  // The swarm tree, as the module's rail: there with no tab open, and not
  // registered by every session tab (AgentHubSidebar).
  sidebar: lazy(() => import("./AgentHubSidebar").then(({ AgentHubSidebar }) => ({ default: AgentHubSidebar }))),
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
  peek: AgentHubPeek,
  badge: AgentHubBadge,
  commands: [
    {
      id: "agenthub.create",
      title: "New agent session",
      run: () => requestAgentHubCommand("create"),
    },
    {
      id: "agenthub.invoke",
      title: "Focus composer",
      run: () => requestAgentHubCommand("focus"),
    },
    {
      id: "agenthub.cancel",
      title: "Cancel turn",
      run: () => requestAgentHubCommand("cancel"),
    },
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
  paletteSearch: {
    noun: "sessions",
    search: (query, signal) => import("./paletteSearch").then((m) => m.searchPalette(query, signal)),
  },
};

/**
 * `agent.attach` (docs/dev-kit/cross-module.mdx): hands the active session's
 * composer a cross-module EntityRef as structured context, without the
 * caller importing Agent Hub's views. The session reads each ref fresh
 * through its owning module's MCP tool when it needs to (see
 * internal/agenthub/attachedref.go) — DevKit never resolves it here.
 *
 * Only queues the request; AgentHubView and AgentCompanion both drain
 * pendingAgentAttach into whichever composer is actually mounted, the same
 * way requestAgentHubCommand reaches whichever surface is showing.
 */
registerAttachIntent("agent.attach", (refs, prompt) => {
  useAgentSessionsStore.getState().requestAgentAttach(refs, prompt);
});

// pdf.* results read as pages, hits and text: a PDF is a document of the
// session that attached it, so its tools' views are this module's.
registerPdfViews();
// A message this session sent another one reads as who it was for and what
// became of it, not as JSON.
registerMessageViews();
