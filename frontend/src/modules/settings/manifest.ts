import { Settings } from "lucide-react";
import { lazyModuleView, type ModuleManifest } from "../registry";

export const settingsModule: ModuleManifest = {
  id: "settings",
  title: "Settings",
  // One place to be; an empty Settings asked a question with one answer. Its home costs nothing to open, so it opens
  // rather than showing "has nothing open" (ModuleManifest.opensHome).
  opensHome: true,
  version: "1.0.0",
  apiVersion: "1",
  icon: Settings,
  // Keeps the shell's padding until this module owns its own chrome.
  padded: true,
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
