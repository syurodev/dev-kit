import type * as React from "react";
import { useMemo, useRef, useState } from "react";
import { ArrowLeft, ArrowRight, ChevronDown, CircleHelp } from "lucide-react";

import { CodeBlock } from "@/components/code/CodeBlock";
import { detectLanguage } from "@/components/code/languages";
import { ActionTooltip } from "@/components/ui/action-tooltip";
import { Button } from "@/components/ui/button";
import {
  Questionnaire,
  QuestionnaireChoice,
  QuestionnaireChoiceDescription,
  QuestionnaireChoices,
  QuestionnaireError,
  QuestionnaireInput,
  QuestionnaireItem,
  QuestionnaireNext,
  QuestionnairePrevious,
  QuestionnaireProgress,
  QuestionnaireSkip,
  QuestionnaireSubmit,
  QuestionnaireTitle,
} from "@/components/ui/questionnaire";
import type { AgentSession, ElicitationResponse, PendingElicitation } from "@/bridge/agenthub";
import { acceptElicitation, declineElicitation } from "@/bridge/agenthub";
import { cn } from "@/lib/utils";
import {
  BOOLEAN_CHOICES,
  answersFromForms,
  buildElicitationContent,
  parseElicitationForm,
  type ElicitationField,
  type ElicitationForm,
} from "../elicitation";

export type AgentQuestionCardProps = {
  pending: PendingElicitation;
  session?: AgentSession | null;
  onRespond: (response: ElicitationResponse) => void | Promise<void>;
  busy?: boolean;
  /** Why the last answer did not land, if it did not. */
  error?: string;
  /** Where it is shown. Both are one question at a time; only the attribute differs. */
  size?: "rail" | "wide";
  /**
   * Folds the card down into the composer tray's strip so the transcript can
   * be read. The agent is still waiting — folding is not an answer.
   */
  onMinimize?: () => void;
  className?: string;
};

/**
 * The step controls, pinned to the bottom of whichever ancestor scrolls this
 * card (the tray caps its panel and scrolls): a long description otherwise
 * pushes them below the fold with nothing to say they exist. The negative
 * margins cancel the card's p-3 so the bar spans it edge to edge, on the
 * tray's own surface.
 */
const ACTIONS_BAR = "sticky bottom-0 z-10 -mx-3 -mb-3 flex items-center gap-1 bg-surface-raised px-3 pt-1 pb-3";

/** A choice as a compact row — the tray has little height to give, and a boxed row per choice spent most of it on borders. */
const CHOICE = "min-h-8 border-transparent bg-transparent px-2 py-1.5 data-checked:border-transparent";

/**
 * Agent / Question — a question the agent is waiting on (AskUserQuestion, an
 * MCP server's form, the refusal-fallback prompt), asked in the composer tray
 * of the conversation it came out of, the way a permission is.
 *
 * One question at a time, always: showing two together read as one form with
 * two answers, and a stack of them left no transcript to read the reason in.
 * The top row is what the agent said and where the steps are (2/3); then the
 * question and its choices as plain rows; then back, skip and next. Who is
 * asking is in the icon's tooltip — in a session's own tray it is that session,
 * and the row it took was the tray's to give.
 *
 * There is always a way out that does not require an answer. Skip all declines
 * — the tool runs on with no answers and the turn continues — and a field this
 * build cannot render says so and stays skippable. Nothing on the agent's side
 * will time the question out, so a card that could not be dismissed would be a
 * turn that never ends.
 */
