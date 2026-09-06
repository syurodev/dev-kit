import * as React from "react"
import { cn } from "@/lib/utils"

export interface StepProgressItem {
  id: number | string
  label: string
}

const DEFAULT_STEPS: StepProgressItem[] = [
  { id: 1, label: "Create password" },
  { id: 2, label: "Save recovery key" },
  { id: 3, label: "Confirm" },
  { id: 4, label: "Ready" },
]

export interface StepProgressProps extends React.ComponentProps<"div"> {
  currentStep: number
  steps?: StepProgressItem[]
  onStepClick?: (stepIndex: number) => void
}

function StepProgress({
  currentStep = 1,
  steps = DEFAULT_STEPS,
  onStepClick,
  className,
  ...props
}: StepProgressProps) {
  return (
    <div
      data-slot="step-progress"
      className={cn(
        // No glass here. Figma 29:40 draws this as a bare 600x68 frame with no
        // fill, no backdrop blur and no inset highlight — it is the one member
        // of the 05/06 family that opts out of the shared glass recipe. A blur
        // on a transparent element still blurs what is behind it, so restoring
        // it "for consistency" is a visible regression, not a tidy-up.
        "grid grid-cols-4 w-full max-w-[600px] pt-2.5 pb-2.5",
        className
      )}
      {...props}
    >
      {steps.map((item, index) => {
        const stepNum = index + 1
        const isActive = stepNum === currentStep
        const isCompleted = stepNum < currentStep
        const isPending = stepNum > currentStep

        return (
          <div
            key={item.id}
            data-slot="step-progress-item"
            data-step={stepNum}
            className={cn(
              "flex flex-col items-center gap-[15px] relative group w-full",
              onStepClick && isCompleted && "cursor-pointer"
            )}
            onClick={() => {
              if (onStepClick && (isCompleted || isActive)) {
                onStepClick(stepNum)
              }
            }}
          >
            {/* Milestone line */}
            <div
              data-slot="step-progress-milestone"
              className={cn(
                "h-[3px] rounded-full transition-all duration-300",
                isActive && "w-14 bg-action-primary",
                isCompleted && "w-8 bg-action-primary",
                isPending && "w-8 bg-border"
              )}
            />

            {/* Label */}
            <span
              data-slot="step-progress-label"
              className={cn(
                "whitespace-nowrap transition-colors",
                isActive && "text-caption-11-medium text-text-primary",
                isCompleted && "text-caption-11 text-text-primary",
                isPending && "text-caption-11 text-text-tertiary"
              )}
            >
              {item.label}
            </span>
          </div>
        )
      })}
    </div>
  )
}

export { StepProgress }
