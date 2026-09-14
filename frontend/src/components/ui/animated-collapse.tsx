import type * as React from "react";
import { AnimatePresence, motion, useReducedMotion } from "framer-motion";

import { EASE_OUT_STRONG, MOTION_BASE, MOTION_SLOW, noMotion } from "@/lib/motion";

/**
 * Content that opens and closes by height, for disclosures whose open state
 * the caller owns — a chevron row and what it hides.
 *
 * Not Radix's Collapsible: that one mounts and unmounts with nothing between,
 * and animating it means keyframes against a CSS variable it measures, per
 * call site. This is the height-to-auto that RunGroup already does with
 * framer-motion, on the same tokens: opens over --motion-slow, closes over
 * --motion-base (shutting should be quicker than opening), --ease-out-strong
 * both ways.
 *
 * Nothing is rendered while closed, so a closed disclosure costs no DOM and
 * server rendering shows exactly what is open. Under Reduce Motion it snaps.
 *
 * Put spacing on `className`, not on the parent's gap: the element animates
 * from 0, and a gap outside it would snap shut only after the height has
 * already finished closing.
 */
export function AnimatedCollapse({
  open,
  children,
  className,
}: {
  open: boolean;
  children: React.ReactNode;
  className?: string;
}) {
  const reduce = useReducedMotion() ?? false;
  return (
    <AnimatePresence initial={false}>
      {open ? (
        <motion.div
          key="collapse"
          data-slot="animated-collapse"
          initial={{ height: 0, opacity: 0 }}
          animate={{
            height: "auto",
            opacity: 1,
            transition: reduce ? noMotion : { duration: MOTION_SLOW, ease: EASE_OUT_STRONG },
          }}
          exit={{
            height: 0,
            opacity: 0,
            transition: reduce ? noMotion : { duration: MOTION_BASE, ease: EASE_OUT_STRONG },
          }}
          style={{ overflow: "hidden" }}
        >
          <div className={className}>{children}</div>
        </motion.div>
      ) : null}
    </AnimatePresence>
  );
}
