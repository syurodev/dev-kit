import type * as React from "react";
import { useEffect, useId, useRef, useState } from "react";
import { AlertCircle, Check, Clock, Copy, Play, Square, TriangleAlert } from "lucide-react";
import { Button } from "@/components/ui/button";
import { ActionTooltip } from "@/components/ui/action-tooltip";
import { cn } from "@/lib/utils";
import { copyText } from "../../../bridge/clipboard";
import type { ConsoleStatus } from "../consoleStatus";

/**
 * Run, and what came back, as one body of liquid.
 *
 * Pressing Run draws a second shape out of it: the button stays where it is
 * and becomes Stop, while a pill carrying the clock and the outcome grows out
 * to its left. They are never quite separate — a neck of the same material
 * runs between them — which is the point. The pill is not a status bar that
 * appeared somewhere else on the screen; it is the button telling you what it
 * is doing.
 *
 * Why the metaball filter rather than an authored neck: the neck is emergent.
 * Two shapes under one blur-and-threshold pass grow a waist between them, thin
 * it as they part and snap it when they are far enough — for free, from the
 * only thing actually being animated, which is the pill's width. An authored
 * neck would have to be told about every frame of that.
 *
 * The filter blurs everything under it, text included, so the group is drawn
 * twice: once for the shapes with the type transparent, and once for the type
 * with the shapes transparent. The reference implementation this comes from
 * (liquid-taffy) swaps a crisp layer in at rest to stop a CSS border doubling
 * against the filter's own rim; there is nothing to double here, because these
 * two controls are solid fills with no border, so the goo draws the whole
 * picture the whole time.
 */

/**
 * The blur, and when it is allowed to exist.
 *
 * The joint was solved twice the wrong way round. Tuned across a 6px gap it
 * welded for a long pill and broke for "9.9s", because how far a shape's
 * blurred alpha reaches depends on how much of it there is — two blobs with a
 * nub each, pointing at a neck that was not there. Moved to touching it welded
 * at every width, but a capsule end meets a flat edge at ONE POINT, and a
 * metaball welded at a point is a cusp: the notch that was left.
 *
 * Both were attempts to keep a neck standing still. It should not stand still.
 * The two bodies rest APART — crisp, unfiltered, the button the same square as
 * the five controls beside it — and the goo exists only while the pill is
 * moving. That is 180ms of stretching neck that thins and snaps, which is what
 * taffy does, and it is also the only window in which the filter's corner
 * inflation is on screen.
 *
 * It also makes the break correct rather than a defect: the pill starts UNDER
 * the button, so the early frames are overlapped and weld at any width, and
 * the neck parts because the pill pulled away.
 */
const GOO_BLUR = 4;

/** Where they come to rest: far enough apart to be two things. */
const GOO_GAP = "gap-1.5";

/**
 * alpha' = 26a − 13, so the silhouette is everything the blur left above 50%.
 * The interior keeps the source colours, which is what lets the neck run a
 * gradient from the pill's fill into the button's.
 */
const GOO_THRESHOLD = "1 0 0 0 0  0 1 0 0 0  0 0 1 0 0  0 0 0 26 -13";

/**
 * The two boxes, declared once and worn by both layers.
 *
 * This is the whole of what keeps the glyphs registered on their shapes: the
 * layers are the same markup with the same classes, so there is no measuring
 * and nothing to fall out of step.
 */
const PILL_BOX =
  "flex h-(--control-height-sm) w-full min-w-0 items-center overflow-hidden rounded-full text-code-11";
const RUN_BOX = "h-(--control-height-sm) w-(--control-height-sm) shrink-0 p-0 gap-0";

/**
 * Every state is a filled tone, and the running one is the Stop button's own.
 *
 * Two reasons. A pill on --surface-raised measured 1.1:1 against the card it
 * sits on, so the body the neck ran into was barely there. And while the
 * statement is in flight the pill IS the button — one red body parting into
 * two red bodies is the whole of what the split has to say. It takes its own
 * colour only once there is an outcome to report.
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
 * all the way through every run, and the split animation happened to start at
 * the same instant the ticking did.
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
 * True while the pill is on its way in or out.
 *
 * transitionend is the honest end of the motion — a duration copied into JS is
 * a second place for --motion-base to live — but it never fires when the
 * transition is off, which is exactly what prefers-reduced-motion does. Hence
 * the fallback: someone who asked for no motion gets no goo either, which is
 * the right answer for them anyway.
 */
