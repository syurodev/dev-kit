import * as React from "react";
import { Wrench, X } from "lucide-react";

import { cn } from "@/lib/utils";
import { Shimmer } from "@/components/ui/shimmer";
import { Spinner } from "@/components/ui/spinner";
import { ErrorBoundary } from "@/components/ui/error-boundary";
import { FramedCard, FramedCardAction, FramedCardHeader, FramedCardPreview } from "@/components/ui/framed-card";
import { getModule } from "@/modules/registry";
import { dispatchIntent } from "@/workbench/intents";
import { hasToolResultView, renderToolResult } from "@/workbench/toolResultViews";
import { successPayloadOf, toolNameOf, toolPayloadOf, toolResultInputOf } from "@/workbench/toolPayload";
import { toolActivityOf, type ToolActivity, type ToolActivityState, type ToolActivityTarget } from "@/workbench/toolActivity";
import type { ToolCall } from "../types";
import { toolActivityState } from "./AgentToolCall";
import { resultCallOf, useFullToolCall } from "./toolCallDetail";
import { useInEarlierTurn } from "./toolSettle";

/** Whether a call is one of the DevKit tools that change something. */
export function isToolActivity(call: ToolCall | undefined): boolean {
  // Codex names no call; its title is `devkit.<tool>` (toolNameOf).
  return Boolean(call && toolActivityOf(toolNameOf(call)));
}

/**
 * What an activity acts on, read from the call's input while it runs and from
 * its answer — wherever the vendor put it (workbench/toolPayload.ts) — once
 * done. Shared with the follow panel, so both name the same thing.
 */
export function activityTargetOf(activity: ToolActivity, call: ToolCall, state: ToolActivityState): ToolActivityTarget | null {
  return activity.target({
    name: toolNameOf(call),
    rawInput: call.rawInput,
    rawOutput: state === "done" ? successPayloadOf(call) : undefined,
    state,
  });
}

/** The intent that opens a target: `<module>.reveal`, but API client's is `api.`. */
function revealIntent(module: string): string {
  return module === "apiclient" ? "api.reveal" : `${module}.reveal`;
}

/**
 * One DevKit tool that changes something — a board created, a task moved, a
 * commit, a push — drawn as a card that stays on screen while the run's other
 * steps fold away (AgentTurn's StepGroup).
 *
 * While the call runs it says what is happening and to what, from the call's
 * own input ("Creating board · Backend"); when it lands, the owning module's
 * result card takes its place (registerToolResultView) and falls back to this
 * card with an Open when the module has none. Both are the same FramedCard, a
 * header over one line, so the swap does not move what is below it; and the
 * result card's chunk is warmed while the call runs, with this card as the
 * Suspense fallback, so there is no empty frame between the two.
 *
 * Motion only for a card born while the agent worked: a loaded history — and
 * the transcript is virtualised, so a row remounts on every scroll — renders
 * standing still.
 */
