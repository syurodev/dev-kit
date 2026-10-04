import { Terminal } from "lucide-react";
import { INFRA_GROUP } from "../groups";
import { lazyModuleView, type ModuleManifest } from "../registry";

export const sshModule: ModuleManifest = {
  id: "ssh",
  title: "SSH",
  // The connection list is the module. Its home costs nothing to open, so it opens
  // rather than showing "has nothing open" (ModuleManifest.opensHome).
  opensHome: true,
  version: "1.0.0",
  apiVersion: "1",
  icon: Terminal,
  group: INFRA_GROUP,
  // Owns its sidebar and connection workspace.
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
