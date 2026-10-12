import { useMemo, useState } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  SessionRemoteControlItem,
  type SessionRemoteControlAPI,
} from "@/modules/agenthub/sidebar/SessionRemoteControlItem";
import type { TelegramStatus } from "@/bridge/telegram";
import type { AgentSession } from "@/bridge/agenthub";
const session: AgentSession = {
  id: "preview-session",
  agentType: "codex",
  workspaceId: "preview-workspace",
  projectPath: "/Projects/demo",
  status: "idle",
  parentSessionId: null,
  depth: 0,
  title: "Review billing query",
  acpSessionId: "",
  updatedAt: 0,
};
/** The real menu action, with a local API; no agent or Telegram messages. */
export default function SessionRemoteControlPreview() {
  const [client] = useState(() => new QueryClient());
  const api = useMemo<SessionRemoteControlAPI>(() => {
    let status: TelegramStatus = {
      config: {
        enabled: true,
        accountId: "preview",
        workspaces: ["preview-workspace"],
        sendDetails: true,
        readTranscript: true,
        approveInteractions: false,
        sessionIds: [],
      },
      paired: true,
      userId: 0,
      candidateId: 0,
      candidateName: "",
      botName: "preview",
      error: "",
    };
    return {
      status: async () => structuredClone(status),
      setEnabled: async (id, enabled) => {
        status = {
          ...status,
          config: { ...status.config, sessionIds: enabled ? [id] : [] },
        };
      },
    };
  }, []);
  return (
    <QueryClientProvider client={client}>
      <main className="flex h-svh items-center justify-center bg-surface-base text-text-primary">
        <section className="w-96 rounded-xl border border-border-subtle bg-surface-raised p-5">
          <h1 className="text-body-14 font-medium">Review billing query</h1>
          <p className="my-3 text-body-13 text-text-secondary">
            Remote control is off until you enable this session.
          </p>
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="outline">Session actions</Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent>
              <SessionRemoteControlItem
                session={session}
                Item={DropdownMenuItem}
                api={api}
              />
            </DropdownMenuContent>
          </DropdownMenu>
          <p className="mt-4 text-body-12 text-text-tertiary">
            Local preview · no messages are sent.
          </p>
        </section>
      </main>
    </QueryClientProvider>
  );
}
