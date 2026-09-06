import { cn } from "@/lib/utils";

/**
 * The dock at rest: one line per module, the active one lit. Small enough
 * that it can float over content — including the bottom slot's terminal —
 * without a reserved band.
 *
 * Lines only, no labels: position is the cue, and dock order is the stable
 * alphabetical-by-id sort in listRegisteredModules(), so it is learnable.
 */
export function CollapsedDock({
  count,
  activeIndex,
}: {
  count: number;
  activeIndex: number;
}) {
  return (
    <div aria-hidden="true" className="flex items-center gap-1 px-2 py-1">
      {Array.from({ length: count }, (_, i) => (
        <span
          key={i}
          className={cn(
            "h-[3px] w-3 rounded-full transition-colors",
            // action/primary for active line, text-tertiary for others per Figma 208:438
            i === activeIndex ? "bg-action-primary" : "bg-text-tertiary",
          )}
        />
      ))}
    </div>
  );
}
