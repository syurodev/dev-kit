import { RefreshCw } from "lucide-react";
import { lazyModuleView, type ModuleManifest } from "../registry";

export const syncModule: ModuleManifest = {
  id: "sync",
  title: "Sync",
  version: "1.0.0",
  apiVersion: "1",
  icon: RefreshCw,
  // Keeps the shell's padding until this module owns its own chrome.
  padded: true,
  capabilities: ["sync:manage"],
  commands: [
    {
      id: "sync.run",
      title: "Sync now",
    },
  ],
  views: [
    {
      id: "sync.status",
      title: "Sync",
      ...lazyModuleView(() =>
        import("./SyncView").then(({ SyncView }) => ({ default: SyncView })),
      ),
    },
  ],
};
