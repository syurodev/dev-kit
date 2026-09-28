import NumberFlow from "@number-flow/react";
import type * as React from "react";
import { AnimatePresence, motion, useReducedMotion } from "framer-motion";

import { EASE_OUT_STRONG, MOTION_BASE, MOTION_SLOW, noMotion } from "@/lib/motion";
import { cn } from "@/lib/utils";
import type { AgentSession, PendingPermission, PermissionOption } from "@/bridge/agenthub";
import { permissionCommand, permissionDetail, permissionToolTitle } from "@/bridge/agenthub";
import { AgentState } from "./AgentState";

export type PermissionEnv = "open" | "locked";

/** The last few seconds are when a silent denial is about to happen. */
export const URGENT_SECONDS = 10;

/**
 * The two permission surfaces — the card asked inside a conversation and the
 * toast floating over another module — ask the same three questions in the same
 * order: who is asking, from where, for what. These are those parts, so the two
 * read as one family and cannot drift into two vocabularies for one decision.
 *
 * What stays with each surface is what differs for a real reason: the inline
 * card offers Allow always, the toast offers a deadline bar, a queue pill and
 * Open rail.
 */

/**
 * The way a permission card arrives and leaves, shared by all three surfaces so
 * one request does not fade in the rail and snap in the module.
 *
 * Keyed by the request's id, which is what makes a queue readable: answer one
 * while another is waiting and the second does not appear in the first's place
 * mid-blink — the old card leaves and the new one arrives, and you can see that
 * there was a second question.
 *
 * `inline` animates height as well, because that card is in the flow above a
 * composer: opening without it would shove the transcript, and closing without
 * it would drop the composer a row in one frame. `toast` is in a fixed corner
 * with nothing under it to push, so it travels instead — up from the edge it
 * lives on. Opens over --motion-slow, closes over --motion-base: a question
 * arriving deserves to be noticed, an answered one should get out of the way.
 *
 * Under Reduce Motion both snap, the same as AnimatedCollapse.
 */
export function PermissionPresence({
  pendingId,
  variant = "inline",
  className,
  children,
  onExitComplete,
}: {
  /** The pending request's id, or null/undefined when there is nothing to ask. */
  pendingId?: string | null;
  variant?: "inline" | "toast";
  className?: string;
  children?: React.ReactNode;
  onExitComplete?: () => void;
}) {
  const reduce = useReducedMotion() ?? false;
  const inline = variant === "inline";
  const open = inline
    ? { height: "auto" as const, opacity: 1 }
    : { opacity: 1, y: 0, scale: 1 };
  const shut = inline ? { height: 0, opacity: 0 } : { opacity: 0, y: 12, scale: 0.98 };

  // `initial` left at its default, unlike AnimatedCollapse: a card whose
  // surface mounts at the same moment it appears — the toast host, which does
  // not exist until there is something to ask — would otherwise skip its
  // entrance, since AnimatePresence treats a child present on its own first
  // render as already there.
  return (
    <AnimatePresence onExitComplete={onExitComplete}>
      {pendingId ? (
        <motion.div
          key={pendingId}
          data-slot="permission-presence"
          initial={shut}
          animate={{
            ...open,
            transition: reduce ? noMotion : { duration: MOTION_SLOW, ease: EASE_OUT_STRONG },
          }}
          exit={{
            ...shut,
            transition: reduce ? noMotion : { duration: MOTION_BASE, ease: EASE_OUT_STRONG },
          }}
          // Height from 0 has to clip what is inside it on the way.
          style={inline ? { overflow: "hidden" } : undefined}
          className={className}
        >
          {children}
        </motion.div>
      ) : null}
    </AnimatePresence>
  );
}

/** Locked = an environment that asks every time. Read off the request's params. */
export function permissionEnv(params: unknown, explicit?: PermissionEnv): PermissionEnv {
  if (explicit) return explicit;
  if (!params || typeof params !== "object") return "open";
  const record = params as Record<string, unknown>;
  return record.locked || record.env === "prod" || record.environment === "prod"
    ? "locked"
    : "open";
}

