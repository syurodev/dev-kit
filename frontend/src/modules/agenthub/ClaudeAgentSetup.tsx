import { Download, X } from "lucide-react";

import { Button } from "@/components/ui/button";
import {
  RuntimeInstallLog,
  RuntimeRequirements,
  useAgentRuntime,
} from "@/modules/settings/AgentRuntimeSetting";

/**
 * What the new-session dialog shows instead of its form when Claude cannot be
 * started: not installed, or installed but missing Node/npm. The install runs
 * right here, with the same log as Settings → Agents.
 */
export function ClaudeAgentSetup({ onOpenSettings }: { onOpenSettings: () => void }) {
  const rt = useAgentRuntime();
  const s = rt.status;
  if (!s) return null;
  const unmet = s.requirements.filter((r) => !r.met);

  return (
    <div
      data-testid="claude-agent-setup"
      className="flex w-full flex-col items-center gap-3 rounded-sm bg-(--state-soft) p-4 text-center"
    >
      <p className="text-body-13-medium text-text-primary">
        {s.current ? "Claude Agent can't start" : "Claude Agent isn't installed"}
      </p>
      <p className="text-caption-11 text-text-tertiary">
        {s.current
          ? "Something it needs is missing from this machine."
          : `DevKit installs the version it has tested (${s.testedVersion}) into its own folder — nothing goes on your PATH.`}
      </p>
      {unmet.length > 0 ? <RuntimeRequirements requirements={unmet} /> : null}
      <div className="flex items-center gap-2">
        {rt.installing ? (
          <Button type="button" variant="outline" size="sm" disabled={!rt.canCancel} onClick={rt.cancel}>
            <X />
            Cancel install
          </Button>
        ) : !s.current ? (
          <Button type="button" size="sm" disabled={rt.busy} onClick={() => rt.install()}>
            <Download />
            Install Claude Agent
          </Button>
        ) : null}
        <Button type="button" variant="ghost" size="sm" onClick={onOpenSettings}>
          Open agent settings
        </Button>
      </div>
      {rt.error ? <p className="text-caption-11 text-status-danger">{rt.error}</p> : null}
      <RuntimeInstallLog lines={rt.lines} installing={rt.installing} failed={Boolean(rt.error)} />
    </div>
  );
}
