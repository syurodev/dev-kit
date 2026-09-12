import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { AlertCircle, Check, Clock, Copy, Play, Square, TriangleAlert } from "lucide-react";
import { Button } from "@/components/ui/button";
import { ActionTooltip } from "@/components/ui/action-tooltip";
import { cn } from "@/lib/utils";
import { copyText } from "../../../bridge/clipboard";
import type { ConsoleStatus } from "../consoleStatus";

/**
 * Run, and what came back, as one control that changes width.
 *
 * Press Run and the body opens to the left: the button stays where it is and
 * becomes Stop, and the room it makes is filled by the clock, then by the
 * outcome. It is one rounded body with two segments, not two shapes — the
 * status is not a thing that appeared somewhere else on screen, it is this
 * control saying what it is doing.
 *
 * There was a metaball version of this: two bodies under a blur-and-threshold
 * pass, growing a real liquid neck between them. It is gone, and the reasons
 * are worth keeping so nobody rebuilds it. The filter inflates a corner, so a
 * 28px square came back visibly rounder than the five controls beside it. How
 * far a shape's blurred alpha reaches depends on how much of the shape there
 * is, so a joint tuned against a long message broke against "9.9s" and left
 * two blobs with a nub each. And a capsule end meets a flat edge at one point,
 * which is a cusp, not a waist. Every fix for one of those made another worse.
 * One body has none of the problems because it has no joint.
 */

/** The body: one radius, one clip, two segments inside it. */
const BODY =
  "relative h-(--control-height-sm) shrink-0 overflow-hidden rounded-md" +
  " has-[:focus-visible]:ring-1 has-[:focus-visible]:ring-status-info";

const SEGMENT =
  "flex h-full items-center gap-1.5 px-2.5 text-code-11 transition-colors" +
  " duration-(--motion-slow) ease-in-out motion-reduce:transition-none";

/**
 * Every state is a filled tone, and the running one is the Stop button's own.
 *
 * A status on --surface-raised measured 1.1:1 against the card it sits on. And
 * while the statement is in flight the left segment IS the button: one colour
 * across the whole body, which is what makes the opening read as the control
 * growing rather than as something arriving beside it.
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
 * The body's width, measured from its own content.
 *
 * `width` cannot transition to `auto`, and the grid-column trick this used to
 * lean on only animates between nothing and everything — so a statement that
 * finished while the body was ALREADY open changed the text from "0.4s" to
 * "0.4s 12 rows" and the body jumped. A fast query is that case every time,
 * which is why it looked like the animation only worked on slow ones.
 *
 * Measuring makes every width change the same kind of event. The content is
 * laid out at its natural size and never moves; the body is given a number and
 * transitions between numbers.
 */
function useContentWidth(): [React.RefObject<HTMLDivElement | null>, number | undefined] {
  const ref = useRef<HTMLDivElement>(null);
  const [width, setWidth] = useState<number>();

  // Measured after every commit, because a render is what changes this
  // content: the clock gains a digit, the outcome replaces the clock. React
  // is the notifier, so the number is right in the same frame the text is.
  //
  // A ResizeObserver alone was not: it delivers on the rendering steps, and
  // when those are throttled the body keeps an older width and clips its own
  // status — seen, 92px of body around 99px of content.
  useLayoutEffect(() => {
    const el = ref.current;
    if (el) setWidth((current) => (current === el.offsetWidth ? current : el.offsetWidth));
  });

  // And an observer as well, for the widths React is not told about: a font
  // finishing its load, the appearance settings changing the code size.
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

  const body = (
    <>
      <StatusIcon status={status} />
      <span className="tabular-nums">{status.kind === "idle" ? "" : status.time}</span>
      {status.kind !== "idle" && status.kind !== "running" && (
        <span className="max-w-64 truncate">{status.message}</span>
      )}
    </>
  );

  return (
    <div className="relative flex shrink-0 items-center">
      <div
        className={cn(
          BODY,
          "transition-[width] duration-(--motion-base) ease-(--ease-out-strong) motion-reduce:transition-none",
          width === undefined && "w-max",
        )}
        style={width === undefined ? undefined : { width }}
      >
        {/* Pinned to the right and sized to its content, so narrowing the body
            clips the status off the LEFT and leaves Run where it was. Nothing
            in here moves; only the window onto it changes. */}
        <div ref={content} className="absolute top-0 right-0 flex h-full w-max items-center">
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
                className={cn(SEGMENT, toneOf(status), "cursor-pointer outline-none")}
              >
                {body}
              </button>
            ) : (
              <div role="status" className={cn(SEGMENT, toneOf(status))}>
                {body}
              </div>
            ))}
          <ActionTooltip label={runLabel} wrap>
            <Button
              variant={running ? "danger" : "default"}
              size="xs"
              // The body carries the radius and the focus ring; the segment
              // inside it carries neither, or they would be drawn twice and
              // the ring would be clipped by the body's own overflow.
              //
              // The height comes from `size` and NOT from h-full. ActionTooltip's
              // `wrap` puts a span between this button and the row, and that span
              // is content-sized: h-full resolved against 14px of icon and left
              // the button half the body's height, which is the notch that
              // appeared at the right end in dark mode.
              className="w-(--control-height-sm) shrink-0 gap-0 rounded-none p-0 duration-(--motion-slow) ease-in-out focus-visible:ring-0"
              onClick={running ? onStop : onRun}
              disabled={!canRun && !running}
            >
              {running ? <Square className="fill-current" /> : <Play className="fill-current" />}
            </Button>
          </ActionTooltip>
        </div>
      </div>

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