function useMoving(shown: boolean): [boolean, () => void] {
  const [moving, setMoving] = useState(false);
  const settled = useRef(true);
  useEffect(() => {
    if (settled.current) {
      settled.current = false;
      return;
    }
    setMoving(true);
    // Past the 500ms spring, so it only ever catches the case transitionend
    // cannot: a transition that was never allowed to run.
    const fallback = setTimeout(() => setMoving(false), 800);
    return () => clearTimeout(fallback);
  }, [shown]);
  return [moving, () => setMoving(false)];
}

type Layer = "shape" | "glyph";

/** Which control the keyboard is on, so the unfiltered layer can say so. */
type FocusTarget = "pill" | "run" | null;

/**
 * The ring, drawn on the glyph layer.
 *
 * A 1px ring under the goo is no ring at all: the blur spreads it until its
 * alpha never reaches the threshold, and it vanishes — measured, not guessed,
 * by forcing the focus-visible classes on and looking. So the shape layer
 * reports which control the keyboard is on and the layer ABOVE the filter
 * draws the indicator, at the same box, crisp.
 *
 * `:focus-visible` is asked of the element rather than reimplemented: a ring
 * that also appeared on a mouse click would be a different bug.
 *
 * It is a ring and not an outline because `cn("outline outline-2", …)` returns
 * only `outline-2` — tailwind-merge reads the bare `outline` as the same
 * width utility and drops it, leaving a width with no style to draw. A ring is
 * also what every other focused control in this app wears.
 */
const RING = "ring-1 ring-status-info";

function focusProps(
  shape: boolean,
  target: Exclude<FocusTarget, null>,
  onFocusChange: (target: FocusTarget) => void,
) {
  if (!shape) return {};
  return {
    onFocus: (event: React.FocusEvent<HTMLElement>) => {
      onFocusChange(event.currentTarget.matches(":focus-visible") ? target : null);
    },
    onBlur: () => onFocusChange(null),
  };
}

/**
 * One layer of the group.
 *
 * `shape` is the real, interactive one — it takes the pointer, carries hover
 * and focus, and is the thing the filter runs over. `glyph` rides on top of it
 * with no fill and no pointer, holding the type the filter would otherwise
 * smear.
 */
