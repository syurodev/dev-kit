import { Settings } from "lucide-react";
import type { ModuleManifest } from "../registry";
import { SettingsView } from "./SettingsView";

export const settingsModule: ModuleManifest = {
  id: "settings",
  title: "Settings",
  version: "1.0.0",
  apiVersion: "1",
  icon: Settings,
  capabilities: ["env:read", "env:write"],
  commands: [],
  views: [
    {
      id: "settings.environments",
      title: "Settings",
      component: SettingsView,
    },
  ],
};
