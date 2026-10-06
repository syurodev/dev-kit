import * as React from "react";
import { AnimatePresence, animate, motion, useMotionValue, useReducedMotion, type Transition, type Variants } from "framer-motion";
import { ChevronsLeft, ChevronsRight } from "lucide-react";

import { cn } from "@/lib/utils";
import { ActionTooltip } from "@/components/ui/action-tooltip";
import { Button } from "@/components/ui/button";
import { CONTENT_FADE_DELAY, EASE_OUT_STRONG, MOTION_BASE, MOTION_FAST, MOTION_SLOW, noMotion } from "@/lib/motion";

/**
 * What the tray shows above its strip: an "@" or "/" list while one is being
 * typed, a permission or a question the agent is waiting on, or a list the
 * person opened from a chip. `key` names it, so swapping one for another fades
 * through instead of morphing one into the other.
 */
export type TrayPanel = { key: string; content: React.ReactNode };

/*
 * Motion, by docs/dev-kit/smoothness.mdx: the tokens (§4.1) — a panel opens
 * over --motion-slow and closes quicker, over --motion-base; springs with no
 * bounce, because this is data; interruptible, so a spring rather than a
 * tween (§4.5): a panel swapped mid-growth retargets from where it is, with
 * the speed it has. The tray's height is the one layout property animated —
 * it is a panel container, the exception §4.2 allows; what is inside moves
 * only by transform and opacity, and trails the container by
 * CONTENT_FADE_DELAY so the panel leads and the body follows (§4.4).
 */
const GROW: Transition = { type: "spring", duration: MOTION_SLOW, bounce: 0 };
const SHRINK: Transition = { type: "spring", duration: MOTION_BASE, bounce: 0 };

const CONTENT_VARIANTS: Variants = {
  enter: { opacity: 0, y: -4 },
  center: {
    opacity: 1,
    y: 0,
    transition: { duration: MOTION_BASE, ease: EASE_OUT_STRONG, delay: CONTENT_FADE_DELAY },
  },
  exit: { opacity: 0, transition: { duration: MOTION_FAST, ease: EASE_OUT_STRONG } },
};
const REDUCED_CONTENT_VARIANTS: Variants = {
  enter: { opacity: 0 },
  center: { opacity: 1, transition: noMotion },
  exit: { opacity: 0, transition: noMotion },
};

/** Chips read it: in icon-only mode a label becomes the tooltip. */
const TrayCompactContext = React.createContext(false);

/** Whether the tray around this is icon-only — for a control in the strip that is not a TrayChip. */
export function useTrayCompact(): boolean {
  return React.useContext(TrayCompactContext);
}

/**
 * The composer's tray — Figma has none; the shape follows Codex's "Cloud ·
 * repo · branch" bar. One surface grown out of the top of the composer that
 * is always there, at two heights:
 *
 * - The strip, one row: where this work is (workspace, branch), what it is
 *   for (tasks), and what is running under it (subagents). Always shown —
 *   even a scratch session says which folder it is in — so the composer does
 *   not change shape when a status appears.
 * - The panel above it, when there is something to answer or pick. It grows
 *   and shrinks by height and fades between contents, rather than popping in
 *   over the transcript the way the old floating lists and the stacked
 *   permission / question cards did.
 *
 * The height follows the content as measured, not height:auto, so a list
 * narrowing as a query is typed animates too — not only opening and closing.
 * It is measured before paint whenever the content changes (so the spring
 * starts on the first frame, from where the tray was) and by a
 * ResizeObserver in between (a question moving to its next step). Contents
 * swap with popLayout: the old one is lifted out of the flow to fade while the
 * new one already sets the height — waiting for the old one to leave first
 * dropped the tray towards zero between the two. Under Reduce Motion the
 * height snaps and contents only fade.
 *
 * Opaque, like the lists it replaces: it sits in the composer's layer, over
 * the transcript, and glass there let the conversation read through the rows
 * (see COMPOSER_LIST's note in AgentComposer).
 */
