import { Terminal } from "lucide-react";
import type { ModuleManifest } from "../registry";
import { SSHView } from "./SSHView";

export const sshModule: ModuleManifest = {
  id: "ssh",
  title: "SSH",
  version: "1.0.0",
  apiVersion: "1",
  icon: Terminal,
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
      component: SSHView,
    },
  ],
};
