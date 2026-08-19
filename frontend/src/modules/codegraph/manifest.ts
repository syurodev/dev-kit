import { Network } from "lucide-react";
import { lazyModuleView, type ModuleManifest } from "../registry";

export const codegraphModule: ModuleManifest = {
  id: "codegraph",
  title: "Code Graph",
  version: "1.0.0",
  apiVersion: "1",
  icon: Network,
  capabilities: ["codegraph:index", "codegraph:read"],
  commands: [
    {
      id: "codegraph.openProject",
      title: "Open Java project",
    },
  ],
  views: [
    {
      id: "codegraph.explorer",
      title: "Code Graph",
      ...lazyModuleView(() =>
        import("./CodeGraphView").then(({ CodeGraphView }) => ({ default: CodeGraphView })),
      ),
    },
  ],
};
