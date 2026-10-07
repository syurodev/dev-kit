import { AnimatedNumber, AnimatedNumberGroup } from "@/components/ui/animated-number";
import { Bot, Hourglass, Timer } from "lucide-react";
import { Progress } from "@/components/ui/progress";
import type { BoardSummary } from "../taskModel";

/**
 * The board at a glance, in its header: how much is done, and the three
 * things worth a look — work under way, work waiting on other work, and work
 * an agent holds. A count that is zero is left out rather than shown as "0",
 * so what is there is what needs attention.
 */
export function BoardSummaryBar({ summary }: { summary: BoardSummary }) {
  if (summary.total === 0) return null;
  const percent = Math.round((summary.done / summary.total) * 100);
  return (
    <AnimatedNumberGroup><div className="flex min-w-0 items-center gap-4 text-caption-11 text-text-tertiary">
      <div className="flex items-center gap-2" title={`${summary.done} of ${summary.total} tasks finished`}>
        <Progress value={percent} className="w-28" aria-label="Board progress" />
        <span className="text-text-secondary tabular-nums">
          <AnimatedNumber value={summary.done} />/<AnimatedNumber value={summary.total} />
        </span>
        <span className="tabular-nums"><AnimatedNumber value={percent} suffix="%" className="min-w-[4ch]" /></span>
      </div>
      {summary.active > 0 ? (
        <span className="inline-flex items-center gap-1 text-status-info">
          <Timer aria-hidden className="size-(--icon-12)" />
          <span className="tabular-nums"><AnimatedNumber value={summary.active} /></span> in progress
        </span>
      ) : null}
      {summary.blocked > 0 ? (
        <span className="inline-flex items-center gap-1 text-status-warning">
          <Hourglass aria-hidden className="size-(--icon-12)" />
          <span className="tabular-nums"><AnimatedNumber value={summary.blocked} /></span> waiting
        </span>
      ) : null}
      {summary.heldByAgent > 0 ? (
        <span className="inline-flex items-center gap-1 text-status-success">
          <Bot aria-hidden className="size-(--icon-12)" />
          <span className="tabular-nums"><AnimatedNumber value={summary.heldByAgent} /></span> with agents
        </span>
      ) : null}
    </div></AnimatedNumberGroup>
  );
}
