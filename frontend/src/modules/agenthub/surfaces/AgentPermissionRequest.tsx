import { usePermissionDeadline } from "./usePermissionDeadline";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import type { AgentSession, PendingPermission } from "@/bridge/agenthub";
import { parsePermissionOptions } from "@/bridge/agenthub";
import {
  PermissionAsk,
  PermissionIdentity,
  PermissionLockedNote,
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
 * DEVIATION: Figma draws no countdown. The gate fails closed after 60s
 * (interaction.DefaultTimeout) and the backend stamps expiresAt, so hiding the
 * clock would hide a denial that is about to happen on its own.
 */
export function AgentPermissionRequest({
  pending,
  session,
  onResolve,
  busy = false,
  env: explicitEnv,
  size = "rail",
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
          Allow always
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
        "flex w-full flex-col rounded-control border border-border-subtle bg-surface-raised",
        // The pulsing dot and the clock carry the urgency; the card itself stays
        // the app's ordinary raised surface. A tinted panel beside a tinted
        // composer was one colour too many, and it was the wrong one — warning
        // over a transcript reads as an error that already happened.
        wide ? "gap-2 px-3 py-2.5" : "gap-3 p-3",
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
