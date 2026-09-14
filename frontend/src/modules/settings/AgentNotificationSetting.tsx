import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Switch } from "@/components/ui/switch";
import { errorMessage } from "../../bridge/errors";
import { getAgentNotifications, saveAgentNotifications } from "../../bridge/preferences";

const QUERY_KEY = ["preferences", "agent-notifications"] as const;

/**
 * Whether Agent Hub raises an OS notification when an agent needs you or
 * finishes. On by default.
 *
 * Turning it on is also when DevKit asks the OS for permission to notify —
 * a moment you chose, not the moment an agent first needs you, when the
 * dialog would sit on top of the prompt it is about.
 */
export function AgentNotificationSetting() {
  const client = useQueryClient();
  const [failed, setFailed] = useState("");

  const { data, error: readError } = useQuery({
    queryKey: QUERY_KEY,
    queryFn: getAgentNotifications,
  });

  const save = useMutation({
    mutationFn: saveAgentNotifications,
    onSuccess: (_, enabled) => {
      setFailed("");
      client.setQueryData(QUERY_KEY, enabled);
    },
    onError: (cause) => setFailed(errorMessage(cause)),
  });

  const message = failed || (readError ? errorMessage(readError) : "");

  return (
    <div className="flex flex-col gap-2">
      <label className="flex items-center gap-2.5 rounded-control border border-border-subtle bg-surface-raised py-2.5 pr-3 pl-3.5">
        <span className="min-w-0 flex-1 text-body-13 text-text-primary">
          Notify when an agent needs you or finishes
        </span>
        <Switch
          checked={data ?? true}
          disabled={data === undefined || save.isPending}
          onCheckedChange={(checked) => save.mutate(checked)}
        />
      </label>

      <p className="text-body-12 text-text-tertiary">
        Only while DevKit is not the focused window, one notification per
        session. Turning this on asks your system for permission to show them.
      </p>

      {message ? <p className="text-body-12 text-status-danger">{message}</p> : null}
    </div>
  );
}
