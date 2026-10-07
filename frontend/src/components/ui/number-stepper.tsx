import type { KeyboardEvent } from "react";
import { Minus, Plus } from "lucide-react";
import { AnimatedNumber } from "./animated-number";
import { Button } from "./button";
import { cn } from "@/lib/utils";

export type NumberStepperProps = {
  value: number;
  onChange: (value: number) => void;
  min?: number;
  max?: number;
  step?: number;
  label?: string;
  disabled?: boolean;
  className?: string;
};

/** Fixed value slot prevents digit changes from moving either button.
 * Keyboard and pointer update the real value immediately; only the glyphs roll. */
export function NumberStepper({ value, onChange, min = 0, max = 100, step = 1,
  label = "Value", disabled = false, className }: NumberStepperProps) {
  function change(next: number) {
    const bounded = Math.min(max, Math.max(min, next));
    if (!disabled && bounded !== value) onChange(bounded);
  }
  function onKeyDown(event: KeyboardEvent<HTMLSpanElement>) {
    if (disabled || event.altKey || event.ctrlKey || event.metaKey) return;
    const values: Record<string, number> = {
      ArrowUp: value + step, ArrowRight: value + step,
      ArrowDown: value - step, ArrowLeft: value - step,
      PageUp: value + step * 10, PageDown: value - step * 10,
      Home: min, End: max,
    };
    const next = values[event.key];
    if (next !== undefined) { event.preventDefault(); change(next); }
  }
  return <div className={cn("grid h-9 w-26 shrink-0 grid-cols-[2rem_minmax(0,1fr)_2rem] items-center rounded-md border border-border-subtle bg-surface-well", className)}>
    <Button type="button" variant="ghost" size="icon" className="h-full w-full rounded-r-none"
      disabled={disabled || value <= min} aria-label={`${label}: smaller`} onClick={() => change(value - step)}><Minus aria-hidden className="size-3.5" /></Button>
    <span role="spinbutton" tabIndex={disabled ? -1 : 0} aria-label={label}
      aria-valuenow={value} aria-valuemin={min} aria-valuemax={max} aria-disabled={disabled || undefined}
      onKeyDown={onKeyDown}
      className="flex h-full min-w-0 items-center justify-center text-code-12 text-text-primary outline-none focus-visible:ring-2 focus-visible:ring-status-info/20 aria-disabled:opacity-50">
      <AnimatedNumber value={value} aria-hidden="true" />
    </span>
    <Button type="button" variant="ghost" size="icon" className="h-full w-full rounded-l-none"
      disabled={disabled || value >= max} aria-label={`${label}: larger`} onClick={() => change(value + step)}><Plus aria-hidden className="size-3.5" /></Button>
  </div>;
}
