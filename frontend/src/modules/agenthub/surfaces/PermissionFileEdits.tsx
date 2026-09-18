import * as React from "react";
import {
  agentHubPermissionFileDiff,
  permissionFileEdits,
  type PendingPermission,
  type PermissionFileDiff,
} from "@/bridge/agenthub";
import { FileDiffPane } from "./FileDiffPane";
import { cn } from "@/lib/utils";

/**
 * The files a permission request would change, shown as 08 Diff panes in the
 * card that asks for it — what is being allowed, not only which tool.
 *
 * Drawn at once from what the request carries (the agent's snippet), then
 * replaced by the whole-file preview the backend reads from disk, which places
 * the edit among its surrounding lines and tells a new file from an
 * overwrite. Nothing for a request that edits no file.
 */
export function PermissionFileEdits({
  pending,
  projectPath,
  actions,
  className,
}: {
  pending: PendingPermission;
  projectPath?: string;
  /** The card's Deny / Allow buttons, so the full-diff dialog can answer too. */
  actions?: React.ReactNode;
  className?: string;
}) {
  const fromRequest = React.useMemo(() => permissionFileEdits(pending.params), [pending.params]);
  const [fromDisk, setFromDisk] = React.useState<PermissionFileDiff[] | null>(null);

  React.useEffect(() => {
    if (fromRequest.length === 0) return;
    let live = true;
    setFromDisk(null);
    agentHubPermissionFileDiff(pending.id)
      .then((diffs) => {
        if (live && diffs.length > 0) setFromDisk(diffs);
      })
      .catch(() => {
        // The request's own snippet stays on screen; it is still accurate.
      });
    return () => {
      live = false;
    };
  }, [pending.id, fromRequest.length]);

  const diffs = fromDisk ?? fromRequest;
  if (diffs.length === 0) return null;
  return (
    <div data-testid="permission-file-edits" className={cn("flex w-full min-w-0 flex-col gap-2", className)}>
      {diffs.map((diff, i) => (
        <FileDiffPane
          key={`${diff.path}:${i}`}
          diff={diff}
          projectPath={projectPath}
          actions={actions}
          labels={{ before: diff.beforeMissing ? "Not there yet" : "On disk", after: "Proposed" }}
          testIdPrefix="permission-diff"
        />
      ))}
    </div>
  );
}
