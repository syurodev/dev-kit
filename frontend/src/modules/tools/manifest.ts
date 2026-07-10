import type { ModuleDefinition } from "../registry";
import { ToolsView } from "./ToolsView";

export const toolsModule: ModuleDefinition = {
  id: "tools",
  title: "Tools",
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
