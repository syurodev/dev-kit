import { Search, LocateFixed, Maximize } from "lucide-react";
import { cn } from "@/lib/utils";

export type CanvasToolbarProps = {
  onSearch: () => void;
  onFit: () => void;
  onExpand: () => void;
  fitActive?: boolean;
};

export function CanvasToolbar({ onSearch, onFit, onExpand, fitActive }: CanvasToolbarProps) {
  const buttons = [
    { icon: Search, action: onSearch, active: false, label: "Search nodes" },
    { icon: LocateFixed, action: onFit, active: !!fitActive, label: "Fit to view" },
    { icon: Maximize, action: onExpand, active: false, label: "Expand all" },
  ];
  return (
    <div className="flex gap-0.5 rounded-[9px] border border-border-subtle bg-surface-raised p-0.75">
      {buttons.map(({ icon: Icon, action, active, label }) => (
        <button
          key={label}
          type="button"
          aria-label={label}
          onClick={action}
          className={cn(
            "flex size-6.5 items-center justify-center rounded-md",
            active && "bg-surface-tinted",
          )}
        >
          <Icon size={14} className="text-icon-secondary" />
        </button>
      ))}
    </div>
  );
}
