import * as React from "react"
import { Questionnaire as QuestionnairePrimitive } from "@shadcn/react/questionnaire"
import { CheckIcon } from "lucide-react"

import { buttonVariants, type Button } from "@/components/ui/button"
import { cn } from "@/lib/utils"

/**
 * Questionnaire — a form of questions, one at a time, answered by click or by
 * shortcut key (shadcn's headless primitive, dressed in DevKit's tokens).
 *
 * The primitive shows only the active item: the rest are `hidden` and `inert`,
 * and Previous / Next / Skip / Submit hide themselves when they do not apply.
 * A caller that wants several questions on screen at once renders one Root per
 * question rather than fighting that.
 *
 * Vocabulary is the app's, not shadcn's: the form surface is Input's recipe
 * (white/6 on a status-info/16 hairline, a 1.5px info border on focus), a
 * selected choice is the state layer with an info border, the indicator is the
 * Checkbox's action-primary fill. No `dark:` variants — the ten palettes flip
 * through the CSS variables, not a class.
 */

/**
 * A choice row: the whole row is the hit target (the real input covers it).
 *
 * Grid, not flex: the indicator (col 1) and shortcut badge (col 3) only ever
 * occupy the row's first line, but a flex row would reserve their width on
 * every wrapped line of the label — the description would be squeezed to a
 * column dozens of pixels narrower than the row for its whole height, wrapping
 * early and leaving the rest of the row empty beside it. Grid rows let the
 * description (row 2) span columns 2 and 3, reclaiming the width the shortcut
 * only uses on row 1.
 */
export const QUESTIONNAIRE_CHOICE = cn(
  "group/questionnaire-choice relative grid min-h-(--control-height-lg) min-w-0 cursor-pointer grid-cols-auto-fr-auto items-start gap-x-2.5 gap-y-0.5 rounded-md border border-border-subtle bg-surface-well px-3 py-2 text-start text-body-13 text-text-primary transition-control outline-none select-none",
  "hover:bg-state-soft",
  "has-[>input:focus-visible]:ring-2 has-[>input:focus-visible]:ring-border-focus/50 has-[>input:focus-visible]:border-status-info",
  "data-checked:border-status-info data-checked:bg-state-soft",
  "data-invalid:border-status-danger",
  "data-disabled:pointer-events-none data-disabled:cursor-not-allowed data-disabled:opacity-50"
)

/** The radio dot or checkbox tick, sharing Checkbox's checked fill. */
export const QUESTIONNAIRE_INDICATOR = cn(
  "pointer-events-none relative col-start-1 row-start-1 flex size-4 shrink-0 translate-y-px items-center justify-center rounded-xs border border-border-subtle bg-surface-well",
  "group-has-data-[slot=questionnaire-choice-description]/questionnaire-choice:translate-y-0.5",
  "group-data-[type=radio]/questionnaire-choice:rounded-full",
  "group-data-checked/questionnaire-choice:border-action-primary group-data-checked/questionnaire-choice:bg-action-primary group-data-checked/questionnaire-choice:text-action-primary-foreground"
)

/** The shortcut key cap at the end of a choice, in Kbd's quiet tone. */
export const QUESTIONNAIRE_SHORTCUT = cn(
  "pointer-events-none col-start-3 row-start-1 hidden size-5 shrink-0 translate-y-px items-center justify-center justify-self-end rounded-sm border border-border bg-surface-base font-mono text-code-11 text-text-tertiary",
  "group-has-data-[slot=questionnaire-choice-description]/questionnaire-choice:translate-y-0.5",
  "group-data-[shortcut]/questionnaire-choice:inline-flex"
)

function Questionnaire({
  className,
  ...props
}: React.ComponentProps<typeof QuestionnairePrimitive.Root>) {
  return (
    <QuestionnairePrimitive.Root
      data-slot="questionnaire"
      className={cn("flex w-full min-w-0 flex-col gap-3", className)}
      {...props}
    />
  )
}

function QuestionnaireProgress({
  className,
  ...props
}: React.ComponentProps<typeof QuestionnairePrimitive.Progress>) {
  return (
    <QuestionnairePrimitive.Progress
      data-slot="questionnaire-progress"
      className={cn(
        "min-h-line w-fit min-w-counter text-caption-11 text-text-tertiary tabular-nums",
        className
      )}
      {...props}
    />
  )
}

