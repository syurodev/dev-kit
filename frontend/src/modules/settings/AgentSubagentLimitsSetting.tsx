import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { agentHubGetSubagentLimits, agentHubSetSubagentLimits } from "../../bridge/agenthub";
import { errorMessage } from "../../bridge/errors";
import { SubagentLimitsEditor } from "./SubagentLimitsEditor";

const LIMITS_KEY = ["agenthub", "subagentLimits"] as const;

/** The cap on Codex subagents; read and saved through the UI-only bridge methods. */
export function AgentSubagentLimitsSetting() {
  const client = useQueryClient();
  const [saving, setSaving] = useState(false);
  const [failed, setFailed] = useState("");
  const stored = useQuery({ queryKey: LIMITS_KEY, queryFn: agentHubGetSubagentLimits });

  async function onSave(next: number | null): Promise<boolean> {
    if (!stored.data) return false;
    setSaving(true);
    setFailed("");
    try {
      client.setQueryData(LIMITS_KEY, await agentHubSetSubagentLimits(stored.data.revision, next));
      return true;
    } catch (cause) {
      setFailed(errorMessage(cause));
      // A stale revision is fixed by reading again.
      void client.invalidateQueries({ queryKey: LIMITS_KEY });
      return false;
    } finally {
      setSaving(false);
    }
  }

  return (
    <SubagentLimitsEditor
      stored={stored.data?.codexMaxSubagents ?? null}
      saving={saving || stored.isPending}
      error={failed || (stored.error ? errorMessage(stored.error) : "")}
      onSave={onSave}
    />
  );
}