export function AgentQuestionCard({
  pending,
  session,
  onRespond,
  busy = false,
  error,
  size = "wide",
  onMinimize,
  className,
}: AgentQuestionCardProps) {
  const form = useMemo(() => parseElicitationForm(pending.params, pending.message), [pending.params, pending.message]);
  const forms = useRef<(HTMLFormElement | null)[]>([]);
  const agentTitle = session?.title || pending.title || pending.sessionId.slice(0, 8);
  const depth = pending.depth ?? session?.depth ?? 0;
  const askedBy = `Asked by ${agentTitle}${depth > 0 ? ` · subagent, depth ${depth}` : ""}`;
  const many = form.fields.length > 1;

  function submit() {
    if (busy) return;
    const content = buildElicitationContent(form, answersFromForms(forms.current));
    void onRespond(acceptElicitation(content));
  }

  function onFormSubmit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    submit();
  }

  const skipAll = (label: string) => (
    <Button
      type="button"
      variant="ghost"
      size="sm"
      disabled={busy}
      data-testid="question-skip-all"
      onClick={() => void onRespond(declineElicitation())}
      className="text-text-tertiary hover:text-text-primary"
    >
      {label}
    </Button>
  );

  const header = (progress: React.ReactNode) => (
    <div className="flex min-w-0 items-start gap-2">
      <span title={askedBy} className="flex h-5 shrink-0 items-center text-icon-secondary">
        <CircleHelp className="size-4" aria-label={askedBy} />
      </span>
      <p data-testid="question-message" className="line-clamp-3 min-w-0 flex-1 text-body-13 whitespace-pre-wrap text-text-secondary">
        {form.message || `${agentTitle} is asking`}
      </p>
      {progress}
      {onMinimize ? (
        <ActionTooltip label="Minimize — read the conversation">
          <Button
            type="button"
            variant="ghost"
            size="icon-xs"
            aria-label="Minimize question"
            data-testid="question-minimize"
            onClick={onMinimize}
            className="shrink-0 text-text-tertiary hover:text-text-primary"
          >
            <ChevronDown />
          </Button>
        </ActionTooltip>
      ) : null}
    </div>
  );

  return (
    <div
      role="group"
      aria-label="Question from the agent"
      data-testid="agent-question-card"
      data-size={size}
      data-layout="stepwise"
      data-flavor={form.flavor}
      className={cn("flex w-full min-w-0 flex-col gap-2 p-3", className)}
    >
      {form.fields.length === 0 ? (
        <>
          {header(null)}
          <div className={cn(ACTIONS_BAR, "justify-end")}>
            {skipAll("Skip")}
            <Button type="button" size="sm" disabled={busy} data-testid="question-submit" onClick={submit}>
              Continue
            </Button>
          </div>
        </>
      ) : (
        <Questionnaire
          key={pending.id}
          ref={(node) => {
            forms.current = [node];
          }}
          shortcuts="numbers"
          onSubmit={onFormSubmit}
          aria-label="Questions"
          className="gap-2"
        >
          {header(many ? <QuestionnaireProgress className="h-5 shrink-0 leading-5" /> : null)}
          {form.fields.map((field) => (
            <QuestionField key={field.key} field={field} form={form} />
          ))}
          <div data-testid="question-actions" className={ACTIONS_BAR}>
            {/* With one question, skipping it and skipping them all are the
                same click; only the one that declines is kept. */}
            {skipAll(many ? "Skip all" : "Skip")}
            <span className="flex-1" />
            <QuestionnairePrevious
              disabled={busy}
              size="icon-sm"
              aria-label="Previous question"
              className="col-start-auto row-start-auto justify-self-auto"
            >
              <ArrowLeft />
            </QuestionnairePrevious>
            {many ? (
              <QuestionnaireSkip disabled={busy} className="col-start-auto row-start-auto justify-self-auto text-text-tertiary">
                Skip
              </QuestionnaireSkip>
            ) : null}
            <QuestionnaireNext
              disabled={busy}
              size="icon-sm"
              aria-label="Next question"
              className="col-start-auto row-start-auto justify-self-auto rounded-full"
            >
              <ArrowRight />
            </QuestionnaireNext>
            <QuestionnaireSubmit
              disabled={busy}
              data-testid="question-submit"
              className="col-start-auto row-start-auto justify-self-auto"
            />
          </div>
        </Questionnaire>
      )}

      {error ? (
        <p role="alert" className="text-caption-11 text-status-danger">
          {error}
        </p>
      ) : null}
    </div>
  );
}

function QuestionField({ field, form }: { field: ElicitationField; form: ElicitationForm }) {
  const [preview, setPreview] = useState<string | undefined>(undefined);
  // A lone field whose only name is its key ("choice") is asked by the message
  // above it; a legend repeating the key would be noise.
  const showTitle = !(form.fields.length === 1 && field.label === field.key);
  // An unrenderable field is never required: required would hide Skip, and
  // there would be no way to answer it either.
  const required = field.kind !== "unsupported" && field.required;

  return (
    <QuestionnaireItem
      name={field.key}
      multiple={field.kind === "multi"}
      required={required}
      data-testid="question-field"
      data-kind={field.kind}
    >
      {showTitle ? (
        <QuestionnaireTitle>
          {field.header ? (
            <span className="mr-2 rounded-full bg-state-soft px-2 py-0.5 align-middle text-caption-11-medium text-text-secondary">
              {field.header}
            </span>
          ) : null}
          {field.label}
        </QuestionnaireTitle>
      ) : null}

      {field.kind === "single" || field.kind === "multi" || field.kind === "boolean" ? (
        <QuestionnaireChoices className="gap-0.5">
          {(field.kind === "boolean" ? BOOLEAN_CHOICES : field.choices).map((choice) => (
            <QuestionnaireChoice
              key={choice.value}
              value={choice.value}
              className={CHOICE}
              onChange={(event) => {
                if (event.target.checked) setPreview(choice.preview);
                else if (field.kind === "multi" && preview === choice.preview) setPreview(undefined);
              }}
            >
              {choice.label}
              {choice.description ? <QuestionnaireChoiceDescription>{choice.description}</QuestionnaireChoiceDescription> : null}
            </QuestionnaireChoice>
          ))}
          {field.kind !== "boolean" && field.customKey ? (
            <QuestionnaireInput
              placeholder="Other…"
              aria-label="Other answer"
              data-testid="question-other"
              onChange={() => setPreview(undefined)}
            />
          ) : null}
        </QuestionnaireChoices>
      ) : field.kind === "text" ? (
        <QuestionnaireInput type={field.inputType} aria-label={field.label} />
      ) : field.kind === "number" ? (
        <QuestionnaireInput type="number" step={field.integer ? 1 : "any"} inputMode="decimal" aria-label={field.label} />
      ) : (
        <p data-testid="question-unsupported" className="text-caption-11 text-text-tertiary">
          DevKit can’t show this field. Skip it and answer the rest.
        </p>
      )}

      {preview ? (
        <CodeBlock
          code={preview}
          language={detectLanguage(preview)}
          className="max-h-40 max-w-full overflow-auto rounded-md border border-border-subtle bg-surface-base p-2 text-code-11 whitespace-pre-wrap wrap-anywhere text-text-secondary"
        />
      ) : null}

      <QuestionnaireError />
    </QuestionnaireItem>
  );
}
