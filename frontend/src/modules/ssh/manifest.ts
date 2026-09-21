import { Terminal } from "lucide-react";
import { INFRA_GROUP } from "../groups";
import { lazyModuleView, type ModuleManifest } from "../registry";

export const sshModule: ModuleManifest = {
  id: "ssh",
  title: "SSH",
  version: "1.0.0",
  apiVersion: "1",
  icon: Terminal,
  group: INFRA_GROUP,
  // Keeps the shell's padding until this module owns its own chrome.
  padded: true,
  capabilities: ["ssh:connect", "ssh:forward"],
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
