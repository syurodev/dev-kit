import type * as React from "react";
import { cn } from "@/lib/utils";
import type { DiffPair, DiffRow, DiffRun, SourcedDiffLine } from "@/lib/diffHunks";
import type { SyntaxSegment } from "@/components/code/highlight";

/**
 * 08 Diff — Figma `408:3`. One diff language for Notes version history, Sync
 * resolve, the Git module, and an agent's edit waiting for permission.
 *
 * Diff / Line: Kind = Context / Added / Removed, the tint full-bleed across
 * the pane so a run of changed lines reads as one block. Additions are
 * --brand-text and removals --status-danger, both at 10%: lime, not green,
 * because a diff has no opinion about which change is good (see
 * AgentToolDiff).
 */
/**
 * Row height in the two-pane view. Fixed, because the ribbon between the panes
 * is drawn from row indices: a row that grew by a pixel would slide the band
 * off the lines it belongs to.
 */
export const DIFF_ROW_HEIGHT = 24;

export function DiffLineRow({
  row,
  segments,
  fixed = false,
  lineNumber,
}: {
  row: DiffRow;
  segments?: SyntaxSegment[];
  /** Locks the row to DIFF_ROW_HEIGHT; see the constant. */
  fixed?: boolean;
  /** The line's number in the file it came from. Omitted draws no gutter. */
  lineNumber?: number;
}) {
  const gutter =
    lineNumber === undefined ? null : (
      // Out of the selection: copying a diff should give code, not code with a
      // column of numbers down its left edge.
      <span
        aria-hidden="true"
        data-slot="diff-line-number"
        className="mr-3 min-w-8 shrink-0 text-right text-code-11 tabular-nums text-text-tertiary/70 select-none"
      >
        {lineNumber}
      </span>
    );

  if (row.kind === "gap") {
    return (
      <div
        data-kind="gap"
        className={cn(
          "flex min-w-max items-center pr-3 pl-3 text-code-12 text-text-tertiary select-none",
          fixed ? "h-6 py-0" : "py-0.75",
        )}
      >
        {lineNumber === undefined ? null : <span aria-hidden="true" className="mr-3 min-w-8 shrink-0" />}
        {`⋯ ${row.count} unchanged line${row.count === 1 ? "" : "s"}`}
      </div>
    );
  }
  return (
    <div
      data-kind={row.kind}
      className={cn(
        "flex min-w-max items-center pr-3 pl-3 text-code-12",
        fixed ? "h-6 py-0" : "py-0.75",
        row.kind === "added" && "bg-(--brand-text)/10",
        row.kind === "removed" && "bg-status-danger/10",
        row.kind === "added" && !segments?.length && "text-(--brand-text)",
        row.kind === "removed" && !segments?.length && "text-status-danger",
        row.kind === "same" && "text-text-secondary",
      )}
    >
      {gutter}
      <span
        aria-hidden="true"
        className={cn(
          "shrink-0 whitespace-pre select-none",
          row.kind === "same" && "text-text-tertiary",
          row.kind === "added" && "text-(--brand-text)",
          row.kind === "removed" && "text-status-danger",
        )}
      >
        {row.kind === "added" ? "+ " : row.kind === "removed" ? "- " : "  "}
      </span>
      {/* Syntax colours win over the row's tone where they exist: the tint
          behind the line already says added or removed, and code reads as code.
          The prefix keeps the tone, so a glance down the gutter still sorts
          them. */}
      <span className="whitespace-pre">
        {segments && segments.length > 0
          ? segments.map((segment, i) => (
              <span key={i} className={segment.className || undefined}>
                {segment.text}
              </span>
            ))
          : row.text}
      </span>
    </div>
  );
}

/**
 * Diff / Pane: a header label over lines. Emphasis Active is the side being
 * decided on — focus-blue border and label.
 */
export function DiffPane({
  label,
  meta,
  emphasis = "default",
  caps = true,
  children,
  className,
  bodyClassName,
}: {
  label: React.ReactNode;
  /** Right of the label: counts, a "new file" note. */
  meta?: React.ReactNode;
  emphasis?: "default" | "active";
  /** Off for a file path, which is case-sensitive and must read as typed. */
  caps?: boolean;
  children: React.ReactNode;
  className?: string;
  /** The scrolling area — cap its height here. */
  bodyClassName?: string;
}) {
  const active = emphasis === "active";
  return (
    <div
      data-slot="diff-pane"
      data-emphasis={emphasis}
      className={cn(
        "flex w-full min-w-0 flex-col overflow-clip rounded-lg bg-(--tint-veil)/3 pb-3",
        active ? "border-(length:--stroke-1-5) border-border-focus" : "border border-border-subtle",
        className,
      )}
    >
      <div className="flex w-full min-w-0 items-center gap-2 px-4 pt-3 pb-2.5">
        <span
          className={cn(
            "min-w-0 flex-1 truncate text-code-11 font-medium",
            caps && "tracking-(--type-tracking-caps) uppercase",
            active ? "text-status-info" : "text-text-tertiary",
          )}
        >
          {label}
        </span>
        {meta}
      </div>
      <div className={cn("w-full overflow-auto select-text", bodyClassName)}>{children}</div>
    </div>
  );
}


