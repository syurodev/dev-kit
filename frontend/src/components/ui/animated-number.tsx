import NumberFlow, { type NumberFlowProps } from "@number-flow/react";
import { EASE_OUT_CSS, MOTION_FAST, MOTION_SLOW } from "@/lib/motion";
import { cn } from "@/lib/utils";

export { NumberFlowGroup as AnimatedNumberGroup } from "@number-flow/react";

const ROLL_TIMING = { duration: MOTION_SLOW * 1000, easing: EASE_OUT_CSS };
const FADE_TIMING = { duration: MOTION_FAST * 1000, easing: EASE_OUT_CSS };

export type AnimatedNumberProps = Omit<NumberFlowProps,
  "value" | "transformTiming" | "spinTiming" | "opacityTiming" | "respectMotionPreference" | "willChange"
> & { value: number };

/** Small, changing UI counts. NumberFlow handles Reduce Motion, hidden views
 * and unsupported engines; it exposes the final value to assistive technology.
 * Keep dense tables, line numbers and editable inputs as ordinary text. */
export function AnimatedNumber({ className, ...props }: AnimatedNumberProps) {
  return <NumberFlow
    format={{ useGrouping: false }}
    {...props}
    className={cn("inline-block tabular-nums whitespace-nowrap", className)}
    transformTiming={ROLL_TIMING}
    spinTiming={ROLL_TIMING}
    opacityTiming={FADE_TIMING}
    respectMotionPreference
  />;
}
