import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Switch } from "@/components/ui/switch";
import { errorMessage } from "../../bridge/errors";
import { revealRequiresPassword, setRevealRequiresPassword } from "../../bridge/accounts";

const QUERY_KEY = ["accounts", "reveal-requires-password"] as const;

/**
 * Whether showing a credential's value asks for the Master Password again.
 *
 * It is a choice rather than a rule because the cost falls on exactly the
 * person making it, and somebody rotating a token ten times a day knows what
 * they are trading. On by default, because the person who has not thought
 * about it yet is the one the prompt is for.
 *
 * The switch only says what will be asked for — the backend re-reads this
 * preference on every reveal, so turning it off here is not what lets a value
 * out, and a caller that skipped this screen entirely would still be asked.
 */
export function RevealPasswordSetting() {
  const client = useQueryClient();
  const [failed, setFailed] = useState("");

  const { data, error: readError } = useQuery({
    queryKey: QUERY_KEY,
    queryFn: revealRequiresPassword,
  });

  const save = useMutation({
    mutationFn: setRevealRequiresPassword,
    onSuccess: (_, required) => {
      setFailed("");
      client.setQueryData(QUERY_KEY, required);
    },
    onError: (cause) => setFailed(errorMessage(cause)),
  });

  const message = failed || (readError ? errorMessage(readError) : "");

  return (
    <div className="flex flex-col gap-2">
      <label className="flex items-center gap-2.5 rounded-control border border-border-subtle bg-surface-raised py-2.5 pr-3 pl-3.5">
        <span className="min-w-0 flex-1 text-body-13 text-text-primary">
          Ask for the Master Password before showing a credential
        </span>
        <Switch
          // Unset means on, and a switch that has not loaded yet must not draw
          // itself as off — that reads as a guard that is already down.
          checked={data ?? true}
          disabled={data === undefined || save.isPending}
          onCheckedChange={(checked) => save.mutate(checked)}
        />
      </label>

      <p className="text-body-12 text-text-tertiary">
        Off leaves showing a value behind a confirmation only. Checking the password
        never locks or unlocks the vault, and every reveal is written to the audit log —
        the provider and the subject, never the value.
      </p>

      {message ? <p className="text-body-12 text-status-danger">{message}</p> : null}
    </div>
  );
}
