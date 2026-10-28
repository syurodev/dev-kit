import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { agentIdleRelease, setAgentIdleRelease } from "../../bridge/idleRelease";
import { errorMessage } from "../../bridge/errors";
import { IdleReleaseEditor, type IdleReleaseValue } from "./IdleReleaseEditor";

const IDLE_KEY = ["agenthub", "idleRelease"] as const;

/** Releasing the process of idle agent sessions; read and saved through the UI-only bridge methods. */
export function AgentIdleReleaseSetting() {
  const client = useQueryClient();
  const [saving, setSaving] = useState(false);
  const [failed, setFailed] = useState("");
  const stored = useQuery({ queryKey: IDLE_KEY, queryFn: agentIdleRelease });

  async function onSave(next: IdleReleaseValue): Promise<boolean> {
    setSaving(true);
    setFailed("");
    try {
      client.setQueryData(IDLE_KEY, await setAgentIdleRelease(next));
      return true;
    } catch (cause) {
      setFailed(errorMessage(cause));
      void client.invalidateQueries({ queryKey: IDLE_KEY });
      return false;
    } finally {
      setSaving(false);
    }
  }

  return (
    <IdleReleaseEditor
      stored={stored.data ?? null}
      saving={saving || stored.isPending}
      error={failed || (stored.error ? errorMessage(stored.error) : "")}
      onSave={onSave}
    />
  );
}