export function PermissionIdentity({
  pending,
  session,
  env,
  secondsLeft,
  expired,
  testIdPrefix,
  trailing,
  dense = false,
}: {
  /**
   * Who is asking. Structural rather than PendingPermission: a question from the
   * agent is attributed the same way — the same agent, depth and folder — and
   * has none of a permission's other fields.
   */
  pending: Pick<PendingPermission, "sessionId" | "title" | "depth">;
  session?: AgentSession | null;
  env: PermissionEnv;
  /**
   * The deadline. Omitted, the clock is not drawn: a question has no deadline,
   * and a clock counting nothing would promise a denial that never comes.
   */
  secondsLeft?: number;
  expired?: boolean;
  /** "toast" or "permission" — the test ids the two surfaces already publish. */
  testIdPrefix: string;
  /** Extra marks between the identity and the clock — the toast's queue pill. */
  trailing?: React.ReactNode;
  /**
   * One line: the agent and, for a subagent, its depth. For the composer
   * tray, whose strip already says which folder this is — the second line
   * repeated it at the cost of a row of a panel that has little height.
   */
  dense?: boolean;
}) {
  const agentTitle = session?.title || pending.title || pending.sessionId.slice(0, 8);
  const cwd = session?.projectPath || ".";
  // Depth comes off the request, not the session: it is the caller's depth, and
  // a subagent's request carries a deeper one than the session it belongs to.
  const depth = pending.depth ?? session?.depth ?? 0;
  const timed = secondsLeft !== undefined;
  const urgent = timed && !expired && secondsLeft <= URGENT_SECONDS;

  const marks = (
    <>
      {env === "locked" ? (
        <span
          data-testid={`${testIdPrefix}-env-locked`}
          className="shrink-0 rounded-full bg-(--feedback-danger-soft) px-2 py-0.5 text-caption-11-medium text-status-danger"
        >
          prod
        </span>
      ) : null}
      {timed ? (
        <span
          data-testid={`${testIdPrefix}-seconds`}
          className={cn(
            "shrink-0 text-caption-11 whitespace-nowrap",
            urgent ? "text-status-warning" : "text-text-tertiary",
          )}
        >
          {/* The digits roll rather than being redrawn: on a card you are not
              looking at, movement is what says the deadline is still running.
              NumberFlow owns the aria-label too, so a reader hears "44s" rather
              than two digits. */}
          {expired ? "expired" : <NumberFlow value={secondsLeft} suffix="s" willChange />}
        </span>
      ) : null}
    </>
  );

  if (dense) {
    return (
      <div className="flex w-full min-w-0 items-center gap-2">
        <AgentState status="awaiting" labelHidden pulse className="h-5" />
        {/* The folder is the strip's to show; it stays on hover here. */}
        <p title={cwd} className="min-w-0 flex-1 truncate text-body-13-medium text-text-primary">
          {agentTitle}
          {depth > 0 ? <span className="text-caption-11 font-normal text-text-tertiary">{` · subagent, depth ${depth}`}</span> : null}
        </p>
        {marks}
        {/* Last in the row: a control after the facts about the request. */}
        {trailing}
      </div>
    );
  }

  return (
    <div className="flex w-full items-start gap-3">
      <AgentState status="awaiting" labelHidden pulse className="h-5" />
      <div className="flex min-w-0 flex-1 flex-col">
        <p className="truncate text-body-13-medium text-text-primary">{agentTitle}</p>
        {/* The path gives way from its start: the project is at the end. */}
        <p className="flex min-w-0 gap-1 text-caption-11 text-text-tertiary">
          <span className="shrink-0 whitespace-nowrap">{`depth ${depth} ·`}</span>
          <span dir="rtl" className="min-w-0 truncate text-left" title={cwd}>
            <bdi>{cwd}</bdi>
          </span>
        </p>
      </div>
      {trailing}
      {marks}
    </div>
  );
}

/** The line that says an environment asks every time. Locked only. */
export function PermissionLockedNote({ env }: { env: PermissionEnv }) {
  if (env !== "locked") return null;
  return (
    <p className="w-full text-caption-11 text-text-tertiary">
      prod requires confirmation every time
    </p>
  );
}

export function PermissionAsk({
  pending,
  className,
}: {
  pending: PendingPermission;
  className?: string;
}) {
  const toolTitle = permissionToolTitle(pending.params);
  const command = permissionCommand(pending.params);
  const detail = command ? {} : permissionDetail(pending.params);
  // claude-agent-acp titles a Bash call with the command in backticks; showing
  // it twice, once as a title and once in the well, says nothing new.
  const titleRepeatsCommand =
    command !== undefined && toolTitle.replace(/^`|`$/g, "").trim() === command.trim();

  return (
    <div
      className={cn(
        "flex min-w-0 flex-col justify-center gap-0.5 rounded-md border border-border-subtle bg-surface-base px-3 py-1.5",
        className,
      )}
    >
      {titleRepeatsCommand ? null : (
        <p className="truncate text-label-12 text-text-secondary">{toolTitle}</p>
      )}
      {command ? (
        <p className="line-clamp-2 break-all text-code-12 text-text-primary" title={command}>
          {command}
        </p>
      ) : null}
      {detail.message ? (
        <p data-testid="permission-message" className="line-clamp-2 text-body-12 text-text-primary">
          {detail.message}
        </p>
      ) : null}
      {detail.args ? (
        <p data-testid="permission-args" className="line-clamp-2 break-all text-code-11 text-text-secondary" title={detail.args}>
          {detail.args}
        </p>
      ) : null}
    </div>
  );
}

/**
 * What the always-grant button says. Codex's lasts for the session and names
 * itself so ("Allow for this session"); calling that "Allow always" would
 * promise a rule nobody wrote. Anything else keeps the old words.
 */
export function allowAlwaysLabel(option: PermissionOption | undefined): string {
  return option && /session/i.test(option.name) ? "Allow for session" : "Allow always";
}

/** The option the vendor means by "just this once", and the always-grant. */
export function permissionChoices(options: PermissionOption[]) {
  const once =
    options.find(
      (o) =>
        o.optionId.includes("once") ||
        o.name.toLowerCase().includes("once") ||
        o.kind.includes("once"),
    ) ?? options[0];
  const always = options.find(
    (o) =>
      o.optionId.includes("always") ||
      o.name.toLowerCase().includes("always") ||
      o.kind.includes("always"),
  );
  return { once, always };
}
