import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Browser } from "@wailsio/runtime";
import { Button } from "@/components/ui/button";
import { Switch } from "@/components/ui/switch";
import { errorMessage } from "../../bridge/errors";
import {
  checkForUpdate,
  getCurrentVersion,
  getUpdateChecks,
  saveUpdateChecks,
  updateStatusLine,
  versionLabel,
} from "../../bridge/updates";

const VERSION_KEY = ["updates", "version"] as const;
const CHECKS_KEY = ["updates", "automatic"] as const;

/**
 * The running version, a "Check now" button, and whether DevKit checks by
 * itself at startup.
 *
 * It only ever tells you. There is no Install button on purpose: DevKit
 * downloads nothing, so a newer release is a link to its page and you install
 * it the way you installed this one.
 */
export function UpdatesSetting() {
  const client = useQueryClient();
  const [failed, setFailed] = useState("");

  const { data: version } = useQuery({ queryKey: VERSION_KEY, queryFn: getCurrentVersion, staleTime: Infinity });
  const { data: automatic, error: readError } = useQuery({ queryKey: CHECKS_KEY, queryFn: getUpdateChecks });

  const save = useMutation({
    mutationFn: saveUpdateChecks,
    onSuccess: (_, enabled) => {
      setFailed("");
      client.setQueryData(CHECKS_KEY, enabled);
    },
    onError: (cause) => setFailed(errorMessage(cause)),
  });

  // Not cached: a result is a moment, and stale "you are up to date" is worse
  // than showing nothing until the button is pressed again.
  const check = useMutation({
    mutationFn: () => checkForUpdate(false),
    onMutate: () => setFailed(""),
    onError: (cause) => setFailed(errorMessage(cause)),
  });

  const result = check.data;
  const message = failed || (readError ? errorMessage(readError) : "");

  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center gap-3 rounded-control border border-border-subtle bg-surface-raised py-2.5 pr-3 pl-3.5">
        <div className="flex min-w-0 flex-1 flex-col gap-0.5">
          <span className="text-body-13 text-text-primary">
            DevKit {version ? versionLabel(version) : ""}
          </span>
          <span className="text-body-12 text-text-tertiary" aria-live="polite">
            {check.isPending ? "Checking…" : result ? updateStatusLine(result) : "Not checked yet this session."}
          </span>
        </div>
        {result?.available && result.url ? (
          <Button type="button" size="default" onClick={() => void Browser.OpenURL(result.url)}>
            View release
          </Button>
        ) : (
          <Button type="button" variant="outline" size="default" disabled={check.isPending} onClick={() => check.mutate()}>
            Check now
          </Button>
        )}
      </div>

      <label className="flex items-center gap-2.5 rounded-control border border-border-subtle bg-surface-raised py-2.5 pr-3 pl-3.5">
        <span className="min-w-0 flex-1 text-body-13 text-text-primary">Check for updates when DevKit starts</span>
        <Switch
          checked={automatic ?? true}
          disabled={automatic === undefined || save.isPending}
          onCheckedChange={(checked) => save.mutate(checked)}
        />
      </label>

      <p className="text-body-12 text-text-tertiary">
        One request to GitHub for public release information; nothing about you or this machine is sent. DevKit
        never downloads or installs an update itself. Beta builds are told about newer betas, stable builds only
        about stable releases.
      </p>

      {message ? <p className="text-body-12 text-status-danger">{message}</p> : null}
    </div>
  );
}
