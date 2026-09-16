import type { ComponentProps } from "react";

import { cn } from "@/lib/utils";

/** A quiet text sweep for work in progress. Keep completed content static. */
export function Shimmer({
  active = true,
  className,
  ...props
}: ComponentProps<"span"> & { active?: boolean }) {
  return (
    <span
      {...props}
      data-slot="shimmer"
      data-active={active || undefined}
      className={cn(
        active &&
          "shimmer shimmer-duration-2400 shimmer-spread-12 shimmer-color-text-primary motion-reduce:shimmer-none motion-reduce:animate-none forced-colors:shimmer-none",
        className,
      )}
    />
  );
}
