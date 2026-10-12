import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Send } from "lucide-react";
import { toast } from "sonner";
import { telegramStatus, setTelegramSessionEnabled } from "@/bridge/telegram";
import { errorMessage } from "@/bridge/errors";
import type { AgentSession } from "@/bridge/agenthub";
import type { MenuKit } from "./actions";

const LIVE_API = {
  status: telegramStatus,
  setEnabled: setTelegramSessionEnabled,
};
export type SessionRemoteControlAPI = typeof LIVE_API;

export function SessionRemoteControlItem({
  session,
  Item,
  api = LIVE_API,
}: {
  session: AgentSession;
  Item: MenuKit["Item"];
  api?: SessionRemoteControlAPI;
}) {
  const client = useQueryClient();
  const key = [
    "settings",
    "telegram",
    api === LIVE_API ? "live" : "session-preview",
  ];
  const { data } = useQuery({
    queryKey: key,
    queryFn: api.status,
    staleTime: 0,
  });
  const [busy, setBusy] = useState(false);
  const enabled = data?.config.sessionIds?.includes(session.id) ?? false;
  const allowed = Boolean(
    session.workspaceId &&
      data?.config.workspaces?.includes(session.workspaceId),
  );
  return (
    <Item
      icon={<Send />}
      disabled={busy || !data || (!enabled && !allowed)}
      onSelect={async () => {
        setBusy(true);
        try {
          await api.setEnabled(session.id, !enabled);
          await client.invalidateQueries({ queryKey: key });
        } catch (e) {
          toast.error("Could not change remote control", {
            description: errorMessage(e),
          });
        } finally {
          setBusy(false);
        }
      }}
    >
      {enabled ? "Disable remote control" : "Enable remote control"}
    </Item>
  );
}
