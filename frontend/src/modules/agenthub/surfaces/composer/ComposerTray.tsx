import * as React from "react";
import { AnimatePresence, animate, motion, useMotionValue, useReducedMotion, type Transition, type Variants } from "framer-motion";

import { cn } from "@/lib/utils";
import { ActionTooltip } from "@/components/ui/action-tooltip";
import { AnimatedNumber } from "@/components/ui/animated-number";
import { CONTENT_FADE_DELAY, EASE_OUT_STRONG, MOTION_BASE, MOTION_FAST, MOTION_SLOW, noMotion } from "@/lib/motion";

/**
 * What the composer's panel area shows above its context bar: an "@" or "/"
 * list while one is being typed, a permission or a question the agent is
 * waiting on, or a list the person opened from the bar. `key` names it, so
 * swapping one for another fades through instead of morphing one into the
 * other.
 */
export type TrayPanel = {
  key: string;
  content: React.ReactNode;
  /**
   * Escape inside the panel puts it away: a list closes, a permission folds
   * into the bar (the agent is still waiting). Without it Escape does nothing
   * here. Focus then goes back to the field — see AgentComposer.
   */
  onDismiss?: () => void;
};

/*
 * Motion, by docs/dev-kit/smoothness.mdx: the tokens (§4.1) — a panel opens
 * over --motion-slow and closes quicker, over --motion-base; springs with no
 * bounce, because this is data; interruptible, so a spring rather than a
 * tween (§4.5): a panel swapped mid-growth retargets from where it is, with
 * the speed it has. The panel area's height is the one layout property
 * animated — it is a panel container, the exception §4.2 allows; what is
 * inside moves only by transform and opacity, and trails the container by
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

/** Whether the bar around this is icon-only — for a control in it that is not a TrayChip. */
export function useTrayCompact(): boolean {
  return React.useContext(TrayCompactContext);
}

/** Provides the bar's icon-only mode to its chips; see ComposerContextBar. */
export function TrayCompactProvider({ compact, children }: { compact: boolean; children: React.ReactNode }) {
  return <TrayCompactContext.Provider value={compact}>{children}</TrayCompactContext.Provider>;
}

/**
 * The panel area of the composer cluster, the first thing in it: a permission
 * or question to answer, or a list to pick from. Part of the cluster, not a
 * card of its own — the cluster draws the border; this draws only the divider
 * under itself while it has something to show.
 *
 * The height follows the content as measured, not height:auto, so a list
 * narrowing as a query is typed animates too — not only opening and closing.
 * It is measured before paint whenever the content changes (so the spring
 * starts on the first frame, from where the area was) and by a ResizeObserver
 * in between (a question moving to its next step). Contents swap with
 * popLayout: the old one is lifted out of the flow to fade while the new one
 * already sets the height — waiting for the old one to leave first dropped the
 * area towards zero between the two. Under Reduce Motion the height snaps and
 * contents only fade.
 *
 * What is measured is what the panel's own content asks for, already capped by
 * the content (`composer-panel-cap` + its own scroll), so a tall panel is a
 * scrolling one and never taller than the pane allows.
 */
export function ComposerPanelArea({ panel, className }: { panel: TrayPanel | null; className?: string }) {
  const reduce = useReducedMotion() ?? false;
  const innerRef = React.useRef<HTMLDivElement>(null);
  // A motion value animated imperatively, not `animate={{ height }}`: it is a
  // number from the first measure on, so opening grows from 0 — animated from
  // an unset height the first open had nothing to start from and appeared at
  // full height on its first frame — and each move can pick its own speed,
  // growing or shrinking.
  const height = useMotionValue(0);
  // Until the first measure (the server render, the first client render) the
  // area takes its content's own height.
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
    <motion.div
      data-testid="composer-panel-area"
      data-panel={panel?.key}
      style={{ height: measured ? height : undefined, overflow: "hidden" }}
      // Escape puts the panel away from wherever focus is inside it. Not for a
      // key an input in the panel is using (a select, an IME composition).
      onKeyDown={(event) => {
        if (event.key !== "Escape" || event.defaultPrevented || event.nativeEvent.isComposing) return;
        if (!panel?.onDismiss) return;
        event.preventDefault();
        event.stopPropagation();
        panel.onDismiss();
      }}
      // The seam to what is below belongs to the area, not to each content: two
      // contents crossing would otherwise draw two.
      className={cn("shrink-0", panel && "border-b border-border-subtle", className)}
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
  );
}

/** A chip arriving in or leaving the bar: opacity and scale only (§4.2); leaving is quicker. */
const CHIP_MOTION = {
  initial: { opacity: 0, scale: 0.9 },
  animate: { opacity: 1, scale: 1, transition: { duration: MOTION_BASE, ease: EASE_OUT_STRONG } },
  exit: { opacity: 0, scale: 0.9, transition: { duration: MOTION_FAST, ease: EASE_OUT_STRONG } },
};

/** A second number on a chip that is not the first's kind — "6 of these 8 need a look". */
export type TrayChipBadge = { value: number; tone: "warning" | "danger"; label: string };