export const AgentToolActivity = React.memo(function AgentToolActivity({
  call,
  sessionId,
}: {
  call: ToolCall;
  /** Whose transcript this is: a cut answer is read from its full row. */
  sessionId?: string;
}) {
  const name = toolNameOf(call);
  const activity = toolActivityOf(name);
  const state = toolActivityState(call.status);
  // Still pending or in progress in the stored transcript, but its turn is
  // over (toolSettle.ts): the card says so instead of spinning.
  const earlierTurn = useInEarlierTurn();
  const stopped = state === "running" && earlierTurn;
  // Fixed at mount: whether this card is news or history.
  const bornLive = React.useRef(state === "running" && !stopped).current;

  // A done call's answer on the page may be a cut preview: the full row is
  // read then, shared with the call's row and any result card.
  const previewCut = React.useMemo(
    () => state === "done" && Boolean(activity) && Boolean(call.truncated) && toolPayloadOf(call) === null,
    [state, activity, call],
  );
  const { fullCall } = useFullToolCall(call, sessionId, previewCut);
  const resultCall = React.useMemo(() => resultCallOf(call, fullCall), [call, fullCall]);

  const target = React.useMemo(
    () => (activity ? activityTargetOf(activity, resultCall, state) : null),
    [activity, resultCall, state],
  );

  // The result card's code, fetched while the call is still running.
  React.useEffect(() => {
    if (state === "running" && !stopped) void activity?.preload?.().catch(() => undefined);
  }, [activity, state, stopped]);

  const result = React.useMemo(() => {
    // Only a tool with a view is worth reading the answer for.
    if (state !== "done" || !activity || !hasToolResultView(name)) return null;
    // Wherever the vendor put the result (workbench/toolPayload.ts); an error
    // has no input and keeps this card's own failed state.
    const input = toolResultInputOf(resultCall, activity.done);
    return input ? renderToolResult(input) : null;
  }, [activity, state, name, resultCall]);

  if (!activity) return null;
  const card = <ActivityCard activity={activity} state={state} stopped={stopped} target={target} />;
  return (
    <div
      data-testid="tool-activity"
      data-state={stopped ? "unfinished" : state}
      data-tool={name}
      className={cn("w-full min-w-0", bornLive && "activity-enter")}
    >
      {result ? (
        // A result card that throws, or whose chunk is gone after an update,
        // leaves this card standing instead of taking the page with it.
        <ErrorBoundary label={`${name || "tool"} result card`} fallback={card} resetKey={call}>
          <React.Suspense fallback={card}>
            {/* Keyed apart from the running card so the arrival is its own
                element: it fades in over the frame the running card held. */}
            <div key="result" className={cn("w-full min-w-0", bornLive && "activity-enter")}>
              {result}
            </div>
          </React.Suspense>
        </ErrorBoundary>
      ) : (
        card
      )}
    </div>
  );
});

function ActivityCard({
  activity,
  state,
  stopped = false,
  target,
}: {
  activity: ToolActivity;
  state: ToolActivityState;
  /** Running in the transcript, but nothing is left to finish it. */
  stopped?: boolean;
  target: ToolActivityTarget | null;
}) {
  const Icon = getModule(activity.module)?.icon ?? Wrench;
  const ref = state === "done" ? target?.ref : undefined;
  const verb = state === "running" ? activity.running : state === "failed" ? activity.failed : activity.done;
  const live = state === "running" && !stopped;
  return (
    <FramedCard data-testid="tool-activity-card">
      <FramedCardHeader
        icon={Icon}
        title={
          live ? (
            <Shimmer active className="text-label-12">{`${verb}…`}</Shimmer>
          ) : stopped ? (
            // Not "Committing" beside "didn't finish": the verb would claim it is under way.
            <span className="text-text-secondary">Didn't finish</span>
          ) : (
            <span className={cn(state === "failed" && "text-status-danger")}>{verb}</span>
          )
        }
        meta={
          live ? (
            <Spinner className="size-3 text-text-tertiary" aria-label={verb} />
          ) : state === "failed" ? (
            <X className="size-3 text-status-danger" aria-hidden="true" />
          ) : (
            target?.detail
          )
        }
        action={
          ref ? (
            <FramedCardAction onClick={() => void dispatchIntent(revealIntent(ref.module), ref, { placement: "beside" })}>
              Open
            </FramedCardAction>
          ) : undefined
        }
      />
      {target?.label ? (
        <FramedCardPreview className="flex-row items-baseline gap-(--space-inline)">
          {state !== "done" && target.detail ? (
            <span className="shrink-0 text-code-12-medium text-text-secondary">{target.detail}</span>
          ) : null}
          <span className="min-w-0 truncate text-body-13 text-text-primary" title={target.label}>
            {target.label}
          </span>
        </FramedCardPreview>
      ) : null}
    </FramedCard>
  );
}
