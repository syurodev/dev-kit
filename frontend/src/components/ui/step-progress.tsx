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
        "flex w-full max-w-[600px] items-center justify-between py-2.5",
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
            className={cn(
              "flex flex-col items-center gap-2 relative group",
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
              className={cn(
                "h-[3px] rounded-full transition-all duration-300",
                isActive && "w-[56px] bg-action-primary shadow-[0_0_8px_rgba(198,255,52,0.4)]",
                isCompleted && "w-[32px] bg-action-primary",
                isPending && "w-[32px] bg-border"
              )}
            />

            {/* Label */}
            <span
              className={cn(
                "text-[11px] whitespace-nowrap transition-colors",
                isActive && "font-medium text-text-primary",
                isCompleted && "font-normal text-text-primary",
                isPending && "font-normal text-text-tertiary"
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