/** An empty row opposite a line the other side does not have. */
function DiffBlankRow() {
  return <div aria-hidden="true" data-kind="blank" className="h-6 bg-(--text-tertiary)/4" />;
}

/**
 * The gutter's chunk ribbon — Figma 08 Diff, part C: "a generated recipe, never
 * a stretched component". A band per changed run, flush at both pane edges,
 * tinted exactly as the rows it joins so the three read as one shape: the
 * removed side's danger at 10% running into the added side's brand at 10%.
 */
function DiffRibbon({ runs, rows }: { runs: DiffRun[]; rows: number }) {
  const height = rows * DIFF_ROW_HEIGHT;
  return (
    <svg
      aria-hidden="true"
      data-slot="diff-ribbon"
      width="100%"
      height={height}
      viewBox={`0 0 100 ${height}`}
      preserveAspectRatio="none"
      className="block shrink-0"
    >
      <defs>
        <linearGradient id="diff-ribbon-modified" x1="0" x2="1" y1="0" y2="0">
          <stop offset="0%" stopColor="var(--status-danger)" stopOpacity="0.1" />
          <stop offset="100%" stopColor="var(--brand-text)" stopOpacity="0.1" />
        </linearGradient>
      </defs>
      {runs.map((run) => {
        const y = run.start * DIFF_ROW_HEIGHT;
        const h = (run.end - run.start) * DIFF_ROW_HEIGHT;
        const fill =
          run.kind === "modified"
            ? "url(#diff-ribbon-modified)"
            : run.kind === "added"
              ? "var(--brand-text)"
              : "var(--status-danger)";
        return (
          <rect
            key={`${run.start}-${run.end}`}
            x="0"
            y={y}
            width="100"
            height={h}
            fill={fill}
            fillOpacity={run.kind === "modified" ? 1 : 0.1}
          />
        );
      })}
    </svg>
  );
}

/**
 * Two panes and the gutter between them — Figma 08 Diff, example D: the file
 * as it is beside what it becomes, changed lines facing each other, and a
 * ribbon joining each changed run across the gap.
 *
 * One scroller holds both panes, so the two sides cannot drift apart: they are
 * laid out row for row (splitPairs) and a single scrollbar moves them
 * together.
 */
export function DiffSplitPanes({
  pairs,
  runs,
  leftLabel,
  rightLabel,
  leftMeta,
  rightMeta,
  segmentsFor,
  className,
}: {
  pairs: DiffPair[];
  runs: DiffRun[];
  leftLabel: React.ReactNode;
  rightLabel: React.ReactNode;
  leftMeta?: React.ReactNode;
  rightMeta?: React.ReactNode;
  /** A line's syntax runs; the side decides which file's colours it takes. */
  segmentsFor?: (line: SourcedDiffLine, side: "left" | "right") => SyntaxSegment[] | undefined;
  className?: string;
}) {
  const side = (get: (pair: DiffPair) => SourcedDiffLine | null, which: "left" | "right") =>
    pairs.map((pair, i) => {
      const line = get(pair);
      return line ? (
        <DiffLineRow
          key={i}
          row={line}
          segments={segmentsFor?.(line, which)}
          lineNumber={(which === "left" ? line.before : line.after) + 1}
          fixed
        />
      ) : (
        <DiffBlankRow key={i} />
      );
    });

  return (
    <div data-slot="diff-split" className={cn("flex w-full min-w-0 items-stretch gap-0", className)}>
      <DiffPane label={leftLabel} meta={leftMeta} caps={false} className="min-w-0 flex-1" bodyClassName="overflow-visible">
        {side((pair) => pair.left, "left")}
      </DiffPane>
      {/* The transparent border matches the panes' own, so the band starts on
          the same pixel as their first row. */}
      <div className="flex w-10 shrink-0 flex-col border border-transparent">
        {/* A copy of the pane header, invisible: the band has to start where
            the rows do, and the header's own box is the only thing that knows
            how tall that is. */}
        <div aria-hidden="true" className="invisible px-4 pt-3 pb-2.5 text-code-11 font-medium">
          &nbsp;
        </div>
        <DiffRibbon runs={runs} rows={pairs.length} />
      </div>
      <DiffPane
        label={rightLabel}
        meta={rightMeta}
        caps={false}
        emphasis="active"
        className="min-w-0 flex-1"
        bodyClassName="overflow-visible"
      >
        {side((pair) => pair.right, "right")}
      </DiffPane>
    </div>
  );
}
