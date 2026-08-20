import { cn } from "@/lib/utils";

/**
 * The dock at rest: one line per module, the active one lit. Small enough
 * that it can float over content — including the bottom slot's terminal —
 * without a reserved band.
 *
 * Lines only, no labels: position is the cue, and dock order is the fixed
 * registerModule order in Workbench.tsx, so it is learnable.
 */
export function CollapsedDock({
  count,
  activeIndex,
}: {
  count: number;
  activeIndex: number;
}) {
  return (
    <div aria-hidden="true" className="flex items-center gap-1 px-2 py-[5px]">
      {Array.from({ length: count }, (_, i) => (
        <span
          key={i}
          className={cn(
            "h-[3px] w-3 rounded-full transition-colors",
            // action/primary is the same token --sidebar-primary already uses
            // for an active nav item, so "you are here" reads the same
            // everywhere in the app.
            i === activeIndex ? "bg-primary" : "bg-text-tertiary/45",
          )}
        />
      ))}
    </div>
  );
}
