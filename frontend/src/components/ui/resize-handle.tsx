'use client';

import type * as React from 'react';
import { cn } from '@/lib/utils';

export type ShellResizeHandleProps = React.HTMLAttributes<HTMLDivElement> & {
  orientation?: 'vertical' | 'horizontal';
  isResizing?: boolean;
  forceState?: 'rest' | 'hover' | 'focus' | 'resizing';
  onPointerDown?: (e: React.PointerEvent<HTMLDivElement>) => void;
  'aria-label'?: string;
  tabIndex?: number;
};

/**
 * Shell / Resize Handle (Figma 699:1104, 4 variants: Orientation × State)
 *
 * EXCEPTION — Rest is completely empty. No fill, no border, no hairline, no grip.
 * "Invisible at rest so it is never mistaken for a divider."
 *
 * - Vertical: 8 wide (e.g. 8×120 or 8×full), Horizontal: 8 tall (120×8 or full×8)
 * - Hover hairline: 1px --border-default rgba(178,213,229,0.16), spans full length, centred
 * - Hover grip: 3×32 (V) / 32×3 (H), --text-tertiary, rounded-full, centred
 * - Focus ring: 2px --border-focus #b2d5e5, deliberately overflows the 8px gutter
 */
export function ShellResizeHandle({
  orientation = 'vertical',
  isResizing = false,
  forceState,
  className,
  style,
  onPointerDown,
  'aria-label': ariaLabel,
  tabIndex = 0,
  ...props
}: ShellResizeHandleProps) {
  const isVertical = orientation === 'vertical';

  const showHover = forceState === 'hover' || forceState === 'resizing' || isResizing;
  const showFocus = forceState === 'focus';

  return (
    <div
      role="separator"
      aria-orientation={orientation}
      aria-label={ariaLabel ?? (isVertical ? 'Resize column' : 'Resize row')}
      tabIndex={tabIndex}
      onPointerDown={onPointerDown}
      data-orientation={orientation}
      data-resizing={isResizing}
      data-state={forceState ?? (isResizing ? 'resizing' : 'rest')}
      style={style}
      className={cn(
        'group relative shrink-0 select-none outline-none transition-colors',
        isVertical
          ? 'w-2 h-full cursor-col-resize flex items-center justify-center'
          : 'h-2 w-full cursor-row-resize flex items-center justify-center',
        // In rest state: completely empty (transparent, no border)
        'bg-transparent',
        className
      )}
      {...props}
    >
      {/* 1px hairline: --border-default rgba(178,213,229,0.16), centered, spans full length */}
      <div
        data-slot="resize-hairline"
        className={cn(
          'pointer-events-none absolute transition-opacity duration-150',
          isVertical
            ? 'inset-y-0 left-1/2 -translate-x-1/2 w-px'
            : 'inset-x-0 top-1/2 -translate-y-1/2 h-px',
          isResizing
            ? 'bg-ring opacity-100'
            : 'bg-[rgba(178,213,229,0.16)]',
          showHover
            ? 'opacity-100'
            : 'opacity-0 group-hover:opacity-100'
        )}
      />

      {/* Grip pill: 3×32 (V) / 32×3 (H), --text-tertiary, rounded-full, centred */}
      <div
        data-slot="resize-grip"
        className={cn(
          'pointer-events-none absolute inset-0 m-auto rounded-full bg-text-tertiary transition-opacity duration-150',
          isVertical ? 'w-[3px] h-8' : 'h-[3px] w-8',
          showHover
            ? 'opacity-100'
            : 'opacity-0 group-hover:opacity-100'
        )}
      />

      {/* Focus ring: 2px --border-focus #b2d5e5, deliberately overflows 8px gutter */}
      <div
        data-slot="resize-focus-ring"
        className={cn(
          // Figma 699:1104 insets the ring -2.5% / -37.5%, which on both an
          // 8x120 and a 120x8 handle works out to a uniform -3px, and gives it
          // no corner radius. It deliberately overflows the 8px gutter.
          'pointer-events-none absolute -inset-[3px] border-2 border-[var(--border-focus,#b2d5e5)] transition-opacity duration-150',
          showFocus
            ? 'opacity-100'
            : 'opacity-0 group-focus-visible:opacity-100'
        )}
      />
    </div>
  );
}

export { ShellResizeHandle as ResizeHandle };
