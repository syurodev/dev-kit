import { Skeleton } from "@/components/ui/skeleton";
import { useSidebarContent } from "@/components/ui/sidebar-shell";

/**
 * What a module column looks like while its chunk is still on the wire.
 *
 * The old fallback was one line of "Loading Notes..." at the top of an empty
 * card, and the rail beside it showed nothing at all — a switch that took a
 * second read as the app hanging. This paints the shape every module shares
 * (rail search + rows, header with title/chips/actions, a toolbar, a list)
 * so the eye has somewhere to land, and registers a rail skeleton under the
 * same module id the real view will overwrite the moment it mounts.
 */
export function ModuleSkeleton() {
  useSidebarContent(RAIL);
  return (
    <div role="status" aria-label="Loading module" className="flex min-h-0 flex-1 flex-col">
      <div className="flex items-center gap-3 border-b border-border px-6 py-3.5">
        <Skeleton className="h-5 w-36" />
        <Skeleton className="h-6 w-20" />
        <Skeleton className="h-6 w-14" />
        <div className="ml-auto flex gap-2">
          <Skeleton className="h-8 w-24" />
          <Skeleton className="h-8 w-20" />
        </div>
      </div>
      <div className="flex gap-2 px-6 py-3">
        <Skeleton className="h-7 w-28" />
        <Skeleton className="h-7 w-36" />
        <Skeleton className="h-7 w-20" />
      </div>
      <div className="flex flex-col gap-2 px-6">
        {ROWS.map((w, i) => (
          <Skeleton key={i} className="h-4" style={{ width: w }} />
        ))}
      </div>
    </div>
  );
}

// Varied widths so it reads as a list, not a barcode.
const ROWS = ["72%", "58%", "81%", "64%", "49%", "76%", "61%", "68%"];

const RAIL = (
  <div className="flex flex-col gap-3 p-1">
    <Skeleton className="h-7 w-full" />
    <div className="flex flex-col gap-2 px-1">
      {ROWS.slice(0, 6).map((w, i) => (
        <Skeleton key={i} className="h-3.5" style={{ width: w }} />
      ))}
    </div>
  </div>
);
