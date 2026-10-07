import * as React from "react"

import { cn } from "@/lib/utils"
import { CONTROL_SURFACE, CONTROL_FOCUS } from "./control-styles"
import { ChevronDownIcon } from "lucide-react"

type NativeSelectProps = Omit<React.ComponentProps<"select">, "size"> & {
  size?: "sm" | "default"
}

function NativeSelect({
  className,
  size = "default",
  ...props
}: NativeSelectProps) {
  return (
    <div
      className={cn(
        "group/native-select relative w-fit has-[select:disabled]:opacity-50",
        className
      )}
      data-slot="native-select-wrapper"
      data-size={size}
    >
      <select
        data-slot="native-select"
        data-size={size}
        className={cn(
          "h-(--control-height-md) w-full min-w-0 appearance-none rounded-control py-1 pr-8 pl-2.5 text-body-13 outline-none select-none disabled:pointer-events-none disabled:cursor-not-allowed data-[size=sm]:h-(--control-height-sm) data-[size=sm]:rounded-md data-[size=sm]:py-0.5",
          CONTROL_SURFACE,
          CONTROL_FOCUS,
        )}
        {...props}
      />
      <ChevronDownIcon className="pointer-events-none absolute top-1/2 right-2.5 size-4 -translate-y-1/2 text-muted-foreground select-none" aria-hidden="true" data-slot="native-select-icon" />
    </div>
  )
}

function NativeSelectOption({
  className,
  ...props
}: React.ComponentProps<"option">) {
  return (
    <option
      data-slot="native-select-option"
      className={cn("bg-system-canvas text-system-canvas-text", className)}
      {...props}
    />
  )
}

function NativeSelectOptGroup({
  className,
  ...props
}: React.ComponentProps<"optgroup">) {
  return (
    <optgroup
      data-slot="native-select-optgroup"
      className={cn("bg-system-canvas text-system-canvas-text", className)}
      {...props}
    />
  )
}

export { NativeSelect, NativeSelectOptGroup, NativeSelectOption }
