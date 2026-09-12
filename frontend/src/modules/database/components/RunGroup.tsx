import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { motion, useReducedMotion } from "framer-motion";
import { AlertCircle, Check, Clock, Copy, Play, Square, TriangleAlert } from "lucide-react";
import { Button } from "@/components/ui/button";
import { ActionTooltip } from "@/components/ui/action-tooltip";
import { cn } from "@/lib/utils";
import { copyText } from "../../../bridge/clipboard";
import type { ConsoleStatus } from "../consoleStatus";

/**
 * Run, and what came back, as one control that changes width.
 *
 * ONE body. There are two hit areas inside it — press the outcome to open an
 * error, press the end to run or stop — but there is one rounded rect, one
 * fill, one focus ring and one width, and the body owns all four. The
 * segments are laid out BY it rather than being two things placed side by
 * side. Press Run and it opens to the left: the end stays where it is and
 * becomes Stop, and the room it makes fills with the clock, then the outcome.
 *
 * There was a metaball version: two bodies under a blur-and-threshold pass
 * with a real liquid neck. It is gone, and the reasons are kept so nobody
 * rebuilds it. The filter inflates a corner, so a 28px square came back
 * visibly rounder than the five controls beside it. How far a shape's blurred
 * alpha reaches depends on how much of the shape there is, so a joint tuned
 * against a long message broke against "9.9s" and left two blobs with a nub
 * each. And a capsule end meets a flat edge at one point, which is a cusp,
 * not a waist. One body has no joint, so it has none of them.
 */

/** The body. Everything that makes it one thing is declared here, once. */
const BODY =
  "relative flex h-(--control-height-sm) shrink-0 items-center overflow-hidden rounded-md" +
  " has-[:focus-visible]:ring-1 has-[:focus-visible]:ring-status-info";

/** A segment of it: same height, no fill, no radius and no ring of its own. */
const SEGMENT =
  "flex h-(--control-height-sm) shrink-0 items-center gap-1.5 pr-1.5 pl-2.5 text-code-11";

/**
 * The app's own spring, as framer takes it.
 *
 * index.css samples --ease-house-spring from ζ=0.434, ω=22.46; at mass 1 that
 * is stiffness ω² and damping 2ζω. The same curve the Dock uses, written the
 * other way round.
 *
 * A spring rather than a CSS transition because this width is re-targeted
 * while it is still moving — the clock gains a digit, then the outcome
 * replaces the clock. A transition restarts from wherever it had reached with
 * a fresh ease and throws its momentum away; a spring keeps its velocity and
 * bends toward the new number.
 */
const SPRING = { type: "spring", stiffness: 504, damping: 19.5, mass: 1 } as const;

/**
 * The summary leaves downwards and the message arrives from where it was.
 *
 * Opening swaps one line of text for another at a different place on screen,
 * and a swap with no motion between them reads as two unrelated things, which
 * is the complaint this answers: it should be the same text carrying on down
 * the control. So the summary drifts down out of the row as the message rises
 * into place from the row's own line — one direction, shared, which is what
 * makes the eye follow it as one movement instead of tracking two.
 *
 * Opacity on a short tween rather than on the spring: a spring overshoots,
 * and an overshoot in opacity is a flicker.
 */
const TEXT_MOTION = { ...SPRING, opacity: { duration: 0.12 } } as const;

/** How far the summary drifts on its way out. Enough to read as leaving. */
const SUMMARY_DRIFT = 10;

/** What every one of those becomes under prefers-reduced-motion. */
const INSTANT = { duration: 0 } as const;

/**
 * Every state is a filled tone, and the running one is the Stop end's own.
 *
 * A status on --surface-raised measured 1.1:1 against the card it sits on. And
 * the fill is on the BODY, not on a segment: one colour across the whole
 * control is what makes the opening read as this thing growing rather than as
 * something arriving beside it.
 */
function toneOf(status: ConsoleStatus): string {
  switch (status.kind) {
    case "error":
      return "bg-status-danger text-surface-base";
    case "warning":
      return "bg-status-warning text-surface-base";
    case "success":
      return "bg-status-success text-surface-base";
    default:
      return "bg-status-danger text-surface-base";
  }
}

function StatusIcon({ status }: { status: ConsoleStatus }) {
  switch (status.kind) {
    case "error":
      return <AlertCircle className="size-3 shrink-0" />;
    case "warning":
      return <TriangleAlert className="size-3 shrink-0" />;
    case "success":
      return <Check className="size-3 shrink-0" />;
    default:
      return <Clock className="size-3 shrink-0" />;
  }
}

