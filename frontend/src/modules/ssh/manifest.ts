import { createElement, lazy } from "react";
import { useSSHStore } from "./sshStore";
import { Terminal } from "lucide-react";
import { INFRA_GROUP } from "../groups";
import { lazyModuleView, type ModuleManifest } from "../registry";
import { registerToolResultView } from "@/workbench/toolResultViews";
import { parseRunCommand } from "./toolViews/parse";

export const sshModule: ModuleManifest = {
  id: "ssh",
  title: "SSH",
  tabs: true,
  newTab: () => useSSHStore.getState().setShowNewConnection(true),
  sidebar: lazy(() => import("./SSHSidebar").then(({ SSHSidebar }) => ({ default: SSHSidebar }))),
  version: "1.0.0",
  apiVersion: "1",
  icon: Terminal,
  group: INFRA_GROUP,
  // One document per connection; the module rail remains available with no tabs.
  padded: false,
  capabilities: ["ssh:connect", "ssh:forward", "terminal:io"],
  commands: [
    {
      id: "ssh.run",
      title: "Run SSH command",
    },
  ],
  views: [
    {
      id: "ssh.connections",
      title: "SSH",
      ...lazyModuleView(() =>
        import("./SSHView").then(({ SSHView }) => ({ default: SSHView })),
      ),
    },
  ],
};

/**
 * ToolResultView for `ssh.runCommand` (docs/dev-kit/tool-result-view.mdx): the
 * shape is checked synchronously before the card's chunk loads; anything but
 * `{output}` falls back to the generic block.
 */
const LazyCommandResultCard = lazy(() =>
  import("./toolViews/CommandResultCard").then(({ CommandResultCard }) => ({ default: CommandResultCard })),
);
registerToolResultView("ssh.runCommand", ({ rawInput, rawOutput }) => {
  const result = parseRunCommand(rawInput, rawOutput);
  return result ? createElement(LazyCommandResultCard, { result }) : null;
});
