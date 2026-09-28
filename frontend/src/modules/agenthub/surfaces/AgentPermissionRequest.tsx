import { ChevronDown } from "lucide-react";

import { usePermissionDeadline } from "./usePermissionDeadline";
import { ActionTooltip } from "@/components/ui/action-tooltip";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import type { AgentSession, PendingPermission } from "@/bridge/agenthub";
import { parsePermissionOptions } from "@/bridge/agenthub";
import {
  PermissionAsk,
  PermissionIdentity,
  PermissionLockedNote,
  allowAlwaysLabel,
  permissionChoices,
  permissionEnv,
  type PermissionEnv,
} from "./permissionCard";
import { PermissionFileEdits } from "./PermissionFileEdits";

export type { PermissionEnv };

export type AgentPermissionRequestProps = {
  pending: PendingPermission;
  session?: AgentSession | null;
  onResolve: (optionId: string | null) => void | Promise<void>;
  busy?: boolean;
  env?: PermissionEnv;
  /**
   * "rail" stacks; "wide" puts the ask and the answer on one line. The module's
   * composer column is 720 across, and a stacked card there is three short rows
   * of text in a band of empty space — the request reads as a panel rather than
   * as a question waiting on you.
   */
  size?: "rail" | "wide";
  /**
   * Folds the request down into the composer tray's strip, to read the
   * transcript that explains it. The agent keeps waiting: a request carries a
   * deadline only when one is configured (Service.PermissionTimeout), and then
   * the gate — not this card — closes it.
   */
  onMinimize?: () => void;
  className?: string;
};

/**
 * Agent / Permission Request — Figma `909:49`, variants `909:28` Open and
 * `909:48` Locked.
 *
 * Figma's note carries the design: "Asked inside the rail, never as a modal:
 * the request blocks the agent, not the person. Names which agent is asking —
 * with N running, an unattributed prompt is undecidable. Env=Locked drops
 * allow-always because env always wins."
 *
 * So the card reads top-down as an answer to "who is asking, from where, for
 * what": identity and deadline, then the tool and its command. Leading with the
 * tool name would drop the attribution the note calls the decidable part. Both
 * blocks come from permissionCard, shared with the toast.
 *
 * It is asked in two places, and they are the same place by a different route:
 * the rail, and the composer column of the Agent Hub module. Both put it in the
 * conversation the request came out of, which is where the evidence for the
 * answer is.
 *
 * Locked differs in three ways and only three: the env pill turns danger-toned,
 * a line appears saying the environment always asks, and Allow always is gone.
 *
 * DEVIATION: Figma draws no countdown. When a deadline is configured the
 * backend stamps expiresAt and the gate fails closed at it, so hiding the clock
 * would hide a denial that is about to happen on its own. By default there is
 * none, and no clock is drawn (usePermissionDeadline).
 *
 * It lives in the composer tray now, on the tray's surface: no card border of
 * its own, the one-line identity (the strip already names the folder), and a
 * button that folds it into the strip.
 */
export function AgentPermissionRequest({
  pending,
  session,
  onResolve,
  busy = false,
  env: explicitEnv,
  size = "rail",
  onMinimize,
  className,
}: AgentPermissionRequestProps) {
  const { secondsLeft, expired } = usePermissionDeadline(pending);
  const env = permissionEnv(pending.params, explicitEnv);
  const { once, always } = permissionChoices(parsePermissionOptions(pending.params));

  // Allow always is dropped whenever the environment is locked, whatever the
  // vendor offered — Figma: "env always wins".
  const showAllowAlways = env === "open" && Boolean(always);
  const wide = size === "wide";

  // Three buttons have to fit 248px of inner width in the rail, so the variant
  // that shows all three squeezes its padding. Both are 32 tall.
  const pad = !wide && showAllowAlways ? "px-2" : undefined;
  const actions = (
    <div className={cn("flex items-center gap-2", wide ? "shrink-0" : "flex-wrap")}>
      <Button
        type="button"
        variant="outline"
        size="sm"
        disabled={busy || expired}
        data-testid="permission-deny"
        onClick={() => void onResolve(null)}
        className={cn("border-border-subtle text-text-secondary", pad)}
      >
        Deny
      </Button>
      <Button
        type="button"
        size="sm"
        disabled={busy || expired}
        data-testid="permission-allow-once"
        onClick={() => void onResolve(once ? once.optionId : "allow_once")}
        className={pad}
      >
        Allow once
      </Button>
      {showAllowAlways && always ? (
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={busy || expired}
          data-testid="permission-allow-always"
          onClick={() => void onResolve(always.optionId)}
          className={cn("border-border-subtle", pad)}
        >
          {allowAlwaysLabel(always)}
        </Button>
      ) : null}
    </div>
  );

  return (
    <div
      role="alert"
      data-testid="agent-permission-request"
      data-env={env}
      data-size={size}
      className={cn(
        // The pulsing dot and the clock carry the urgency; the card itself is
        // the tray's plain surface. A tinted panel was one colour too many,
        // and the wrong one — warning over a transcript reads as an error that
        // already happened.
        "flex w-full min-w-0 flex-col gap-2 p-3",
        className,
      )}
    >
      <PermissionIdentity
        pending={pending}
        session={session}
        env={env}
        secondsLeft={secondsLeft}
        expired={expired}
        testIdPrefix="permission"
        dense
        trailing={
          onMinimize ? (
            <ActionTooltip label="Minimize — read the conversation">
              <Button
                type="button"
                variant="ghost"
                size="icon-xs"
                aria-label="Minimize permission request"
                data-testid="permission-minimize"
                onClick={onMinimize}
                className="shrink-0 text-text-tertiary hover:text-text-primary"
              >
                <ChevronDown />
              </Button>
            </ActionTooltip>
          ) : undefined
        }
      />

      {wide ? (
        <div className="flex w-full items-stretch gap-2">
          <PermissionAsk pending={pending} className="min-h-8 flex-1" />
          {actions}
        </div>
      ) : (
        <>
          <PermissionAsk pending={pending} className="w-full" />
          {actions}
        </>
      )}

      {/* What the edit is, not only which tool: the file as it would be. */}
      <PermissionFileEdits pending={pending} projectPath={session?.projectPath} actions={actions} />

      <PermissionLockedNote env={env} />
    </div>
  );
}
