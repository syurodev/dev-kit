import { Wrench } from "lucide-react";
import { UTILITIES_GROUP } from "../groups";
import { lazyModuleView, type ModuleManifest } from "../registry";
import { TOOLS } from "./toolCatalogue";

// One pane serves every tool (ToolsView renders whatever the sidebar selected),
// so all eight views share a single lazy component — but each still needs its
// own id here, because the backend manifest lists all eight and parity is
// compared id-by-id.
const toolsView = lazyModuleView(() =>
  import("./ToolsView").then(({ ToolsView }) => ({ default: ToolsView })),
);

export const toolsModule: ModuleManifest = {
  id: "tools",
  title: "Tools",
  version: "1.0.0",
  apiVersion: "1",
  icon: Wrench,
  group: UTILITIES_GROUP,
  // The view draws its own 24px inset (Figma 1144:16).
  padded: false,
  capabilities: ["tools:hash", "tools:util"],
  // Imported when run, not with the manifest: the manifest is in the initial
  // bundle, the tools store is not.
  commands: TOOLS.map((tool) => ({
    id: tool.id,
    title: tool.name,
    run: () => void import("./openTool").then((m) => m.openTool(tool.id)),
  })),
  views: TOOLS.map((tool) => ({ id: tool.id, title: tool.name, ...toolsView })),
};
