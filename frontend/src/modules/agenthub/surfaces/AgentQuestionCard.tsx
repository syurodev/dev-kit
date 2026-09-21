import type * as React from "react";
import { useMemo, useRef, useState } from "react";

import { Button } from "@/components/ui/button";
import {
  Questionnaire,
  QuestionnaireActions,
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
import { PermissionIdentity } from "./permissionCard";

export type AgentQuestionCardProps = {
  pending: PendingElicitation;
  session?: AgentSession | null;
  onRespond: (response: ElicitationResponse) => void | Promise<void>;
  busy?: boolean;
  /** Why the last answer did not land, if it did not. */
  error?: string;
  /** "rail" is always one question at a time; "wide" shows up to two at once. */
  size?: "rail" | "wide";
  className?: string;
};

/** Two questions fit side by side in the reader's head; three is a form, and a form goes step by step. */
const ALL_AT_ONCE_MAX = 2;

/**
 * The row of Skip/Previous/Next/Submit buttons, pinned to the bottom of
 * whichever ancestor scrolls this card (both AgentHubContent and AgentRail
 * cap it at max-h-[60vh] and scroll). A long description otherwise fills
 * that box on its own, and the card reads as if it ended with no way to
 * answer — sticky plus the card's own background keeps the way out on
 * screen while the question above it scrolls underneath. The negative
 * margin/matching padding cancel out to the card's own edges (p-3 on rail,
 * px-3 py-3 on wide) so the bar's seam lines up with the card's border.
 */
const ACTIONS_BAR = "sticky bottom-0 z-10 -mx-3 -mb-3 border-t border-border-subtle bg-surface-raised px-3 pt-2 pb-3";

/**
 * Agent / Question — a question the agent is waiting on (AskUserQuestion, an
 * MCP server's form, the refusal-fallback prompt), asked in the conversation it
 * came out of, the way a permission is.
 *
 * Reads top-down like the permission card: who is asking (the same identity
 * block, without a clock — a question has no deadline), what they said, then
 * the questions. How the questions are laid out follows how many there are: up
 * to two are shown together with one Submit; three or more go one at a time
 * with Previous / Skip / Next, as does anything in the narrow rail.
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
  className,
}: AgentQuestionCardProps) {
  const form = useMemo(() => parseElicitationForm(pending.params, pending.message), [pending.params, pending.message]);
  const forms = useRef<(HTMLFormElement | null)[]>([]);
  const stepwise = size === "rail" || form.fields.length > ALL_AT_ONCE_MAX;
  const wide = size === "wide";

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

  return (
    <div
      role="group"
      aria-label="Question from the agent"
      data-testid="agent-question-card"
      data-size={size}
      data-layout={stepwise ? "stepwise" : "all"}
      data-flavor={form.flavor}
      className={cn(
        "flex w-full flex-col rounded-control border border-border-subtle bg-surface-raised",
        wide ? "gap-3 px-3 py-3" : "max-w-68 gap-3 p-3",
        className,
      )}
    >
      <PermissionIdentity pending={pending} session={session} env="open" testIdPrefix="question" />

      {form.message ? (
        <p data-testid="question-message" className="text-body-13 whitespace-pre-wrap text-text-primary">
          {form.message}
        </p>
      ) : null}

      {form.fields.length === 0 ? (
        <div className={cn(ACTIONS_BAR, "flex items-center justify-end gap-2")}>
          {skipAll("Skip")}
          <Button type="button" size="sm" disabled={busy} data-testid="question-submit" onClick={submit}>
            Continue
          </Button>
        </div>
      ) : stepwise ? (
        <Questionnaire
          key={pending.id}
          ref={(node) => {
            forms.current = [node];
          }}
          shortcuts="numbers"
          onSubmit={onFormSubmit}
          aria-label="Questions"
        >
          {form.fields.length > 1 ? <QuestionnaireProgress /> : null}
          {form.fields.map((field) => (
            <QuestionField key={field.key} field={field} form={form} />
          ))}
          {/* Sticky to the scroll container the card is shown in (rail and
              wide both cap it at max-h-[60vh] and scroll): a description long
              enough to fill that box otherwise pushes Previous/Skip/Next below
              the fold with nothing to say they exist — the card just looks
              like it ended without them. */}
          <div className={cn(ACTIONS_BAR, "flex flex-col gap-2")}>
            {/* With one question, skipping it and skipping them all are the
                same click; only the one that declines is kept. */}
            <QuestionnaireActions>
              <QuestionnairePrevious disabled={busy} />
              {form.fields.length > 1 ? (
                <QuestionnaireSkip disabled={busy} />
              ) : (
                <div className="col-start-2 row-start-1 justify-self-end">{skipAll("Skip")}</div>
              )}
              <QuestionnaireNext disabled={busy} />
              <QuestionnaireSubmit disabled={busy} data-testid="question-submit" />
            </QuestionnaireActions>
            {form.fields.length > 1 ? <div className="-mt-1 flex">{skipAll("Skip all")}</div> : null}
          </div>
        </Questionnaire>
      ) : (
        <>
          {form.fields.map((field, index) => (
            // One Root per question: the primitive shows only its active item,
            // and these are meant to be read together. Enter / ⌘Enter in any of
            // them submits the lot.
            <Questionnaire
              key={`${pending.id}:${field.key}`}
              ref={(node) => {
                forms.current[index] = node;
              }}
              shortcuts="numbers"
              onSubmit={onFormSubmit}
            >
              <QuestionField field={field} form={form} />
            </Questionnaire>
          ))}
          <div className={cn(ACTIONS_BAR, "flex items-center justify-end gap-2")}>
            {skipAll("Skip")}
            <Button type="button" size="sm" disabled={busy} data-testid="question-submit" onClick={submit}>
              Submit
            </Button>
          </div>
        </>
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
        <QuestionnaireChoices>
          {(field.kind === "boolean" ? BOOLEAN_CHOICES : field.choices).map((choice) => (
            <QuestionnaireChoice
              key={choice.value}
              value={choice.value}
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
        <pre className="max-h-40 max-w-full overflow-auto rounded-md border border-border-subtle bg-surface-base p-2 text-code-11 whitespace-pre-wrap wrap-anywhere text-text-secondary">
          {preview}
        </pre>
      ) : null}

      <QuestionnaireError />
    </QuestionnaireItem>
  );
}