function QuestionnaireItem({
  className,
  ...props
}: React.ComponentProps<typeof QuestionnairePrimitive.Item>) {
  return (
    <QuestionnairePrimitive.Item
      data-slot="questionnaire-item"
      className={cn(
        "flex min-w-0 flex-col gap-2 border-0 p-0 outline-none",
        className
      )}
      {...props}
    />
  )
}

function QuestionnaireTitle({
  className,
  ...props
}: React.ComponentProps<typeof QuestionnairePrimitive.Title>) {
  return (
    <QuestionnairePrimitive.Title
      data-slot="questionnaire-title"
      className={cn(
        "text-title-13 text-pretty text-text-primary [&:not(:has(~[data-slot=questionnaire-description]))]:mb-1",
        className
      )}
      {...props}
    />
  )
}

function QuestionnaireDescription({
  className,
  ...props
}: React.ComponentProps<typeof QuestionnairePrimitive.Description>) {
  return (
    <QuestionnairePrimitive.Description
      data-slot="questionnaire-description"
      className={cn("text-body-13 text-pretty text-text-secondary", className)}
      {...props}
    />
  )
}

function QuestionnaireChoices({
  className,
  ...props
}: React.ComponentProps<typeof QuestionnairePrimitive.Choices>) {
  return (
    <QuestionnairePrimitive.Choices
      data-slot="questionnaire-choices"
      className={cn(
        "group/questionnaire-choices grid min-w-0 gap-1.5",
        className
      )}
      {...props}
    />
  )
}

function QuestionnaireChoice({
  children,
  className,
  ...props
}: React.ComponentProps<typeof QuestionnairePrimitive.Choice>) {
  // The description is pulled out of the label so it can sit on its own grid
  // row, spanning the label *and* shortcut columns — see QUESTIONNAIRE_CHOICE.
  const items = React.Children.toArray(children)
  const description = items.find(
    (child) => React.isValidElement(child) && child.type === QuestionnaireChoiceDescription
  )
  const label = description ? items.filter((child) => child !== description) : items

  return (
    <QuestionnairePrimitive.Choice
      data-slot="questionnaire-choice"
      className={cn(QUESTIONNAIRE_CHOICE, className)}
      {...props}
    >
      <QuestionnairePrimitive.ChoiceInput
        data-slot="questionnaire-choice-input"
        className="absolute inset-0 z-10 size-full cursor-pointer opacity-0"
      />
      <span
        aria-hidden="true"
        data-slot="questionnaire-choice-indicator"
        className={QUESTIONNAIRE_INDICATOR}
      >
        <span
          data-slot="questionnaire-choice-indicator-dot"
          className="hidden size-2 rounded-full bg-action-primary-foreground group-data-[type=checkbox]/questionnaire-choice:hidden group-data-checked/questionnaire-choice:block"
        />
        <CheckIcon
          data-slot="questionnaire-choice-indicator-check"
          className="hidden size-(--icon-12) group-data-[type=radio]/questionnaire-choice:hidden group-data-checked/questionnaire-choice:block"
        />
      </span>
      <QuestionnairePrimitive.ChoiceLabel
        data-slot="questionnaire-choice-label"
        className="col-start-2 row-start-1 min-w-0 text-pretty wrap-anywhere leading-snug"
      >
        {label}
      </QuestionnairePrimitive.ChoiceLabel>
      <QuestionnairePrimitive.ChoiceShortcut
        data-slot="questionnaire-choice-shortcut"
        className={QUESTIONNAIRE_SHORTCUT}
      />
      {description}
    </QuestionnairePrimitive.Choice>
  )
}

function QuestionnaireChoiceDescription({
  className,
  ...props
}: React.ComponentProps<"span">) {
  return (
    <span
      data-slot="questionnaire-choice-description"
      // col-span-2, row-start-2: QuestionnaireChoice renders this as a
      // sibling of the label (not nested inside it) so it can reclaim the
      // shortcut badge's column below the first line — see
      // QUESTIONNAIRE_CHOICE for why that has to be a grid row rather than
      // part of the label's own flex column.
      //
      // wrap-anywhere, not break-words: a description quoting a URL or path
      // (no spaces to wrap at) otherwise sets the choice row's min-content
      // width to the whole string, pushing it past the rail and clipping the
      // rest under overflow-hidden instead of wrapping — break-word is
      // excluded from that min-content calculation by spec, so it alone
      // never actually stopped the row from being sized that way.
      className={cn(
        "col-start-2 col-span-2 row-start-2 min-w-0 text-body-12 text-pretty wrap-anywhere text-text-tertiary",
        className
      )}
      {...props}
    />
  )
}