/**
 * One thing the context bar says. With `onClick` it is a button that opens its
 * panel (`active` while that panel is showing); without, a plain label.
 *
 * `tone="attention"` is for something waiting on the person — a permission or
 * a question folded away to read the transcript. It is told apart by shape and
 * text, never colour alone: the shield or question glyph, the word, a dot. The
 * dot beats a few times when the request ARRIVES (`pulse`, decided by the
 * caller from the rising edge) and then stands still; nothing here animates
 * forever, and a chip for work under way (subagents) says so with its count.
 *
 * `badge` is a count of a different thing from the chip's own: a repository
 * chip says how many repositories, its badge how many of them need a look. Two
 * numbers in one pill read as one ("8 repos 6"), so the second gets its own
 * rounded badge, a tone and a sentence in the chip's name.
 *
 * `priority` chips are never squeezed out by their neighbours — the permission
 * that blocks the agent is not the thing to truncate. `keepLabel` keeps the
 * label when the bar is narrow (the workspace says where you are).
 */
export function TrayChip({
  icon,
  label,
  count,
  badge,
  title,
  tone = "default",
  active = false,
  pulse = false,
  priority = false,
  keepLabel = false,
  onClick,
  testId,
}: {
  icon: React.ReactNode;
  label?: React.ReactNode;
  count?: number;
  badge?: TrayChipBadge;
  title?: string;
  tone?: "default" | "attention";
  active?: boolean;
  pulse?: boolean;
  priority?: boolean;
  keepLabel?: boolean;
  onClick?: () => void;
  testId?: string;
}) {
  const compact = React.useContext(TrayCompactContext);
  const reduce = useReducedMotion() ?? false;
  const base = title ?? (typeof label === "string" ? label : undefined);
  const name = base && badge ? `${base} — ${badge.label}` : base;
  const body = (
    <>
      <span
        aria-hidden="true"
        className={cn(
          "flex shrink-0 items-center [&_svg]:size-3.5",
          tone === "attention" ? "text-status-warning" : "text-icon-secondary",
          pulse && "agent-attention-pulse",
        )}
      >
        {icon}
      </span>
      {/* The label folds away rather than vanishing when the bar goes icon-only
          — a grid track going 1fr → 0fr, which CSS animates without any
          measuring (§3.5); its gap is its own padding, so it folds too. In a
          narrow bar it is simply not drawn: the container query asks the bar's
          own width, not the window's. */}
      {label !== undefined ? (
        <span
          aria-hidden={compact || undefined}
          className={cn(
            "grid transition-[grid-template-columns,opacity] duration-(--motion-base) ease-(--ease-out-strong) motion-reduce:transition-none",
            !keepLabel && "@max-sm:hidden",
            compact && !keepLabel ? "grid-cols-[0fr] opacity-0" : "grid-cols-[1fr] opacity-100",
          )}
        >
          <span className="min-w-0 truncate pl-1.5">{label}</span>
        </span>
      ) : null}
      {count !== undefined ? <AnimatedNumber value={count} className="ml-1.5 min-w-3 shrink-0 text-center tabular-nums" /> : null}
      {badge ? (
        <span
          data-testid={testId ? `${testId}-badge` : undefined}
          data-tone={badge.tone}
          className={cn(
            "ml-1.5 inline-flex h-4 min-w-4 shrink-0 items-center justify-center rounded-full px-1 text-caption-11-medium tabular-nums",
            badge.tone === "danger" ? "bg-status-danger/15 text-status-danger" : "bg-status-warning/15 text-status-warning",
          )}
        >
          <span aria-hidden="true">{badge.value}</span>
          <span className="sr-only">{badge.label}</span>
        </span>
      ) : null}
      {tone === "attention" ? (
        <span
          aria-hidden="true"
          className={cn("ml-1.5 size-1.5 shrink-0 rounded-full bg-status-warning", pulse && "agent-attention-pulse")}
        />
      ) : null}
    </>
  );
  const chipClass = cn(
    // No gap: the label animates its own margin, so a folded label leaves none.
    "flex h-6 min-w-0 items-center rounded-md px-2 text-caption-11 whitespace-nowrap",
    priority ? "shrink-0" : "shrink",
    tone === "attention" ? "text-text-primary" : "text-text-secondary",
    onClick && "cursor-pointer transition-control hover:bg-state-soft hover:text-text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-status-info",
    active && "bg-state-soft text-text-primary",
    pulse && tone === "attention" && "agent-attention-flash",
  );
  const chip = onClick ? (
    <button
      type="button"
      data-testid={testId}
      aria-pressed={active}
      // The name is always set when there is one: a badge's sentence is part of
      // it, and a compact chip has no label to read it from.
      aria-label={compact || badge || tone === "attention" ? name : undefined}
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
    // §6.7 warns against; the bar's neighbours step over, which at 24px is
    // not a jump anyone reads as one.
    <motion.span {...(reduce ? {} : CHIP_MOTION)} className={cn("flex min-w-0", priority ? "shrink-0" : "shrink")}>
      {compact && name && onClick ? <ActionTooltip label={name}>{chip}</ActionTooltip> : chip}
    </motion.span>
  );
}

/** Chips come and go (a subagent starts, a permission is folded away) — this animates them. */
export function TrayChips({ children }: { children: React.ReactNode }) {
  return <AnimatePresence initial={false}>{children}</AnimatePresence>;
}