export function ComposerTray({
  strip,
  panel,
  compact,
  onToggleCompact,
  className,
}: {
  /** Omitted, only the panel is drawn — a composer with no status to give still shows its "@" and "/" lists here. */
  strip?: React.ReactNode;
  panel: TrayPanel | null;
  /** Icon-only chips — the rail's default, and anyone's on the toggle. */
  compact: boolean;
  onToggleCompact: () => void;
  className?: string;
}) {
  const reduce = useReducedMotion() ?? false;
  const innerRef = React.useRef<HTMLDivElement>(null);
  // A motion value animated imperatively, not `animate={{ height }}`: it is a
  // number from the first measure on, so opening grows from 0 — animated from
  // an unset height the first open had nothing to start from and appeared at
  // full height on its first frame — and each move can pick its own speed,
  // growing or shrinking.
  const height = useMotionValue(0);
  // Until the first measure (the server render, the first client render) the
  // panel area takes its content's own height.
  const [measured, setMeasured] = React.useState(false);
  const measuredRef = React.useRef(false);
  const reduceRef = React.useRef(reduce);
  reduceRef.current = reduce;
  const measure = React.useCallback(() => {
    const inner = innerRef.current;
    if (!inner) return;
    const next = inner.offsetHeight;
    if (reduceRef.current || !measuredRef.current) height.jump(next);
    else if (next !== height.get()) animate(height, next, next > height.get() ? GROW : SHRINK);
    measuredRef.current = true;
    setMeasured(true);
  }, [height]);

  // Before paint, on every change of content: the new panel is already in the
  // DOM (the old one lifted out by popLayout), so this is its height.
  React.useLayoutEffect(measure, [measure, panel?.key]);
  React.useEffect(() => {
    const inner = innerRef.current;
    if (!inner || typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(measure);
    observer.observe(inner);
    return () => observer.disconnect();
  }, [measure]);

  return (
    <TrayCompactContext.Provider value={compact}>
      <div
        data-testid="composer-tray"
        data-compact={compact || undefined}
        data-panel={panel?.key}
        className={cn(
          // Inset by at least the composer's corner radius so its bottom edge
          // meets the composer's straight top edge, and flush with it: the
          // tray reads as part of the input it belongs to.
          "mx-3 flex min-w-0 flex-col overflow-hidden rounded-t-xl border border-b-0 border-glass-border bg-surface-base",
          className,
        )}
      >
        <motion.div
          style={{ height: measured ? height : undefined, overflow: "hidden" }}
          // The seam to the strip belongs to the panel area, not to each
          // content: two contents crossing would otherwise draw two.
          className={cn(panel && "border-b border-border-subtle")}
        >
          {/* relative: popLayout positions the leaving content against it. */}
          <div ref={innerRef} className="relative">
            <AnimatePresence initial={false} mode="popLayout">
              {panel ? (
                <motion.div
                  key={panel.key}
                  data-testid="composer-tray-panel"
                  variants={reduce ? REDUCED_CONTENT_VARIANTS : CONTENT_VARIANTS}
                  initial="enter"
                  animate="center"
                  exit="exit"
                >
                  {panel.content}
                </motion.div>
              ) : null}
            </AnimatePresence>
          </div>
        </motion.div>
        {strip === undefined ? null : (
        <div data-testid="composer-tray-strip" className="flex h-8 min-w-0 items-center gap-0.5 px-1.5">
          <div className="flex min-w-0 flex-1 items-center gap-0.5 overflow-hidden">{strip}</div>
          <ActionTooltip label={compact ? "Show labels" : "Icons only"}>
            <Button
              type="button"
              variant="ghost"
              size="icon-xs"
              aria-label={compact ? "Show labels" : "Icons only"}
              aria-pressed={compact}
              data-testid="composer-tray-compact"
              onClick={onToggleCompact}
              className="shrink-0 text-text-tertiary hover:text-text-primary"
            >
              {compact ? <ChevronsRight aria-hidden="true" /> : <ChevronsLeft aria-hidden="true" />}
            </Button>
          </ActionTooltip>
        </div>
        )}
      </div>
    </TrayCompactContext.Provider>
  );
}

/** A chip arriving in or leaving the strip: opacity and scale only (§4.2); leaving is quicker. */
const CHIP_MOTION = {
  initial: { opacity: 0, scale: 0.9 },
  animate: { opacity: 1, scale: 1, transition: { duration: MOTION_BASE, ease: EASE_OUT_STRONG } },
  exit: { opacity: 0, scale: 0.9, transition: { duration: MOTION_FAST, ease: EASE_OUT_STRONG } },
};

/**
 * One thing the strip says. With `onClick` it is a button that opens its
 * panel (`active` while that panel is showing); without, a plain label.
 *
 * `tone="attention"` is for something waiting on the person — a permission or
 * a question folded away to read the transcript — and pulses so it is not
 * forgotten there. Nothing else moves: a chip for work under way (subagents)
 * says so with its count, and an icon beating beside the composer for the
 * whole of a long run pulled the eye from what was being typed.
 */
export function TrayChip({
  icon,
  label,
  count,
  title,
  tone = "default",
  active = false,
  onClick,
  testId,
}: {
  icon: React.ReactNode;
  label?: React.ReactNode;
  count?: number;
  title?: string;
  tone?: "default" | "attention";
  active?: boolean;
  onClick?: () => void;
  testId?: string;
}) {
  const compact = React.useContext(TrayCompactContext);
  const reduce = useReducedMotion() ?? false;
  const name = title ?? (typeof label === "string" ? label : undefined);
  const body = (
    <>
      <span
        aria-hidden="true"
        className={cn(
          "flex shrink-0 items-center [&_svg]:size-3.5",
          tone === "attention" ? "text-status-warning" : "text-icon-secondary",
        )}
      >
        {icon}
      </span>
      {/* The label folds away rather than vanishing when the strip goes
          icon-only — a grid track going 1fr → 0fr, which CSS animates without
          any measuring (§3.5); its gap is its own padding, so it folds too. */}
      {label !== undefined ? (
        <span
          aria-hidden={compact || undefined}
          className={cn(
            "grid transition-[grid-template-columns,opacity] duration-(--motion-base) ease-(--ease-out-strong) motion-reduce:transition-none",
            compact ? "grid-cols-[0fr] opacity-0" : "grid-cols-[1fr] opacity-100",
          )}
        >
          <span className="min-w-0 truncate pl-1.5">{label}</span>
        </span>
      ) : null}
      {count !== undefined ? <span className="ml-1.5 shrink-0 tabular-nums">{count}</span> : null}
      {tone === "attention" ? (
        <span aria-hidden="true" className="ml-1.5 size-1.5 shrink-0 rounded-full bg-status-warning motion-safe:animate-pulse" />
      ) : null}
    </>
  );
  const chipClass = cn(
    // No gap: the label animates its own margin, so a folded label leaves none.
    "flex h-6 min-w-0 shrink items-center rounded-sm px-1.5 text-caption-11 whitespace-nowrap",
    tone === "attention" ? "text-text-primary" : "text-text-secondary",
    onClick && "cursor-pointer hover:bg-state-soft hover:text-text-primary",
    active && "bg-state-soft text-text-primary",
  );
  const chip = onClick ? (
    <button
      type="button"
      data-testid={testId}
      aria-pressed={active}
      aria-label={compact ? name : undefined}
      title={compact ? undefined : name}
      onClick={onClick}
      className={chipClass}
    >
      {body}
    </button>
  ) : (
    <span data-testid={testId} title={name} className={chipClass}>
      {body}
    </span>
  );
  return (
    // No `layout` here: a FLIP measure per chip is the per-element layout
    // §6.7 warns against; the strip's neighbours step over, which at 24px is
    // not a jump anyone reads as one.
    <motion.span {...(reduce ? {} : CHIP_MOTION)} className="flex min-w-0 shrink">
      {compact && name && onClick ? <ActionTooltip label={name}>{chip}</ActionTooltip> : chip}
    </motion.span>
  );
}

/** Chips come and go (a subagent starts, a permission is folded away) — this animates them. */
export function TrayChips({ children }: { children: React.ReactNode }) {
  return <AnimatePresence initial={false}>{children}</AnimatePresence>;
}