function QuestionnaireInput({
  className,
  ...props
}: React.ComponentProps<typeof QuestionnairePrimitive.Input>) {
  return (
    <div
      data-slot="questionnaire-input-wrapper"
      className="group/questionnaire-input relative w-full min-w-0"
    >
      <QuestionnairePrimitive.Input
        data-slot="questionnaire-input"
        className={cn(
          "h-(--control-height-md) w-full min-w-0 rounded-md border border-border-subtle bg-surface-well px-3 text-body-13 text-text-primary transition-control outline-none",
          "placeholder:text-text-tertiary",
          "focus-visible:ring-2 focus-visible:ring-border-focus/50 focus-visible:border-status-info",
          "data-filled:border-status-info",
          "disabled:pointer-events-none disabled:cursor-not-allowed disabled:text-text-tertiary",
          "aria-invalid:border-status-danger",
          className
        )}
        {...props}
      />
    </div>
  )
}

function QuestionnaireError({
  className,
  ...props
}: React.ComponentProps<typeof QuestionnairePrimitive.Error>) {
  return (
    <QuestionnairePrimitive.Error
      data-slot="questionnaire-error"
      className={cn("text-caption-11 text-status-danger", className)}
      {...props}
    />
  )
}

function QuestionnaireActions({
  className,
  ...props
}: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="questionnaire-actions"
      className={cn(
        "grid w-full grid-cols-fill-auto-auto items-center gap-2",
        className
      )}
      {...props}
    />
  )
}

type NavigationStyle = Pick<React.ComponentProps<typeof Button>, "size" | "variant">

function QuestionnairePrevious({
  children,
  className,
  size = "sm",
  variant = "ghost",
  ...props
}: React.ComponentProps<typeof QuestionnairePrimitive.Previous> & NavigationStyle) {
  return (
    <QuestionnairePrimitive.Previous
      data-slot="questionnaire-previous"
      data-size={size}
      data-variant={variant}
      className={cn(
        buttonVariants({ size, variant }),
        "col-start-1 row-start-1 justify-self-start",
        className
      )}
      {...props}
    >
      {children ?? "Previous"}
    </QuestionnairePrimitive.Previous>
  )
}

function QuestionnaireSkip({
  children,
  className,
  size = "sm",
  variant = "ghost",
  ...props
}: React.ComponentProps<typeof QuestionnairePrimitive.Skip> & NavigationStyle) {
  return (
    <QuestionnairePrimitive.Skip
      data-slot="questionnaire-skip"
      data-size={size}
      data-variant={variant}
      className={cn(
        buttonVariants({ size, variant }),
        "col-start-2 row-start-1 justify-self-end",
        className
      )}
      {...props}
    >
      {children ?? "Skip"}
    </QuestionnairePrimitive.Skip>
  )
}

function QuestionnaireNext({
  children,
  className,
  size = "sm",
  variant = "default",
  ...props
}: React.ComponentProps<typeof QuestionnairePrimitive.Next> & NavigationStyle) {
  return (
    <QuestionnairePrimitive.Next
      data-slot="questionnaire-next"
      data-size={size}
      data-variant={variant}
      className={cn(
        buttonVariants({ size, variant }),
        "col-start-3 row-start-1 justify-self-end",
        className
      )}
      {...props}
    >
      {children ?? "Next"}
    </QuestionnairePrimitive.Next>
  )
}

function QuestionnaireSubmit({
  children,
  className,
  size = "sm",
  variant = "default",
  ...props
}: React.ComponentProps<typeof QuestionnairePrimitive.Submit> & NavigationStyle) {
  return (
    <QuestionnairePrimitive.Submit
      data-slot="questionnaire-submit"
      data-size={size}
      data-variant={variant}
      className={cn(
        buttonVariants({ size, variant }),
        "col-start-3 row-start-1 justify-self-end",
        className
      )}
      {...props}
    >
      {children ?? "Submit"}
    </QuestionnairePrimitive.Submit>
  )
}

export {
  Questionnaire,
  QuestionnaireActions,
  QuestionnaireChoice,
  QuestionnaireChoiceDescription,
  QuestionnaireChoices,
  QuestionnaireDescription,
  QuestionnaireError,
  QuestionnaireInput,
  QuestionnaireItem,
  QuestionnaireNext,
  QuestionnairePrevious,
  QuestionnaireProgress,
  QuestionnaireSkip,
  QuestionnaireSubmit,
  QuestionnaireTitle,
}
