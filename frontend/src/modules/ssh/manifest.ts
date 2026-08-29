import { Terminal } from "lucide-react";
import { lazyModuleView, type ModuleManifest } from "../registry";

export const sshModule: ModuleManifest = {
  id: "ssh",
  title: "SSH",
  version: "1.0.0",
  apiVersion: "1",
  icon: Terminal,
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
