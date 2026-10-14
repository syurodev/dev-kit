import * as React from "react";
import { Wrench } from "lucide-react";

import { FramedCard, FramedCardHeader, FramedCardPreview } from "@/components/ui/framed-card";
import { Skeleton } from "@/components/ui/skeleton";
import { hasToolResultView, renderToolResult } from "@/workbench/toolResultViews";
import { toolNameOf, toolPayloadOf, toolResultInputOf } from "@/workbench/toolPayload";
import type { ToolCall, TranscriptItem } from "../types";
import { AgentToolCall } from "./AgentToolCall";
import { isToolActivity } from "./AgentToolActivity";
import { resultCallOf, useFullToolCall } from "./toolCallDetail";

/**
 * Results outside the fold (spec 2026-10-13-agent-hub-wide-layout-design §7):
 * a call whose owning module draws its result — a query's rows, a plan, a
 * message — is what the person asked for, not the account of getting it, so
 * the card stands after the run's fold while the fold keeps the row.
 */

/** At most this many results stand outside the fold in one turn; the rest are counted. */
export const SURFACED_RESULTS_PER_TURN = 3;

/**
 * What a call offers as a result card: the view's node, ready to draw, or
 * `pending` — a cut preview the view cannot read, waiting on the full row.
 * Null is no card: not completed, an activity (it has its own card), no view,
 * an error, nothing to read, or a view that declined what it was given.
 */
export type ResultCandidate = { kind: "ready"; node: React.ReactNode } | { kind: "pending" } | null;

// Per call object: the transcript hands the same object back until the call
// changes, so a publish costs a lookup, not a parse and a render of the view.
// It answers against the view registry as it stood on the first read: a view
// registered later applies to calls not yet seen, not to these. Views are
// registered by module manifests at start-up, before any transcript draws.
const candidates = new WeakMap<ToolCall, ResultCandidate>();

export function resultCandidate(call: ToolCall | undefined): ResultCandidate {
  if (!call) return null;
  const known = candidates.get(call);
  if (known !== undefined) return known;
  const candidate = readCandidate(call);
  candidates.set(call, candidate);
  return candidate;
}

function readCandidate(call: ToolCall): ResultCandidate {
  // Completed exactly: a missing or unknown status is not a result yet.
  if (call.status?.toLowerCase() !== "completed") return null;
  if (!hasToolResultView(toolNameOf(call)) || isToolActivity(call)) return null;
  const input = toolResultInputOf(call);
  if (input) {
    // Views check the shape synchronously before their chunk loads, so a
    // node here is a card that will draw, and null is one that would not.
    const node = renderToolResult(input);
    return node === null || node === undefined ? null : { kind: "ready", node };
  }
  return call.truncated && toolPayloadOf(call) === null ? { kind: "pending" } : null;
}

/** One run of steps' share: the results it draws, and those left in its fold. */
export type StepResults = { shown: readonly TranscriptItem[]; hidden: readonly TranscriptItem[] };

export const NO_RESULTS: StepResults = { shown: [], hidden: [] };

/**
 * Splits a turn's results between its runs of steps, in call order, `cap` in
 * all. A lone step that has a result is that card, so it spends a slot too;
 * over the cap it is simply its row, with no fold to point into.
 *
 * Call order, not finishing order: when parallel calls land out of order, an
 * earlier call finishing later takes its slot back, and a card already on
 * screen can move into "+N more". Accepted — the order stays the one the
 * agent made the calls in, which is how the fold lists them too.
 */
export function surfaceResults(
  runs: readonly (readonly TranscriptItem[])[],
  cap = SURFACED_RESULTS_PER_TURN,
): StepResults[] {
  let budget = cap;
  return runs.map((items) => {
    const found = items.filter((item) => {
      const candidate = resultCandidate(item.toolCall);
      // A pending card needs a session to load the full row from.
      return candidate !== null && (candidate.kind === "ready" || Boolean(item.sessionId));
    });
    if (found.length === 0) return NO_RESULTS;
    const take = Math.min(budget, found.length);
    budget -= take;
    return { shown: found.slice(0, take), hidden: items.length === 1 ? NO_RESULTS.hidden : found.slice(take) };
  });
}

/**
 * Just the result of a call, as its module draws it. A pending one loads the
 * full row (shared with the row's "Show full output") behind a skeleton the
 * size of a card's header and line, in the same place the card will be.
 *
 * When the view turns the full row down, or it cannot be loaded, there is no
 * card: `asRow` draws the call's row instead (a lone step has no fold holding
 * it), otherwise nothing — the fold has the row. Such a card still holds its
 * slot in the turn's count: which calls get a slot is decided before the full
 * row is read, and re-deciding after a fetch would move the other cards.
 */
export const ToolResultCard = React.memo(function ToolResultCard({
  call,
  sessionId,
  asRow = false,
  onOpenFile,
}: {
  call: ToolCall;
  sessionId?: string;
  asRow?: boolean;
  onOpenFile?: (path: string, line?: number) => void;
}) {
  const candidate = resultCandidate(call);
  const pending = candidate?.kind === "pending";
  const { fullCall, failed } = useFullToolCall(call, sessionId, pending);
  const loaded = React.useMemo(() => {
    if (!pending || !fullCall) return null;
    const input = toolResultInputOf(resultCallOf(call, fullCall));
    return input ? (renderToolResult(input) ?? null) : null;
  }, [pending, call, fullCall]);

  const declined = candidate === null || (pending && (failed || (fullCall !== null && loaded === null)));
  if (declined) {
    return asRow ? <AgentToolCall variant="row" call={call} sessionId={sessionId} onOpenFile={onOpenFile} /> : null;
  }
  const skeleton = <ResultSkeleton call={call} />;
  const node = candidate.kind === "ready" ? candidate.node : loaded;
  return (
    <div data-testid="turn-result-card" className="w-full min-w-0">
      {/* The view may load lazily (registerToolResultView): its skeleton holds the place. */}
      <React.Suspense fallback={skeleton}>{node ?? skeleton}</React.Suspense>
    </div>
  );
});

/** A card's header and one line, while the result it will show loads. */
function ResultSkeleton({ call }: { call: ToolCall }) {
  return (
    <FramedCard data-testid="turn-result-loading" role="status" aria-label="Loading result">
      <FramedCardHeader icon={Wrench} title={call.title || toolNameOf(call) || "Result"} />
      <FramedCardPreview>
        <Skeleton className="h-3 w-2/3" />
      </FramedCardPreview>
    </FramedCard>
  );
}
