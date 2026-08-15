import { X } from "lucide-react";
import { cn } from "@/lib/utils";
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
 */
export function TabStrip({ tabs, activeTabId, onSelect, onClose }: TabStripProps) {
  return (
    <div className="scrollbar-hide flex min-w-0 flex-1 items-center gap-1 overflow-x-auto [--wails-draggable:drag]">
      {tabs.map((tab) => (
        <button
          key={tab.id}
          type="button"
          onClick={() => onSelect(tab.id)}
          className={cn(
            "group flex h-7 shrink-0 items-center gap-1.5 rounded-md px-2.5 text-xs font-medium transition-colors [--wails-draggable:no-drag]",
            tab.id === activeTabId
              ? "bg-card text-foreground"
              : "text-muted-foreground hover:bg-white/5 hover:text-foreground",
          )}
        >
          {tab.status && tab.status !== "idle" ? (
            <span
              className={cn("size-1.5 shrink-0 rounded-full", statusDotClass(tab.status))}
            />
          ) : null}
          <span className="max-w-40 truncate">{tab.title}</span>
          <span
            role="button"
            aria-label={`Close ${tab.title}`}
            onClick={(event) => {
              event.stopPropagation();
              onClose(tab.id);
            }}
            className="ml-0.5 hidden size-4 shrink-0 items-center justify-center rounded hover:bg-white/10 group-hover:flex [--wails-draggable:no-drag]"
          >
            <X className="size-3" />
          </span>
        </button>
      ))}
    </div>
  );
}
