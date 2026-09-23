import { RefreshCw } from "lucide-react";
import { lazyModuleView, type ModuleManifest } from "../registry";

export const syncModule: ModuleManifest = {
  id: "sync",
  title: "Sync",
  // One view, the sync status. Its home costs nothing to open, so it opens
  // rather than showing "has nothing open" (ModuleManifest.opensHome).
  opensHome: true,
  version: "1.0.0",
  apiVersion: "1",
  icon: RefreshCw,
  // The view draws its own 24px inset (Figma 779:206).
  padded: false,
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
