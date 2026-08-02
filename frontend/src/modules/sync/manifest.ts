import type { ModuleManifest } from "../registry";
import { SyncView } from "./SyncView";

export const syncModule: ModuleManifest = {
  id: "sync",
  title: "Sync",
  version: "1.0.0",
  apiVersion: "1",
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
      component: SyncView,
    },
  ],
};
