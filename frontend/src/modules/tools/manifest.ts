import { Wrench } from "lucide-react";
import { lazyModuleView, type ModuleManifest } from "../registry";

export const toolsModule: ModuleManifest = {
  id: "tools",
  title: "Tools",
  version: "1.0.0",
  apiVersion: "1",
  icon: Wrench,
  // Keeps the shell's padding until this module owns its own chrome.
  padded: true,
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
      ...lazyModuleView(() =>
        import("./ToolsView").then(({ ToolsView }) => ({ default: ToolsView })),
      ),
    },
  ],
};
