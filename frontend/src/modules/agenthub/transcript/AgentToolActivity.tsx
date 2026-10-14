import * as React from "react";
import { Wrench, X } from "lucide-react";

import { cn } from "@/lib/utils";
import { Shimmer } from "@/components/ui/shimmer";
import { Spinner } from "@/components/ui/spinner";
import { FramedCard, FramedCardAction, FramedCardHeader, FramedCardPreview } from "@/components/ui/framed-card";
import { getModule } from "@/modules/registry";
import { dispatchIntent } from "@/workbench/intents";
import { renderToolResult } from "@/workbench/toolResultViews";
import { toolResultInputOf } from "@/workbench/toolPayload";
import { toolActivityOf, type ToolActivity, type ToolActivityState, type ToolActivityTarget } from "@/workbench/toolActivity";
import type { ToolCall } from "../types";
import { toolActivityState } from "./AgentToolCall";

/** Whether a call is one of the DevKit tools that change something. */
export function isToolActivity(call: ToolCall | undefined): boolean {
  return Boolean(toolActivityOf(call?.name));
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
export const AgentToolActivity = React.memo(function AgentToolActivity({ call }: { call: ToolCall }) {
  const activity = toolActivityOf(call.name);
  const state = toolActivityState(call.status);
  // Fixed at mount: whether this card is news or history.
  const bornLive = React.useRef(state === "running").current;

  const target = React.useMemo(
    () =>
      activity
        ? activity.target({ name: call.name ?? "", rawInput: call.rawInput, rawOutput: call.rawOutput, state })
        : null,
    [activity, call.name, call.rawInput, call.rawOutput, state],
  );

  // The result card's code, fetched while the call is still running.
  React.useEffect(() => {
    if (state === "running") void activity?.preload?.().catch(() => undefined);
  }, [activity, state]);

  const result = React.useMemo(
    () =>
      state === "done" && activity
        ? (() => {
            // Wherever the vendor put the result (./toolPayload.ts); an error
            // has no input and keeps this card's own failed state.
            const input = toolResultInputOf(call, activity.done);
            return input ? renderToolResult(input) : null;
          })()
        : null,
    [activity, state, call],
  );

  if (!activity) return null;
  const card = <ActivityCard activity={activity} state={state} target={target} />;
  return (
    <div
      data-testid="tool-activity"
      data-state={state}
      data-tool={call.name}
      className={cn("w-full min-w-0", bornLive && "activity-enter")}
    >
      {result ? (
        <React.Suspense fallback={card}>
          {/* Keyed apart from the running card so the arrival is its own
              element: it fades in over the frame the running card held. */}
          <div key="result" className={cn("w-full min-w-0", bornLive && "activity-enter")}>
            {result}
          </div>
        </React.Suspense>
      ) : (
        card
      )}
    </div>
  );
});

function ActivityCard({
  activity,
  state,
  target,
}: {
  activity: ToolActivity;
  state: ToolActivityState;
  target: ToolActivityTarget | null;
}) {
  const Icon = getModule(activity.module)?.icon ?? Wrench;
  const ref = state === "done" ? target?.ref : undefined;
  const verb = state === "running" ? activity.running : state === "failed" ? activity.failed : activity.done;
  return (
    <FramedCard data-testid="tool-activity-card">
      <FramedCardHeader
        icon={Icon}
        title={
          state === "running" ? (
            <Shimmer active className="text-label-12">{`${verb}…`}</Shimmer>
          ) : (
            <span className={cn(state === "failed" && "text-status-danger")}>{verb}</span>
          )
        }
        meta={
          state === "running" ? (
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
