import { useMemo } from "react";

import { Button } from "@/components/ui/button";
import type { AgentSession, ElicitationResponse, PendingElicitation } from "@/bridge/agenthub";
import { declineElicitation } from "@/bridge/agenthub";
import { cn } from "@/lib/utils";
import { parseElicitationForm, questionCountLabel } from "../elicitation";
import { PermissionIdentity } from "./permissionCard";

export type AgentQuestionToastProps = {
  pending: PendingElicitation;
  session?: AgentSession | null;
  onRespond: (response: ElicitationResponse) => void | Promise<void>;
  /** Opens the conversation, where the inline card takes over. */
  onAnswer: () => void;
  busy?: boolean;
  /** How many other questions are waiting behind this one. */
  queued?: number;
  className?: string;
};

/**
 * Agent / Question Toast — says an agent is waiting on an answer while its
 * conversation is not on screen.
 *
 * Deliberately not the form. Four questions of four options do not fit a
 * 420px corner, and answering from the surface with the least context is the
 * mistake the permission toast already refuses to make with allow-always. So:
 * who is asking and what, how many questions, and two ways forward — Answer
 * opens the rail, where the card is, and Skip declines without opening
 * anything.
 *
 * No deadline bar: a question does not expire.
 */
export function AgentQuestionToast({
  pending,
  session,
  onRespond,
  onAnswer,
  busy = false,
  queued = 0,
  className,
}: AgentQuestionToastProps) {
  const form = useMemo(() => parseElicitationForm(pending.params, pending.message), [pending.params, pending.message]);

  return (
    <div
      role="alert"
      data-testid="agent-question-toast"
      className={cn("glass-float relative flex w-full max-w-105 flex-col gap-3 overflow-hidden rounded-float p-4", className)}
    >
      <PermissionIdentity
        pending={pending}
        session={session}
        env="open"
        testIdPrefix="question-toast"
        trailing={
          queued > 0 ? (
            <span
              data-testid="question-toast-queue"
              className="shrink-0 rounded-full bg-state-soft px-2 py-0.5 text-caption-11-medium whitespace-nowrap text-text-secondary"
            >
              {`+${queued} waiting`}
            </span>
          ) : undefined
        }
      />

      <div className="flex min-w-0 flex-col gap-0.5">
        {form.message ? <p className="line-clamp-3 text-body-13 text-text-primary">{form.message}</p> : null}
        <p data-testid="question-toast-count" className="text-caption-11 text-text-tertiary">
          {questionCountLabel(form)}
        </p>
      </div>

      <div className="flex w-full items-center justify-end gap-2">
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={busy}
          data-testid="question-toast-skip"
          onClick={() => void onRespond(declineElicitation())}
          className="border-border-subtle text-text-secondary"
        >
          Skip
        </Button>
        <Button type="button" size="sm" data-testid="question-toast-answer" onClick={onAnswer}>
          Answer
        </Button>
      </div>
    </div>
  );
}
