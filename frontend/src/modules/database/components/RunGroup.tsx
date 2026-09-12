import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { motion } from "framer-motion";
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
 * How wide the body wants to be, measured from its own content.
 *
 * Animated as a NUMBER rather than through framer's `layout`, and the
 * difference matters here: `layout` animates size with a transform, which
 * scales the type inside — a control whose whole job is to be read would
 * squash its own text on every tick. A real width leaves the content alone
 * and moves the window onto it.
 *
 * Measured after every commit, because a render is what changes this content.
 * A ResizeObserver alone was not enough: it delivers on the rendering steps,
 * and when those are throttled the body keeps an older width and clips its own
 * status. The observer stays for the widths React is not told about — a font
 * finishing its load, the appearance setting changing the code size.
 */
function useContentWidth(): [React.RefObject<HTMLDivElement | null>, number | undefined] {
  const ref = useRef<HTMLDivElement>(null);
  const [width, setWidth] = useState<number>();

  useLayoutEffect(() => {
    const el = ref.current;
    if (el) setWidth((current) => (current === el.offsetWidth ? current : el.offsetWidth));
  });

  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    const observer = new ResizeObserver(() => {
      setWidth((current) => (current === el.offsetWidth ? current : el.offsetWidth));
    });
    observer.observe(el);
    return () => observer.disconnect();
  }, []);

  return [ref, width];
}

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
  const elapsed = useElapsed(running, startedAt);
  const status: ConsoleStatus = running
    ? { kind: "running", time: `${elapsed.toFixed(1)}s` }
    : outcome;
  const shown = status.kind !== "idle";
  const expandable = status.kind === "error";

  const [content, width] = useContentWidth();
  const [open, setOpen] = useState(false);
  const root = useRef<HTMLDivElement>(null);

  // A new run answers the previous one, so whatever was expanded about it is
  // no longer the thing on screen.
  useEffect(() => {
    if (running) setOpen(false);
  }, [running]);

  useCloseOnOutsideClick(open, () => setOpen(false), root);

  const reading = (
    <>
      <StatusIcon status={status} />
      <span className="tabular-nums">{status.kind === "idle" ? "" : status.time}</span>
      {status.kind !== "idle" && status.kind !== "running" && (
        <span className="max-w-64 truncate">{status.message}</span>
      )}
    </>
  );

  return (
    <div ref={root} className="relative flex shrink-0 items-center">
      <motion.div
        className={cn(
          BODY,
          toneOf(status),
          "transition-colors duration-(--motion-slow) ease-in-out motion-reduce:transition-none",
        )}
        // No entrance. The body is already the end's width on the first frame
        // and only ever changes size from there.
        initial={false}
        animate={{ width }}
        transition={SPRING}
      >
        {/* Sized to its content and pinned to the right, so narrowing the body
            clips the outcome off the LEFT and leaves the end where it was.
            Nothing in here moves; only the window onto it changes. */}
        <div ref={content} className="absolute top-0 right-0 flex w-max items-center">
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
                aria-label={`${status.message} — show the full error`}
                onClick={() => setOpen((value) => !value)}
                className={cn(SEGMENT, "cursor-pointer outline-none")}
              >
                {reading}
              </button>
            ) : (
              <div role="status" className={SEGMENT}>
                {reading}
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
              // --state-soft, not a hand-mixed black/white alpha: it is the
              // app's state layer and is already defined per mode — ink where
              // light recesses, white where dark lifts — so the hover darkens
              // a light theme's fill and lightens a dark one's.
              className="w-(--control-height-sm) shrink-0 gap-0 rounded-none bg-transparent p-0 hover:bg-state-soft focus-visible:ring-0"
              onClick={running ? onStop : onRun}
              disabled={!canRun && !running}
            >
              {running ? <Square className="fill-current" /> : <Play className="fill-current" />}
            </Button>
          </ActionTooltip>
        </div>
      </motion.div>

      {status.kind === "error" && open && (
        <ErrorPanel detail={status.detail} onClose={() => setOpen(false)} />
      )}
    </div>
  );
}

/**
 * The rest of the error.
 *
 * Absolute, and anchored to the group's right edge: the toolbar sits directly
 * above the editor, and a panel that took part in the layout would push the
 * whole console down every time a statement failed — the one moment you want
 * the query you just wrote to stay where you left it.
 */
function ErrorPanel({ detail, onClose }: { detail: string; onClose: () => void }) {
  const [copied, setCopied] = useState(false);
  return (
    <div
      className="absolute top-full right-0 z-30 mt-1.5 flex max-h-64 w-120 flex-col gap-2 rounded-md border border-(--border-danger) bg-surface-base p-3 shadow-lg"
    >
      <div className="flex shrink-0 items-center justify-between gap-2">
        <span className="text-caption-11 text-text-tertiary">What the engine said</span>
        <ActionTooltip label={copied ? "Copied" : "Copy the whole message"}>
          <Button
            variant="ghost"
            size="icon-sm"
            onClick={() => {
              void copyText(detail);
              setCopied(true);
              // The label is the feedback, and it has to go back to offering
              // the action or the next copy has nothing to say.
              setTimeout(() => setCopied(false), 1500);
            }}
          >
            {copied ? <Check /> : <Copy />}
          </Button>
        </ActionTooltip>
      </div>
      <pre className="min-h-0 flex-1 overflow-auto rounded border border-border bg-surface-well p-2 text-left text-code-11 whitespace-pre-wrap text-text-primary select-text">
        {detail}
      </pre>
      <Button variant="ghost" size="xs" className="self-end" onClick={onClose}>
        Close
      </Button>
    </div>
  );
}