/**
 * The running clock, owned by the only thing that shows it.
 *
 * It ticks ten times a second, so where it lives decides what re-renders ten
 * times a second. It used to live in QueryConsole, which owns the editor and
 * the result grid: a table of rows nobody had asked to change was re-rendered
 * all the way through every run.
 */
function useElapsed(running: boolean, startedAt?: number | null): number {
  const [elapsed, setElapsed] = useState(0);
  useEffect(() => {
    if (!running || !startedAt) {
      setElapsed(0);
      return;
    }
    const tick = () => setElapsed((Date.now() - startedAt) / 1000);
    tick();
    const interval = setInterval(tick, 100);
    return () => clearInterval(interval);
  }, [running, startedAt]);
  return elapsed;
}

/**
 * How big a piece of content wants to be.
 *
 * Animated as NUMBERS rather than through framer's `layout`, and the
 * difference matters here: `layout` animates size with a transform, which
 * scales the type inside — a control whose whole job is to be read would
 * squash its own text on every tick. Real width and height leave the content
 * alone and move the window onto it.
 *
 * Measured after every commit, because a render is what changes this content.
 * A ResizeObserver alone was not enough: it delivers on the rendering steps,
 * and when those are throttled the box keeps an older size and clips itself.
 * The observer stays for the sizes React is not told about — a font finishing
 * its load, the appearance setting changing the code size.
 */
function useMeasured(): [React.RefObject<HTMLDivElement | null>, { width: number; height: number }] {
  const ref = useRef<HTMLDivElement>(null);
  const [size, setSize] = useState({ width: 0, height: 0 });

  const sync = () => {
    const el = ref.current;
    if (!el) return;
    setSize((current) =>
      current.width === el.offsetWidth && current.height === el.offsetHeight
        ? current
        : { width: el.offsetWidth, height: el.offsetHeight },
    );
  };

  useLayoutEffect(sync);

  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    const observer = new ResizeObserver(sync);
    observer.observe(el);
    return () => observer.disconnect();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return [ref, size];
}

/**
 * The tallest the opened message may get before it scrolls instead.
 *
 * It opens over the editor, so it is borrowing space rather than taking it.
 * Past this an engine's answer is not more readable for being taller — it is
 * just covering more of the query it is about.
 */
const MAX_DETAIL_HEIGHT = 220;

/** The header row: one control tall, always. */
const HEADER_HEIGHT = 28;

/**
 * The narrowest the body may be while open. A first line truncated to 260px
 * is a fine summary; the same width is a bad column to read six lines of a
 * stack trace in.
 */
const MIN_DETAIL_WIDTH = 420;

/**
 * Closes on the next click that lands outside this control.
 *
 * Two things here are scar tissue.
 *
 * `click` in the BUBBLE phase, not `mousedown` in the capture phase. The
 * capture version closed "before the click does its own job" — but closing is
 * setState, React re-renders between mousedown and mouseup, and a browser
 * only fires `click` when both landed on the same element, so the press that
 * closed the panel could be the press that got swallowed.
 *
 * And it ignores anything inside `root`, which is what makes the panel open
 * at all. The effect registers this listener as a result of the click that
 * set `open` — while that same click is still on its way up to the document.
 * Without the check the listener caught the opening click and closed the
 * panel in the same gesture, so pressing the outcome did visibly nothing.
 */
function useCloseOnOutsideClick(
  open: boolean,
  onClose: () => void,
  root: React.RefObject<HTMLElement | null>,
) {
  useEffect(() => {
    if (!open) return;
    const close = (event: MouseEvent) => {
      if (!root.current?.contains(event.target as Node)) onClose();
    };
    document.addEventListener("click", close);
    return () => document.removeEventListener("click", close);
  }, [open, onClose, root]);
}

