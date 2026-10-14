import { Search, LocateFixed, Maximize } from "lucide-react";
import { cn } from "@/lib/utils";

export type CanvasToolbarProps = {
  onSearch: () => void;
  onFit: () => void;
  onExpand: () => void;
  fitActive?: boolean;
};

/** Figma `canvas-toolbar` (482:4535): 26px tools on a soft strip, 3px inset. */
export function CanvasToolbar({ onSearch, onFit, onExpand, fitActive }: CanvasToolbarProps) {
  const buttons = [
    { icon: Search, action: onSearch, active: false, label: "Search nodes" },
    { icon: LocateFixed, action: onFit, active: !!fitActive, label: "Fit to view" },
    { icon: Maximize, action: onExpand, active: false, label: "Expand all" },
  ];
  return (
    <div className="flex gap-0.5 rounded-control border border-border-subtle bg-state-soft p-0.75">
      {buttons.map(({ icon: Icon, action, active, label }) => (
        <button
          key={label}
          type="button"
          aria-label={label}
          onClick={action}
          className={cn(
            "flex size-6.5 items-center justify-center rounded-sm hover:bg-state-soft",
            active && "bg-state-soft",
          )}
        >
          <Icon size={14} className="text-icon-secondary" />
        </button>
      ))}
    </div>
  );
}

/** The toolbar's fit tool alone, on the same strip — for a canvas too small for the rest. */
export function CanvasFitButton({ onFit }: { onFit: () => void }) {
  return (
    <div className="flex rounded-control border border-border-subtle bg-surface-raised p-0.75">
      <button
        type="button"
        aria-label="Fit to view"
        onClick={onFit}
        className="flex size-6.5 items-center justify-center rounded-sm hover:bg-state-soft focus-visible:ring-1 focus-visible:ring-ring focus-visible:outline-none"
      >
        <LocateFixed size={14} className="text-icon-secondary" />
      </button>
    </div>
  );
}
