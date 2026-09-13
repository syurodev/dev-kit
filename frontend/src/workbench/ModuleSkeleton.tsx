import { useContext, useEffect, useRef, type ReactNode } from "react";
import { Skeleton } from "@/components/ui/skeleton";
import {
  SidebarModuleIdContext,
  useSidebarContent,
} from "@/components/ui/sidebar-shell";
import { useSidebarContentStore } from "@/stores/sidebarContent";
import { getModule, type ModuleManifest } from "@/modules/registry";
import { perfLog } from "@/lib/perfLog";

/**
 * Whether a module already has a rail on screen, from either source.
 *
 * Two, now. A document registers one while it is mounted, and a module
 * declares one on its manifest that is there whenever the module is. Missing
 * the second is how the first document opened in a module tore down the real
 * rail and put it back: nothing was REGISTERED, so the skeleton decided the
 * column was empty and took it over.
 */
export function railAlreadyShown(
  registered: ReactNode | undefined,
  moduleRail: ModuleManifest["sidebar"],
): boolean {
  return registered !== undefined || moduleRail !== undefined;
}

/**
 * Whether this skeleton should draw a rail, decided once when it appears.
 *
 * A cold module has no rail yet, and a blank column beside a loading module is
 * the case this skeleton's rail exists for. A module that already has one —
 * from a sibling document still on screen, or from its own manifest — has a
 * rail that has nothing to do with which chunk is loading. Replacing it made
 * opening a document look like the whole module reloading.
 *
 * Decided from a ref rather than re-read: registering changes the very value
 * the decision reads, so reacting to it would deregister, re-register, and
 * loop.
 */
function useShouldDrawRail(): boolean {
  const moduleId = useContext(SidebarModuleIdContext);
  const existing = useSidebarContentStore.getState().content[moduleId ?? ""];
  const moduleRail = getModule(moduleId ?? "")?.sidebar;
  const decided = useRef<boolean | null>(null);
  if (decided.current === null) {
    decided.current = !railAlreadyShown(existing, moduleRail);
  }
  return decided.current;
}

/**
 * Times how long this skeleton is on screen, which is how long a tab's chunk
 * took to arrive. Dev-only and behind the same switch as lib/perfTrace, so a
 * production build carries neither the timing nor the log.
 */
function useSkeletonTrace(): void {
  const shownAt = useRef(0);
  if (shownAt.current === 0) {
    shownAt.current = performance.now();
  }
  useEffect(() => {
    if (!import.meta.env.DEV && localStorage.getItem("devkitPerf") !== "1") {
      return;
    }
    const startedAt = shownAt.current;
    return () => {
      perfLog(`[perf] skeleton gone after ${Math.round(performance.now() - startedAt)}ms`);
    };
  }, []);
}

/**
 * What a module column looks like while its chunk is still on the wire.
 *
 * A line of "Loading…" over an empty card, with the rail beside it blank, reads
 * as the app hanging on any switch that takes a second. This paints the shape
 * every module shares (rail search + rows, header with title/chips/actions, a
 * toolbar, a list) so the eye has somewhere to land, and registers a rail
 * skeleton under the module id the real view overwrites when it mounts.
 */
export function ModuleSkeleton() {
  useSidebarContent(useShouldDrawRail() ? RAIL : null);
  useSkeletonTrace();
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
