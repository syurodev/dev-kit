import { cn } from "@/lib/utils"

function Skeleton({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="skeleton"
      className={cn("animate-pulse rounded-md bg-muted", className)}
      {...props}
    />
  )
}

/**
 * A list that has not answered yet. Distinct from an empty list on purpose:
 * a view that renders its "nothing here" copy while the first fetch is still
 * out flashes that copy on every visit — the thing this exists to stop.
 */
function ListSkeleton({ rows = 6, className }: { rows?: number; className?: string }) {
  return (
    <div role="status" aria-label="Loading" className={cn("flex flex-col gap-2.5 p-4", className)}>
      {Array.from({ length: rows }, (_, i) => (
        <Skeleton key={i} className="h-4" style={{ width: `${[72, 58, 81, 64, 49, 76, 61, 68][i % 8]}%` }} />
      ))}
    </div>
  )
}

export { Skeleton, ListSkeleton }
