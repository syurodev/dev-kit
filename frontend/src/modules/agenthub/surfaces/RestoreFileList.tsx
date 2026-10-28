import { FileMinus, FilePlus } from "lucide-react";

import { Checkbox } from "@/components/ui/checkbox";
import { cn } from "@/lib/utils";
import { conflictReason } from "../restore";
import type { RestoreAction, RestoreConflict } from "../../../../bindings/devkit/internal/agenthub/models";

/**
 * What a restore would change, and what it will leave alone unless told otherwise.
 *
 * The one list both file confirmations draw: the rewind dialog's file half and the undo of an
 * interrupted rewind. Only overridable conflicts can be ticked; the rest are shown disabled,
 * with the reason. The test ids are the rewind dialog's own and stay as they were.
 */
export function RestoreFileList({
  actions,
  conflicts,
  forced,
  onToggle,
}: {
  actions: readonly RestoreAction[];
  conflicts: readonly RestoreConflict[];
  forced: ReadonlySet<string>;
  onToggle: (path: string) => void;
}) {
  if (actions.length === 0 && conflicts.length === 0) return null;
  return (
    <div className="max-h-56 overflow-y-auto">
      <ul className="flex flex-col gap-1">
        {actions.map((action) => (
          <li key={action.path} data-testid="rewind-action" className="flex items-center gap-2">
            {action.kind === "delete" ? (
              <FileMinus aria-hidden="true" className="size-(--icon-12) shrink-0 text-status-warning" />
            ) : (
              <FilePlus aria-hidden="true" className="size-(--icon-12) shrink-0 text-status-success" />
            )}
            <span className="min-w-0 truncate text-code-12 text-text-secondary">{action.path}</span>
          </li>
        ))}
      </ul>
      {conflicts.length > 0 ? (
        <div className="mt-3 flex flex-col gap-2 border-t border-border-subtle pt-3">
          <p className="text-caption-11 text-text-tertiary">These are left alone unless you say otherwise:</p>
          {conflicts.map((conflict) => (
            <label
              key={conflict.path}
              data-testid="rewind-conflict"
              className={cn("flex items-start gap-2", conflict.overridable ? "cursor-pointer" : "cursor-default")}
            >
              <Checkbox
                className="mt-0.5"
                disabled={!conflict.overridable}
                checked={forced.has(conflict.path)}
                onCheckedChange={() => onToggle(conflict.path)}
                aria-label={`Overwrite ${conflict.path}`}
              />
              <span className="flex min-w-0 flex-col">
                <span className="min-w-0 truncate text-code-12 text-text-secondary">{conflict.path}</span>
                <span className="text-caption-11 text-text-tertiary">{conflictReason(conflict)}</span>
              </span>
            </label>
          ))}
        </div>
      ) : null}
    </div>
  );
}