function Layer({
  layer,
  status,
  open,
  running,
  canRun,
  focus,
  onFocusChange,
  onPress,
  onToggle,
  onSettled,
  runLabel,
}: {
  layer: Layer;
  status: ConsoleStatus;
  open: boolean;
  running: boolean;
  canRun: boolean;
  focus: FocusTarget;
  onFocusChange: (target: FocusTarget) => void;
  onPress: () => void;
  onToggle: () => void;
  onSettled: () => void;
  runLabel: string;
}) {
  const shape = layer === "shape";
  const shown = status.kind !== "idle";
  const expandable = status.kind === "error";

  const pillClass = cn(
    PILL_BOX,
    // Capped so a long first line cannot push the rest of the toolbar off its
    // own row; the tail is what expanding is for.
    "max-w-72 gap-1.5 px-2.5",
    toneOf(status),
    // Transparent type on the shape layer, transparent fill on the glyph
    // layer: one picture between them, never two.
    shape ? "text-transparent [&_svg]:opacity-0" : "bg-transparent",
  );
  const body = (
    <>
      <StatusIcon status={status} />
      <span className="tabular-nums">{status.kind === "idle" ? "" : status.time}</span>
      {status.kind !== "idle" && status.kind !== "running" && (
        <span className="truncate">{status.message}</span>
      )}
    </>
  );

  return (
    <div
      className={cn(
        "flex min-w-0 items-center",
        GOO_GAP,
        // The glyph layer is a tracing of the shape layer laid over it: same
        // box, same flex, so every child lands on its own fill.
        !shape && "pointer-events-none absolute inset-0",
      )}
      aria-hidden={!shape || undefined}
    >
      {/* The pill. Width animates through a grid column rather than through
          `width`, which cannot transition to `auto` — 0fr to 1fr is a real
          animation to the content's own size, and the pill, being
          content-width, follows it. Growing is also what parts the two
          shapes, so the neck is drawn by the same tween. */}
      <div
        className={cn(
          "grid min-w-0 transition-[grid-template-columns,translate] motion-reduce:transition-none",
          // The spring, and the app already had one: --ease-house-spring is a
          // sampled polyline that peaks at 1.22, so the pill is flung past
          // where it lands and recoils. A stock ease over 180ms was the whole
          // reason this read as "a thing appeared" rather than "a thing was
          // pulled out of another thing" — 500ms is what the Dock pairs the
          // same spring with, and it is also how long the neck gets to exist.
          "motion-safe:duration-500 motion-safe:ease-house-spring",
          "duration-(--motion-fast) ease-(--ease-out-strong)",
          // Width AND a slide, together. Width alone made the pill materialise
          // beside the button: at nothing-wide its blurred mass is too small to
          // reach across the gap, so the first frames were a detached dot that
          // joined up later — the opposite of being pulled out of something.
          // Starting exactly the button's width to the right (28px, which is
          // --control-height-sm) puts it UNDER Run at zero, and the waist is
          // drawn by it leaving.
          shown ? "grid-cols-[1fr] translate-x-0" : "grid-cols-[0fr] translate-x-7",
        )}
        onTransitionEnd={shape ? onSettled : undefined}
      >
        <div className="min-w-0 overflow-hidden">
          {/* A button only when there is something behind it. An outcome you
              cannot open is a reading, not a control, and a <button> that does
              nothing is a promise to a screen reader the app does not keep.
              The reading gets role=status instead, so an icon-only toolbar
              says its outcome out loud at least once. */}
          {expandable ? (
            <button
              type="button"
              tabIndex={shape ? 0 : -1}
              aria-expanded={open}
              aria-label={`${status.message} — show the full error`}
              onClick={shape ? onToggle : undefined}
              {...focusProps(shape, "pill", onFocusChange)}
              className={cn(
                pillClass,
                shape && "cursor-pointer outline-none",
                !shape && focus === "pill" && RING,
              )}
            >
              {body}
            </button>
          ) : (
            <div role="status" className={pillClass}>
              {body}
            </div>
          )}
        </div>
      </div>

      {/* Run, which is Stop while it is running — see the button's own
          history: one control, because it is one decision. */}
      <ActionTooltip label={runLabel} wrap>
        <Button
          variant={running ? "danger" : "default"}
          size="xs"
          tabIndex={shape ? undefined : -1}
          {...focusProps(shape, "run", onFocusChange)}
          className={cn(
            RUN_BOX,
            "rounded-md",
            shape ? "[&_svg]:opacity-0 focus-visible:ring-0" : "bg-transparent",
            !shape && focus === "run" && RING,
          )}
          onClick={shape ? onPress : undefined}
          disabled={!canRun && !running}
        >
          {running ? <Square className="fill-current" /> : <Play className="fill-current" />}
        </Button>
      </ActionTooltip>
    </div>
  );
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
  const filterId = useId();
  const elapsed = useElapsed(running, startedAt);
  const status: ConsoleStatus = running
    ? { kind: "running", time: `${elapsed.toFixed(1)}s` }
    : outcome;
  const [open, setOpen] = useState(false);
  const [focus, setFocus] = useState<FocusTarget>(null);
  const [moving, settle] = useMoving(status.kind !== "idle");
  const panel = useRef<HTMLDivElement>(null);

  // A new run answers the previous one, so whatever was expanded about it is
  // no longer the thing on screen.
  useEffect(() => {
    if (running) setOpen(false);
  }, [running]);

  useEffect(() => {
    if (!open) return;
    const close = (event: MouseEvent) => {
      if (!panel.current?.contains(event.target as Node)) setOpen(false);
    };
    // Capture, so a click anywhere closes it before that click does its own
    // job — including a click on Run.
    document.addEventListener("mousedown", close, true);
    return () => document.removeEventListener("mousedown", close, true);
  }, [open]);

  const shared = {
    status,
    open,
    running,
    canRun,
    focus,
    onFocusChange: setFocus,
    onSettled: settle,
    onPress: running ? onStop : onRun,
    onToggle: () => setOpen((value) => !value),
    runLabel,
  };

  return (
    <div className="relative flex min-w-0 items-center">
      <svg aria-hidden className="pointer-events-none absolute size-0" focusable="false">
        <defs>
          <filter id={filterId} colorInterpolationFilters="sRGB">
            <feGaussianBlur in="SourceGraphic" stdDeviation={GOO_BLUR} result="blur" />
            <feColorMatrix in="blur" type="matrix" values={GOO_THRESHOLD} />
          </filter>
        </defs>
      </svg>

      {/* Only while something is actually moving. The filter inflates a
          corner, so a 28px square left under it permanently is the one round
          control in a row of square ones — paid, at rest, to animate nothing.

          The filter is inline rather than a class because its id is
          per-instance: two consoles side by side would otherwise both point at
          whichever `<defs>` the document happened to parse first. */}
      <div style={moving ? { filter: `url(#${filterId})` } : undefined}>
        <Layer layer="shape" {...shared} />
      </div>
      <Layer layer="glyph" {...shared} />

      {status.kind === "error" && open && (
        <ErrorPanel ref={panel} detail={status.detail} onClose={() => setOpen(false)} />
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
function ErrorPanel({
  ref,
  detail,
  onClose,
}: {
  ref: React.Ref<HTMLDivElement>;
  detail: string;
  onClose: () => void;
}) {
  const [copied, setCopied] = useState(false);
  return (
    <div
      ref={ref}
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
