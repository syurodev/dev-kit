import type { ModuleDefinition } from "../registry";
import { SSHView } from "./SSHView";

export const sshModule: ModuleDefinition = {
  id: "ssh",
  title: "SSH",
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
