import { Download, X } from "lucide-react";

import { Button } from "@/components/ui/button";
import {
  RuntimeInstallLog,
  RuntimeRequirements,
  useAgentRuntime,
} from "@/modules/settings/AgentRuntimeSetting";
import { vendorInfo } from "./vendors";

/**
 * What the draft shows instead of sending when the picked agent cannot be
 * started: not installed, or installed but missing what its install needs
 * (Node/npm). The install runs right here, with the same log as Settings →
 * Agents. Any vendor with a runtime (vendors.ts) gets it, named for itself.
 */
export function AgentRuntimeSetup({ vendor, onOpenSettings }: { vendor: string; onOpenSettings: () => void }) {
  const info = vendorInfo(vendor);
  const rt = useAgentRuntime(info.runtimeId);
  const s = rt.status;
  if (!s) return null;
  const unmet = s.requirements.filter((r) => !r.met);

  return (
    <div
      data-testid="agent-runtime-setup"
      className="flex w-full flex-col items-center gap-3 rounded-sm bg-(--state-soft) p-4 text-center"
    >
      <p className="text-body-13-medium text-text-primary">
        {s.current ? `${info.runtimeName} can't start` : `${info.runtimeName} isn't installed`}
      </p>
      <p className="text-caption-11 text-text-tertiary">
        {s.current
          ? "Something it needs is missing from this machine."
          : s.testedVersion
            ? `DevKit installs the version it has tested (${s.testedVersion}) into its own folder — nothing goes on your PATH.`
            : "DevKit installs the latest release into its own folder — nothing goes on your PATH."}
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
            {`Install ${info.runtimeName}`}
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
