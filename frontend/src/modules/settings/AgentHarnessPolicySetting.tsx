import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { Switch } from "@/components/ui/switch";
import { errorMessage } from "../../bridge/errors";
import { harnessAvailable, harnessPolicy, setAutoNarrowing } from "../../bridge/harness";

const POLICY_KEY = ["harness", "policy"] as const;
const WIRED_KEY = ["harness", "available"] as const;

/**
 * Whether an agent's request to make result limits tighter may be applied without
 * asking. Off unless you turn it on, and it covers that one kind of request only:
 * anything that raises a limit, anything you change yourself in the window, a
 * request for tool groups and every other change still asks, and it never approves
 * a tool call, an environment confirmation or a permission. The switch saves the
 * revision it read, so a change made elsewhere is a conflict, not an overwrite.
 *
 * It appears only where the host has the tool-group controls wired.
 */
export function AgentHarnessPolicySetting() {
  const client = useQueryClient();
  const [failed, setFailed] = useState("");
  const { data: wired } = useQuery({ queryKey: WIRED_KEY, queryFn: harnessAvailable });
  const { data: policy, error: readError } = useQuery({ queryKey: POLICY_KEY, queryFn: harnessPolicy, enabled: wired === true });

  const save = useMutation({
    mutationFn: (on: boolean) => setAutoNarrowing(policy?.revision ?? 0, on),
    onSuccess: (next) => {
      setFailed("");
      client.setQueryData(POLICY_KEY, next);
    },
    onError: (cause) => {
      setFailed(errorMessage(cause));
      // The stored value moved: show it, and let the person choose again.
      void client.invalidateQueries({ queryKey: POLICY_KEY });
    },
  });

  if (wired !== true) {
    return null;
  }
  const message = failed || (readError ? errorMessage(readError) : "");

  return (
    <div className="flex flex-col gap-2">
      <label className="flex items-center gap-2.5 rounded-control border border-border-subtle bg-surface-raised py-2.5 pr-3 pl-3.5">
        <span className="min-w-0 flex-1 text-body-13 text-text-primary">
          Let an agent tighten result limits without asking
        </span>
        <Switch
          aria-label="Let an agent tighten result limits without asking"
          checked={policy?.autoNarrowing ?? false}
          disabled={policy === undefined || save.isPending}
          onCheckedChange={(on) => save.mutate(on)}
        />
      </label>
      <p className="text-body-12 text-text-tertiary">
        Off by default. When on, an agent's request that only lowers a result limit or changes how results are cut is applied at once and
        shows up in the workspace's limits. A request that raises any limit, anything you change yourself, a request for tool groups and
        every other change still asks. It never approves running a tool.
      </p>
      {message ? (
        <p role="alert" className="text-body-12 text-status-danger">
          {message}
        </p>
      ) : null}
    </div>
  );
}
