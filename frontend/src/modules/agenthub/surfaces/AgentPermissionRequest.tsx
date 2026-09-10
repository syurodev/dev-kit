import { usePermissionDeadline } from "./usePermissionDeadline";
import { AgentState } from "./AgentState";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import type { AgentSession, PendingPermission } from "@/bridge/agenthub";
import { parsePermissionOptions, permissionToolTitle } from "@/bridge/agenthub";

export type PermissionEnv = "open" | "locked";

export type AgentPermissionRequestProps = {
  pending: PendingPermission;
  session?: AgentSession | null;
  onResolve: (optionId: string | null) => void | Promise<void>;
  busy?: boolean;
  timeoutSeconds?: number;
  env?: PermissionEnv;
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
 * what": state and agent name, then the working directory, then the tool and
 * its environment, then the literal command. Leading with the tool name would
 * drop the attribution the note calls the decidable part.
 *
 * Locked differs in three ways and only three: the env pill turns danger-toned,
 * a line appears saying the environment always asks, and Allow always is gone.
 *
 * DEVIATION: Figma draws no countdown. The gate fails closed after 60s
 * (interaction.DefaultTimeout) and the backend stamps expiresAt, so hiding the
 * clock would hide a denial that is about to happen on its own. It sits beside
 * depth, which is the row Figma uses for session metadata.
 */
export function AgentPermissionRequest({
  pending,
  session,
  onResolve,
  busy = false,
  timeoutSeconds = 60,
  env: explicitEnv,
  className,
}: AgentPermissionRequestProps) {
  const { secondsLeft, expired } = usePermissionDeadline(pending, timeoutSeconds);

  const isLocked =
    explicitEnv === "locked" ||
    (typeof pending.params === "object" &&
      pending.params !== null &&
      Boolean(
        (pending.params as Record<string, unknown>).locked ||
          (pending.params as Record<string, unknown>).env === "prod" ||
          (pending.params as Record<string, unknown>).environment === "prod",
      ));
  const env: PermissionEnv = isLocked ? "locked" : "open";

  const toolTitle = permissionToolTitle(pending.params);
  const options = parsePermissionOptions(pending.params);
  const agentTitle =
    session?.title || pending.title || pending.sessionId.slice(0, 8);
  const cwd = session?.projectPath || ".";
  // Depth comes off the request, not the session: it is the caller's depth,
  // and a subagent's request carries a deeper one than the session it belongs
  // to. Figma 909:28 shows "depth 2" on a request from a depth-2 caller.
  const depth = pending.depth ?? session?.depth ?? 0;

  const command =
    typeof pending.params === "object" && pending.params !== null
      ? ((pending.params as Record<string, unknown>).command as string | undefined)
      : undefined;

  const allowOnceOption =
    options.find(
      (o) =>
        o.optionId.includes("once") ||
        o.name.toLowerCase().includes("once") ||
        o.kind.includes("once"),
    ) ?? options[0];

  const allowAlwaysOption = options.find(
    (o) =>
      o.optionId.includes("always") ||
      o.name.toLowerCase().includes("always") ||
      o.kind.includes("always"),
  );

  // Allow always is dropped whenever the environment is locked, whatever the
  // vendor offered — Figma: "env always wins".
  const showAllowAlways = env === "open" && Boolean(allowAlwaysOption);

  // Three buttons have to fit 248px of inner width, so the Open variant squeezes
  // its padding; Locked has room for the usual px-3. Both are 32 tall.
  const actionPad = showAllowAlways ? "px-2" : "px-3";

  return (
    <div
      role="alert"
      data-testid="agent-permission-request"
      data-env={env}
      className={cn(
        "flex w-full max-w-68 flex-col gap-3 rounded-control p-3",
        "border border-border-subtle bg-surface-raised",
        className,
      )}
    >
      {/* who */}
      <div className="flex w-full items-center gap-2">
        <AgentState status="awaiting" />
        <p className="min-w-0 flex-1 truncate text-body-13-medium text-text-primary">
          {agentTitle}
        </p>
        <p className="shrink-0 text-caption-11 text-text-tertiary whitespace-nowrap">
          {/* One string, not text + expression: renderToString splits those
              with comment markers, so "depth 1" would not be contiguous. */}
          {`depth ${depth} · ${expired ? "expired" : `${secondsLeft}s`}`}
        </p>
      </div>

      <p className="w-full text-caption-11 text-text-tertiary">{cwd}</p>

      {/* ask */}
      <div className="flex w-full items-center gap-2">
        <p className="min-w-0 flex-1 truncate text-label-12 text-text-secondary">
          {toolTitle}
        </p>
        <span
          data-testid={env === "locked" ? "permission-env-locked" : "permission-env-open"}
          className={cn(
            "shrink-0 rounded-full px-2 py-1 text-caption-11-medium",
            env === "locked"
              ? "bg-(--feedback-danger-soft) text-status-danger"
              : "bg-(--state-soft) text-text-secondary",
          )}
        >
          {env === "locked" ? "prod" : "dev"}
        </span>
      </div>

      {command ? (
        <p className="w-full text-body-12 text-text-primary break-all">
          {command}
        </p>
      ) : null}

      {env === "locked" ? (
        <p className="w-full text-caption-11 text-text-tertiary">
          prod requires confirmation every time
        </p>
      ) : null}

      <div className="flex items-start gap-2">
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={busy || expired}
          data-testid="permission-deny"
          onClick={() => void onResolve(null)}
          className={cn("h-8 border-border-subtle text-text-secondary", actionPad)}
        >
          Deny
        </Button>
        <Button
          type="button"
          size="sm"
          disabled={busy || expired}
          data-testid="permission-allow-once"
          onClick={() =>
            void onResolve(allowOnceOption ? allowOnceOption.optionId : "allow_once")
          }
          className={cn("h-8", actionPad)}
        >
          Allow once
        </Button>
        {showAllowAlways && allowAlwaysOption ? (
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={busy || expired}
            data-testid="permission-allow-always"
            onClick={() => void onResolve(allowAlwaysOption.optionId)}
            className={cn("h-8 border-border-subtle", actionPad)}
          >
            Allow always
          </Button>
        ) : null}
      </div>
    </div>
  );
}
