import { ChevronDown, ShieldAlert } from "lucide-react";

import { usePermissionDeadline } from "./usePermissionDeadline";
import { ActionTooltip } from "@/components/ui/action-tooltip";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import type { AgentSession, PendingPermission } from "@/bridge/agenthub";
import { parsePermissionOptions, permissionCommand, permissionDetail, permissionToolTitle } from "@/bridge/agenthub";
import {
  PermissionLockedNote,
  PermissionMarks,
  allowAlwaysLabel,
  permissionChoices,
  permissionEnv,
  type PermissionEnv,
} from "./permissionCard";
import { PermissionFileEdits } from "./PermissionFileEdits";
import { PermissionPreview } from "./PermissionPreview";
import { GatePermissionDetails, gatePermission, isUntaggedGate } from "./GatePermissionDetails";
import { TrayPanelHeading } from "./composer/TrayPanelHeading";

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
  /**
   * Drawn as a composer-tray panel: the same heading as Repositories or Tasks
   * (title, how many are waiting, the env and deadline marks, a close button
   * that folds it) over the body, rather than a header row of its own.
   */
  panel?: boolean;
  /** Requests waiting in this session, this one included. Shown in the panel heading when more than one. */
  waiting?: number;
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
 * In the composer tray it reads like the question card beside it: one row
 * saying what the agent wants to run, the command itself in a well the full
 * width of the tray (a box squeezed beside three buttons cut a one-line
 * `find` to two lines and an ellipsis), then the answers on their own row,
 * Allow once last and filled (own row under a divider, on the body's padding,
 * stacking in a narrow rail). The command or arguments fold to three lines with
 * a "Show full request" row that opens all of it, and Copy (PermissionPreview).
 * Who is asking is on the shield's tooltip — in a
 * session's own tray that is the session — and named in the row only for a
 * subagent, where it is the thing that decides the answer.
 */

/** What the agent wants to do, as the card's heading. */
function permissionHeading(params: unknown, command: string | undefined): string {
  const title = permissionToolTitle(params);
  // claude-agent-acp titles a Bash call with the command in backticks; the
  // command is in the well below, so the heading says what kind of thing it is.
  if (command !== undefined && title.replace(/^`|`$/g, "").trim() === command.trim()) return "Run a command";
  return title;
}
export function AgentPermissionRequest({
  pending,
  session,
  onResolve,
  busy = false,
  env: explicitEnv,
  size = "rail",
  onMinimize,
  panel = false,
  waiting = 1,
  className,
}: AgentPermissionRequestProps) {
  const gate = gatePermission(pending.params);
  const { secondsLeft, expired } = usePermissionDeadline(pending);
  const env = permissionEnv(pending.params, explicitEnv);
  const { once, always } = permissionChoices(parsePermissionOptions(pending.params));
  const command = permissionCommand(pending.params);
  const detail = command ? {} : permissionDetail(pending.params);
  const previewText = command ?? detail.argsFull;
  const heading = permissionHeading(pending.params, command);
  const agentTitle = session?.title || pending.title || pending.sessionId.slice(0, 8);
  // The caller's depth, off the request: a subagent's is deeper than its session's.
  const depth = pending.depth ?? session?.depth ?? 0;
  const cwd = session?.projectPath || ".";
  const askedBy = `Asked by ${agentTitle}${depth > 0 ? ` · subagent, depth ${depth}` : ""} · ${cwd}`;

  // Allow always is dropped whenever the environment is locked, whatever the
  // vendor offered — Figma: "env always wins".
  const showAllowAlways = env === "open" && Boolean(always);

  // One row, one height (the shared `sm` control), one gap. Deny and the grant
  // are outlined and Allow once is the only filled control, so the primary
  // reads first without Deny looking like a missing button. Wide: right-aligned
  // as a group. Narrow (the rail): the buttons stack full width rather than
  // overflow or wrap unevenly (`@container` on the card).
  const actions = (
    <div className="flex flex-col gap-2 @min-[20rem]/permission:flex-row @min-[20rem]/permission:flex-wrap @min-[20rem]/permission:items-center @min-[20rem]/permission:justify-end">
      <Button
        type="button"
        variant="outline"
        size="sm"
        disabled={busy || expired}
        data-testid="permission-deny"
        onClick={() => void onResolve(null)}
        className="border-border-subtle text-text-primary"
      >
        Deny
      </Button>
      {showAllowAlways && always ? (
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={busy || expired}
          data-testid="permission-allow-always"
          onClick={() => void onResolve(always.optionId)}
          className="border-border-subtle"
        >
          {allowAlwaysLabel(always)}
        </Button>
      ) : null}
      <Button
        type="button"
        size="sm"
        disabled={busy || expired}
        data-testid="permission-allow-once"
        onClick={() => void onResolve(once ? once.optionId : "allow_once")}
      >
        Allow once
      </Button>
    </div>
  );

  const marks = (
    <PermissionMarks env={env} environment={gate?.env} untagged={isUntaggedGate(gate)} secondsLeft={secondsLeft} expired={expired} testIdPrefix="permission" />
  );

  return (
    <div
      role="alert"
      data-testid="agent-permission-request"
      data-env={env}
      data-size={size}
      className={cn("@container/permission flex w-full min-w-0 flex-col", className)}
    >
      {panel ? (
        <TrayPanelHeading
          title="Permission request"
          count={waiting > 1 ? waiting : undefined}
          detail="The agent is waiting for your answer"
          trailing={marks}
          onClose={onMinimize}
          closeTestId="permission-minimize"
        />
      ) : null}
      <div className="flex w-full min-w-0 flex-col gap-2 px-3 pt-3 pb-3">
        <div className="flex min-w-0 items-center gap-2">
          <span title={askedBy} className="flex shrink-0 items-center text-status-warning">
            <ShieldAlert className="size-4" aria-label={askedBy} />
          </span>
          <p className="min-w-0 flex-1 truncate text-body-13-medium text-text-primary" title={heading}>
            {heading}
            {depth > 0 ? (
              <span className="text-caption-11 font-normal text-text-tertiary">{` · ${agentTitle}, depth ${depth}`}</span>
            ) : null}
          </p>
          {panel ? null : marks}
          {!panel && onMinimize ? (
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
          ) : null}
        </div>

        {gate ? <GatePermissionDetails params={pending.params} showTool={false} /> : null}
        {/* Never cut with an ellipsis: what is allowed is exactly what is shown.
            Folded it is three lines; "Show full request" opens all of it. */}
        {!gate && previewText ? (
          <PermissionPreview
            key={pending.id}
            text={previewText}
            message={detail.message}
            testId={command ? "permission-command" : "permission-args"}
          />
        ) : null}
        {!gate && !previewText && detail.message ? (
          <p data-testid="permission-message" className="line-clamp-3 text-body-13 text-text-secondary">
            {detail.message}
          </p>
        ) : null}

        {/* What the edit is, not only which tool: the file as it would be. */}
        <PermissionFileEdits pending={pending} projectPath={session?.projectPath} actions={actions} />

        <PermissionLockedNote env={env} environment={gate?.env} untagged={isUntaggedGate(gate)} />
      </div>
      {/* Its own row, on the body's padding: sticky to the foot of whatever
          scrolls this card (the composer's panel area on a short pane), so a
          long request or a stack of file edits scrolls past and Deny / Allow
          stay where they can be pressed. */}
      <div data-testid="permission-actions" className="sticky bottom-0 border-t border-border-subtle bg-surface-base px-3 py-2.5">
        {actions}
      </div>
    </div>
  );
}
