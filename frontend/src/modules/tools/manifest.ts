import type { ModuleManifest } from "../registry";
import { ToolsView } from "./ToolsView";

export const toolsModule: ModuleManifest = {
  id: "tools",
  title: "Tools",
  version: "1.0.0",
  apiVersion: "1",
  capabilities: ["tools:hash"],
  commands: [
    {
      id: "tools.hash",
      title: "Hash text",
    },
  ],
  views: [
    {
      id: "tools.hash",
      title: "Hash",
      component: ToolsView,
    },
  ],
};
