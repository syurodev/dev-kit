import * as React from "react"
import { Eye, EyeOff } from "lucide-react"
import { cn } from "@/lib/utils"

/**
 * Input (Figma 28:62) — 8 variants, Type (Text/Password) × State (Default/Focus/Error/Disabled).
 *
 * Field: 360×40, r=8 (rounded-md), padding 0/12/0/12 (px-3).
 * Shared form surface recipe:
 * - rest: bg-white/6 border border-status-info/16
 * - focus: border-(--stroke-1-5) border-status-info (1.5px)
 * - error: border-status-danger (1px)
 * - disabled: text dims to text-text-tertiary (#8b8e8a)
 *
 * Typography:
 * - Label: text-label-12 text-text-secondary (#c6c8c2)
 * - Helper: text-caption-11 text-text-tertiary (#8b8e8a), on error turns text-status-danger (#d87886)
 * - Value: text-body-13 text-text-primary (#f0f1eb) — snapped from retired 14px
 * - Placeholder: text-text-tertiary (#8b8e8a)
 *
 * Type=Password includes a built-in Visibility action (16×16 eye toggle) inside the field.
 */
export interface InputProps extends Omit<React.ComponentProps<"input">, "size"> {
  inputSize?: "sm" | "default" | "lg"
  label?: React.ReactNode
  helperText?: React.ReactNode
  error?: boolean | string
}

const Input = React.forwardRef<HTMLInputElement, InputProps>(
  (
    {
      className,
      type = "text",
      inputSize = "default",
      label,
      helperText,
      error,
      disabled,
      ...props
    },
    ref,
  ) => {
    const [showPassword, setShowPassword] = React.useState(false)
    const isPassword = type === "password"
    const resolvedType = isPassword && showPassword ? "text" : type
    const isError = Boolean(error)
    const errorMessage = typeof error === "string" ? error : undefined

    const inputElement = (
      // No width cap when the caller supplies its own label: it has taken over
      // the layout, and a field that refuses to be wider than 360 than its
      // container is a component overruling the page. The 360 in Figma is the
      // control's DEFAULT size, not a maximum — a connection dialog's grid asks
      // for full width and got a field stopping halfway across the row.
      // The labelled path keeps the cap, because there the component owns the
      // whole block and the spec is describing it.
      <div className="relative flex w-full items-center">
        <input
          ref={ref}
          type={resolvedType}
          disabled={disabled}
          data-slot="input"
          aria-invalid={isError ? true : props["aria-invalid"]}
          className={cn(
            "w-full min-w-0 rounded-md border border-status-info/16 bg-white/6 px-3 text-body-13 text-text-primary transition-colors outline-none",
            "placeholder:text-text-tertiary",
            "focus-visible:border-(--stroke-1-5) focus-visible:border-status-info focus-visible:outline-none focus-visible:ring-0",
            "disabled:pointer-events-none disabled:cursor-not-allowed disabled:text-text-tertiary disabled:placeholder:text-text-tertiary/60",
            "aria-invalid:border-status-danger aria-invalid:ring-0",
            inputSize === "sm" && "h-(--control-height-md) text-body-12 px-2.5",
            inputSize === "default" && "h-(--control-height-xl)",
            inputSize === "lg" && "h-(--control-height-2xl) text-body-16 px-3.5",
            isPassword && "pr-9",
            className,
          )}
          {...props}
        />
        {isPassword && (
          <button
            type="button"
            tabIndex={-1}
            disabled={disabled}
            data-slot="input-visibility-action"
            aria-label={showPassword ? "Hide password" : "Show password"}
            onClick={() => setShowPassword((prev) => !prev)}
            className="absolute right-3 flex size-4 items-center justify-center text-text-secondary hover:text-text-primary focus:outline-none disabled:pointer-events-none disabled:opacity-38 cursor-pointer"
          >
            {showPassword ? (
              <EyeOff className="size-4" aria-hidden="true" />
            ) : (
              <Eye className="size-4" aria-hidden="true" />
            )}
          </button>
        )}
      </div>
    )

    if (!label && !helperText && !errorMessage) {
      return inputElement
    }

    return (
      <div className="flex w-full max-w-[360px] flex-col gap-1.5" data-slot="input-container">
        {label && (
          <label className="text-label-12 text-text-secondary select-none">
            {label}
          </label>
        )}
        {inputElement}
        {(helperText || errorMessage) && (
          <span
            className={cn(
              "text-caption-11 select-none",
              isError ? "text-status-danger" : "text-text-tertiary",
            )}
          >
            {errorMessage || helperText}
          </span>
        )}
      </div>
    )
  },
)
Input.displayName = "Input"

export { Input }
