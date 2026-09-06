import * as React from "react"

import { cn } from "@/lib/utils"

/**
 * Shell / Scroll Ruler.
 *
 * A sparse position indicator: one tick per milestone, plus one for where the
 * viewport currently is. Nothing else — no tick grid down the full height.
 *
 * This deliberately departs from Figma `700:1100`, which draws a tick every
 * ~5px (80 of them over 400px). That reads as a decorated scrollbar: dense
 * ticks encode nothing the scroll position does not already say. Sparse ticks
 * encode *where the meaningful places are*, which is the only thing a ruler
 * adds over a scrollbar. The Figma node is marked "spec — do not instance" and
 * the App Shell frames draw their rulers inline, so nothing else depends on
 * the dense reading.
 *
 * Two rules kept from the spec, because they are about honesty rather than
 * density:
 *
 *  1. It renders NOTHING unless the region actually overflows. Figma: "a
 *     scrollbar over content that fits is a lie about how much there is."
 *  2. Length never encodes anything. Every tick is the same size, and colour
 *     alone separates a milestone from the current position — Figma's own
 *     rule, "a milestone differs only in colour".
 *
 * Sparse ticks are also why this needs no measurement: with no 5px grid to
 * stay in phase with, every position is a percentage, so there is no
 * ResizeObserver, no callback ref and no snapping.
 */

const TRACK_WIDTH = 8
const TICK_LENGTH = 5.5
const TICK_HEIGHT = 2
const TICK_INSET_RIGHT = 2

export interface ScrollRulerProps
  extends Omit<React.ComponentProps<"div">, "children"> {
  /** Full scrollable height of the content. */
  contentHeight: number
  /** Height of the visible region. */
  viewportHeight: number
  /** Current scroll offset. */
  scrollTop: number
  /** Positions worth marking, as 0..1 fractions of the scrollable range. */
  milestones?: number[]
}

function clamp01(n: number): number {
  return n < 0 ? 0 : n > 1 ? 1 : n
}

/** Centres the 2px tick on the fraction without needing the track's height. */
function tickStyle(fraction: number): React.CSSProperties {
  return {
    top: `calc(${clamp01(fraction) * 100}% - ${TICK_HEIGHT / 2}px)`,
    right: TICK_INSET_RIGHT,
    width: TICK_LENGTH,
    height: TICK_HEIGHT,
  }
}

function ScrollRuler({
  contentHeight,
  viewportHeight,
  scrollTop,
  milestones = [],
  className,
  style,
  ...props
}: ScrollRulerProps) {
  if (contentHeight <= viewportHeight) {
    return null
  }

  const maxScroll = contentHeight - viewportHeight
  const progress = maxScroll > 0 ? clamp01(scrollTop / maxScroll) : 0

  return (
    <div
      data-slot="scroll-ruler"
      aria-hidden="true"
      className={cn("pointer-events-none relative shrink-0", className)}
      style={{ width: TRACK_WIDTH, ...style }}
      {...props}
    >
      {milestones.map((fraction, i) => (
        <div
          key={`milestone-${i}`}
          data-slot="scroll-ruler-milestone"
          className="absolute bg-text-secondary"
          style={tickStyle(fraction)}
        />
      ))}

      <div
        data-slot="scroll-ruler-active"
        className="absolute"
        style={{ ...tickStyle(progress), backgroundColor: "var(--brand-text)" }}
      />
    </div>
  )
}

/**
 * Reads the three numbers `ScrollRuler` needs off a real scroll container.
 * Kept here rather than in a hooks file because it exists only to feed this
 * component, and because the ruler is meaningless without live metrics — a
 * ruler wired to constants is the "scrollbar over content that fits" the Figma
 * spec explicitly rules out.
 */
export function useScrollRulerMetrics(
  ref: React.RefObject<HTMLElement | null>,
): { contentHeight: number; viewportHeight: number; scrollTop: number } {
  const [metrics, setMetrics] = React.useState({
    contentHeight: 0,
    viewportHeight: 0,
    scrollTop: 0,
  })

  React.useEffect(() => {
    const node = ref.current
    if (!node) {
      return
    }
    const read = () =>
      setMetrics({
        contentHeight: node.scrollHeight,
        viewportHeight: node.clientHeight,
        scrollTop: node.scrollTop,
      })

    read()
    node.addEventListener("scroll", read, { passive: true })
    // Content can grow without a scroll event, which is the case that silently
    // leaves a stale ruler on screen.
    const observer =
      typeof ResizeObserver !== "undefined" ? new ResizeObserver(read) : null
    observer?.observe(node)
    return () => {
      node.removeEventListener("scroll", read)
      observer?.disconnect()
    }
  }, [ref])

  return metrics
}

export { ScrollRuler }
