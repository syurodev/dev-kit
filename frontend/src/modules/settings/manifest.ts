import { Settings } from "lucide-react";
import { lazyModuleView, type ModuleManifest } from "../registry";

export const settingsModule: ModuleManifest = {
  id: "settings",
  title: "Settings",
  version: "1.0.0",
  apiVersion: "1",
  icon: Settings,
  capabilities: ["env:read", "env:write", "preferences:read", "preferences:write"],
  commands: [],
  views: [
    {
      id: "settings.environments",
      title: "Settings",
      ...lazyModuleView(() =>
        import("./SettingsView").then(({ SettingsView }) => ({ default: SettingsView })),
      ),
    },
  ],
};
