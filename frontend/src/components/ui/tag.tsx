import * as React from "react";
import { cva } from "class-variance-authority";
import { X } from "lucide-react";
import { Slot } from "radix-ui";

import { cn } from "@/lib/utils";

/**
 * Tag / Chip (Figma 378:2674) — 74×20, r=6, label Caption/11 Medium.
 *
 * It is a glass chip, not a solid block: white at 6% over whatever is behind,
 * with a candy-blue 8% hairline. Built as an opaque `surface/raised` fill it
 * looks plausible on one background and disappears on another, which is the
 * whole reason the design uses a translucent tier here.
 *
 * Figma gives this component no variants, so neither does this. Its padding
 * there is 3/6/3/8 with a 5px gap — all off the 4px grid — so the on-scale
 * 8/8 with a 4px gap is used instead, and the 20px height is fixed rather than
 * derived from padding. Flagged as a Figma-side drift, not copied.
 */
const tagVariants = cva(
  "inline-flex h-5 items-center justify-center gap-1 rounded-sm border px-2 bg-white/6 border-status-info/8 text-text-secondary text-caption-11-medium whitespace-nowrap transition-all select-none shrink-0",
);

export interface TagProps extends React.ComponentProps<"span"> {
  label?: React.ReactNode;
  dotColor?: string;
  onRemove?: () => void;
  asChild?: boolean;
}

const Tag = React.forwardRef<HTMLSpanElement, TagProps>(
  (
    {
      className,
      label,
      dotColor,
      onRemove,
      onClick,
      children,
      asChild = false,
      ...props
    },
    ref,
  ) => {
    const Comp = asChild ? Slot.Root : "span";

    return (
      <Comp
        ref={ref}
        data-slot="tag"
        className={cn(
          tagVariants(),
          onClick && "cursor-pointer hover:border-status-info/16",
          className,
        )}
        onClick={onClick}
        {...props}
      >
        {dotColor && (
          <span
            data-slot="tag-dot"
            className="size-1.5 shrink-0 rounded-full"
            style={{ backgroundColor: dotColor }}
            aria-hidden="true"
          />
        )}
        {children ?? label}
        {onRemove && (
          <button
            type="button"
            data-slot="tag-remove"
            aria-label="Remove tag"
            onClick={(e) => {
              e.stopPropagation();
              onRemove();
            }}
            className="inline-flex size-3 shrink-0 items-center justify-center text-text-tertiary hover:text-text-primary focus-visible:outline-none cursor-pointer"
          >
            <X className="size-3" aria-hidden="true" />
          </button>
        )}
      </Comp>
    );
  },
);
Tag.displayName = "Tag";

export { Tag, tagVariants };
export { Tag as Chip, tagVariants as chipVariants };