export function RunGroup({
  outcome,
  running,
  startedAt,
  canRun,
  onRun,
  onStop,
  runLabel,
}: {
  /** What the last finished statement came to. Idle until one has. */
  outcome: ConsoleStatus;
  running: boolean;
  /** When the current run started, for the clock this component owns. */
  startedAt?: number | null;
  /** False with no connection: Run has nothing to run against. */
  canRun: boolean;
  onRun: () => void;
  onStop: () => void;
  runLabel: string;
}) {
  // Everything in here moves: the body's width and height, and the text
  // between its two places. One switch turns all of it into a cut, because a
  // person who has asked their system for less motion did not mean "less in
  // the parts I remembered to ask about".
  const motionOff = useReducedMotion() ?? false;
  const elapsed = useElapsed(running, startedAt);
  const status: ConsoleStatus = running
    ? { kind: "running", time: `${elapsed.toFixed(1)}s` }
    : outcome;
  const shown = status.kind !== "idle";
  const expandable = status.kind === "error";

  const [header, headerSize] = useMeasured();
  const [detail, detailSize] = useMeasured();
  // Open from the FIRST render when there is already an error, not opened by
  // an effect afterwards: an effect runs after paint, so a restored tab drew
  // its error collapsed for one frame and then jumped.
  const [open, setOpen] = useState(() => outcome.kind === "error");
  const root = useRef<HTMLDivElement>(null);

  // A new run answers the previous one, so whatever was open about the last
  // one is no longer the thing on screen.
  useEffect(() => {
    if (running) setOpen(false);
  }, [running]);

  /*
   * An error opens itself.
   *
   * Reading it was the point of running the statement, so making that a
   * second click was charging for something nobody would decline. Keyed on
   * startedAt as well as the kind, because the same typo run twice is two
   * errors and the second must open again — and because neither changes when
   * you close it, closing sticks.
   */
  useEffect(() => {
    if (status.kind === "error") setOpen(true);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [status.kind, startedAt]);

  useCloseOnOutsideClick(open, () => setOpen(false), root);

  const expanded = expandable && open;
  // The width the body has WHEN OPEN, computed without reference to the width
  // it currently has. The message is measured at this width rather than at
  // whatever the animation is passing through, because the two depend on each
  // other: a narrower body wraps the text taller, a taller measurement is
  // what the body animates to. Measured mid-animation it settled on a height
  // for a width it no longer had — 216px of body around 120px of message.
  const openWidth = Math.max(headerSize.width, MIN_DETAIL_WIDTH);
  // Clamped rather than unbounded: past MAX_DETAIL_HEIGHT the message scrolls
  // inside itself instead of covering more of the query it is about.
  const detailHeight = Math.min(detailSize.height, MAX_DETAIL_HEIGHT);
  const height = HEADER_HEIGHT + (expanded ? detailHeight : 0);
  const width = expanded ? openWidth : headerSize.width;

  /*
   * The reading, with or without its summary line.
   *
   * Open, the summary goes: the whole message is about to be printed two
   * pixels below it, and the same sentence twice — once clipped, once not —
   * is what made the open state read as two objects instead of one taller
   * one. What stays is the icon and the clock, which the message is not.
   */
  const hasSummary = status.kind !== "idle" && status.kind !== "running";
  const reading = (live = false) => (
    <>
      <StatusIcon status={status} />
      <span className="tabular-nums">{status.kind === "idle" ? "" : status.time}</span>
      {hasSummary &&
        (live ? (
          // Still mounted while open, at zero opacity: it holds the row's
          // width steady, and something that has unmounted cannot animate its
          // way out.
          <motion.span
            className="max-w-64 truncate"
            // No entrance: a console restored with an error is already open,
            // and animating the summary out of a row it was never in is a
            // flash on arrival.
            initial={false}
            animate={{ opacity: expanded ? 0 : 1, y: expanded ? SUMMARY_DRIFT : 0 }}
            transition={motionOff ? INSTANT : TEXT_MOTION}
            aria-hidden={expanded}
          >
            {status.message}
          </motion.span>
        ) : (
          <span className="max-w-64 truncate">{status.message}</span>
        ))}
    </>
  );

  return (
    /*
     * The wrapper keeps the toolbar's layout: one control's height, the
     * header's width, and it never changes. Everything that grows is
     * absolutely positioned inside it, so opening an error borrows space over
     * the editor instead of pushing the query you just wrote down the screen.
     */
    <div
      ref={root}
      className="relative h-(--control-height-sm) shrink-0"
      style={{ width: headerSize.width || undefined }}
    >
      <motion.div
        className={cn(
          BODY,
          toneOf(status),
          "absolute top-0 right-0 z-30 flex-col items-stretch",
          "transition-colors duration-(--motion-slow) ease-in-out motion-reduce:transition-none",
        )}
        // No entrance. The body is already the end's size on the first frame
        // and only ever changes from there.
        initial={false}
        animate={{ width, height }}
        transition={motionOff ? INSTANT : SPRING}
      >
        {/*
          The closed row, measured and never seen.

          The visible row changes shape when it opens — it spans the body so
          the clock sits over the first character of the message rather than
          out at the right-hand end — and a row that spans the body cannot
          also report how wide the body is when it doesn't. This ghost is the
          one answer to that question: what this control occupies in the
          toolbar, and what the body shrinks back to when the error closes.
        */}
        <div
          ref={header}
          aria-hidden
          className="pointer-events-none invisible absolute top-0 right-0 flex w-max items-center"
        >
          {shown && <div className={SEGMENT}>{reading()}</div>}
          <div className="size-(--control-height-sm)" />
        </div>

        {/* Sized to its content and pinned to the right, so a body narrower
            than this clips the outcome off the LEFT and leaves the end where
            it was. Nothing in here moves; only the window onto it changes.
            Open, it spans the body instead, which puts the clock at the
            message's own left margin and Run out at the end. */}
        <div
          className={cn(
            "absolute top-0 right-0 flex items-center",
            expanded ? "left-0" : "w-max",
          )}
        >
          {shown &&
            /* A button only when there is something behind it. An outcome you
               cannot open is a reading, not a control, and a <button> that
               does nothing is a promise to a screen reader the app does not
               keep. The reading gets role=status instead, so an icon-only
               toolbar says its outcome out loud at least once. */
            (expandable ? (
              <button
                type="button"
                aria-expanded={open}
                aria-label={
                  open ? "Hide the full error" : `${status.message} — show the full error`
                }
                onClick={() => setOpen((value) => !value)}
                className={cn(SEGMENT, "cursor-pointer outline-none", expanded && "grow")}
              >
                {reading(true)}
              </button>
            ) : (
              <div role="status" className={SEGMENT}>
                {reading()}
              </div>
            ))}
          <ActionTooltip label={runLabel} wrap>
            <Button
              variant={running ? "danger" : "default"}
              size="xs"
              // The body owns the fill, the radius and the ring; this end owns
              // none of them. It is a hit area with a glyph in it, and the
              // only thing it paints is its own hover.
              //
              // Its height comes from `size` and NOT from h-full: ActionTooltip's
              // `wrap` puts a content-sized span between it and the row, so
              // 100% resolved against 14px of icon and left the end half the
              // body's height.
              //
              // --state-soft for the hover, not a hand-mixed black/white
              // alpha: it is the app's state layer and is defined per mode —
              // ink where light recesses, white where dark lifts.
              className="w-(--control-height-sm) shrink-0 gap-0 rounded-none bg-transparent p-0 hover:bg-state-soft focus-visible:ring-0"
              onClick={running ? onStop : onRun}
              disabled={!canRun && !running}
            >
              {running ? <Square className="fill-current" /> : <Play className="fill-current" />}
            </Button>
          </ActionTooltip>
        </div>

        {/* The message, in the same body and on the same ground. It sits
            under the clock at the clock's own left margin, so the control
            opening is one block of text getting longer rather than a panel
            arriving under a bar. Measured at its natural height so the body
            can animate to it; past the cap it scrolls inside itself. */}
        {status.kind === "error" && (
          <div
            // Width pinned to what the body will have when open, never to what
            // it has now. inert while closed, because the Copy button inside
            // is clipped out of sight but would still be a tab stop.
            className="absolute right-0 overflow-hidden"
            style={{ top: HEADER_HEIGHT, width: openWidth, height: detailHeight }}
            inert={!expanded}
          >
            {/* No ground of its own. A second surface inside the control is
                a second object, and pl-2.5 is SEGMENT's own left padding —
                the message starts under the clock, at the same margin. */}
            {/* It rises into place from the row above — the line the summary
                is leaving — so the two read as one piece of text moving down
                the control rather than two that swapped. Transform only:
                offsetHeight is what the body animates to, and a transform
                does not change it. */}
            <motion.div
              ref={detail}
              className="flex flex-col gap-0.5 pr-1.5 pb-1.5 pl-2.5"
              initial={false}
              animate={{ y: expanded ? 0 : -HEADER_HEIGHT, opacity: expanded ? 1 : 0 }}
              transition={motionOff ? INSTANT : TEXT_MOTION}
            >
              <pre
                className="overflow-auto text-left text-code-11 whitespace-pre-wrap select-text"
                style={{ maxHeight: MAX_DETAIL_HEIGHT - 40 }}
              >
                {status.detail}
              </pre>
              <CopyDetail detail={status.detail} />
            </motion.div>
          </div>
        )}
      </motion.div>
    </div>
  );
}

/**
 * Copy, inside the message rather than beside it.
 *
 * It takes the body's own text colour rather than a secondary grey: the
 * ground here is the status tone, and a grey mixed for --surface-base is not
 * a colour anyone measured against red.
 */
function CopyDetail({ detail }: { detail: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <button
      type="button"
      onClick={() => {
        void copyText(detail);
        setCopied(true);
        // The label is the feedback, and it has to go back to offering the
        // action or the next copy has nothing to say.
        setTimeout(() => setCopied(false), 1500);
      }}
      className="flex h-(--control-height-xs) shrink-0 cursor-pointer items-center gap-1.5 self-end rounded-sm px-1.5 text-code-11 outline-none hover:bg-state-soft focus-visible:ring-1 focus-visible:ring-current"
    >
      {copied ? <Check className="size-3" /> : <Copy className="size-3" />}
      {copied ? "Copied" : "Copy"}
    </button>
  );
}
