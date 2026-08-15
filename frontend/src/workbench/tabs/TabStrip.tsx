import { AnimatePresence, LazyMotion, domMax, m } from "framer-motion";
import { Wrench, X } from "lucide-react";
import { cn } from "@/lib/utils";
import { getModule } from "../../modules/registry";
import type { TabStatus, WorkbenchTab } from "../../stores/workbenchTabs";

function statusDotClass(status: TabStatus): string {
  switch (status) {
    case "running":
      return "bg-(--status-running)";
    case "success":
      return "bg-(--status-success)";
    case "error":
      return "bg-(--status-error)";
    default:
      return "bg-(--status-idle)";
  }
}

type TabStripProps = {
  tabs: WorkbenchTab[];
  activeTabId: string;
  onSelect: (id: string) => void;
  onClose: (id: string) => void;
};

/**
 * Lives inside the 50px native-titlebar strip (Mac.InvisibleTitleBarHeight
 * in main.go). The container defaults to `--wails-draggable: drag` so the
 * empty space after the last tab still moves the window like the rest of
 * the header; every clickable element inside overrides it back to no-drag,
 * same pattern Wails' own frameless-window docs use.
 *
 * `layout` on each tab (needs the heavier `domMax` bundle, not the
 * `domAnimation` one Dock.tsx uses — layout animation isn't in that
 * smaller bundle) means a tab's own width change (the close button
 * revealing on hover/focus) and a tab being added/removed both animate
 * smoothly, including the resulting reflow of every other open tab —
 * without it, siblings snap to their new position in a single frame.
 */
export function TabStrip({ tabs, activeTabId, onSelect, onClose }: TabStripProps) {
  return (
    <LazyMotion features={domMax}>
      <div className="scrollbar-hide flex min-w-0 flex-1 items-center gap-1 overflow-x-auto [--wails-draggable:drag]">
        <AnimatePresence initial={false}>
          {tabs.map((tab) => (
            <m.button
              key={tab.id}
              layout
              initial={{ opacity: 0, scale: 0.9 }}
              animate={{ opacity: 1, scale: 1 }}
              exit={{ opacity: 0, scale: 0.9 }}
              transition={{ layout: { type: "spring", stiffness: 500, damping: 35 } }}
              type="button"
              onClick={() => onSelect(tab.id)}
              className={cn(
                // No `gap` here on purpose: gap reserves space between EVERY
                // flex child, including the close button even while it's
                // collapsed to `w-0` (unlike the `hidden` it replaced, a
                // zero-width child still participates in `gap`) — that
                // phantom space was pushing the title off-center at rest.
                // Spacing between children is explicit margin below instead.
                "group flex h-7 shrink-0 items-center rounded-md px-2.5 text-xs font-medium transition-colors [--wails-draggable:no-drag]",
                tab.id === activeTabId
                  ? "bg-card text-foreground"
                  : "text-muted-foreground hover:bg-white/5 hover:text-foreground",
              )}
            >
              {(() => {
                // Same self-registered icon the module's Dock entry uses
                // (modules/registry.ts ModuleManifest.icon) — one source of
                // truth, no separate tab-strip-only icon map to drift.
                const ModuleIcon = getModule(tab.moduleId)?.icon ?? Wrench;
                return <ModuleIcon className="size-3.5 shrink-0 opacity-70" />;
              })()}
              {tab.status && tab.status !== "idle" ? (
                <span
                  className={cn(
                    "ml-1.5 size-1.5 shrink-0 rounded-full",
                    statusDotClass(tab.status),
                  )}
                />
              ) : null}
              <span className="ml-1.5 max-w-40 truncate">{tab.title}</span>
              <span
                role="button"
                tabIndex={0}
                aria-label={`Close ${tab.title}`}
                onClick={(event) => {
                  event.stopPropagation();
                  onClose(tab.id);
                }}
                onKeyDown={(event) => {
                  if (event.key === "Enter" || event.key === " ") {
                    event.preventDefault();
                    event.stopPropagation();
                    onClose(tab.id);
                  }
                }}
                className="ml-1 flex w-0 shrink-0 scale-75 items-center justify-center overflow-hidden rounded opacity-0 transition-[width,opacity,transform] duration-150 ease-out hover:bg-white/10 group-hover:w-4 group-hover:scale-100 group-hover:opacity-100 group-focus-within:w-4 group-focus-within:scale-100 group-focus-within:opacity-100 [--wails-draggable:no-drag]"
              >
                <X className="size-3" />
              </span>
            </m.button>
          ))}
        </AnimatePresence>
      </div>
    </LazyMotion>
  );
}
