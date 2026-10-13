import { Layers } from "lucide-react";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { cn } from "@/lib/utils";
import { LAYERS, type Layer } from "./tokens";

const ORDER: Layer[] = ["controller", "service", "repository", "unknown"];

/** Figma `layer-legend` (482:4565) content, now read on demand rather than parked on screen. */
const CARD_ROW = "flex items-center gap-2";

/**
 * Collapsed by default: a two-card legend permanently parked over the graph
 * cost fitView a fixed strip of the canvas it could never get back — on a
 * tall, narrow plan (most of them) that strip was the difference between the
 * graph filling the pane and the graph shrunk to a column with dead space on
 * both sides. A small trigger in the same corner opens the same content as a
 * popover instead: read once, then out of the way.
 */
export function LayerLegend({ plan = false }: { plan?: boolean }) {
  return (
    <Popover>
      <PopoverTrigger asChild>
        <button
          type="button"
          aria-label="Show layer legend"
          className="flex items-center gap-1.5 rounded-control border border-glass-border bg-glass-float px-2 py-1.25 text-label-11-caps text-text-tertiary shadow-canvas-legend backdrop-blur-md hover:text-text-primary"
        >
          <Layers size={12} />
          Legend
        </button>
      </PopoverTrigger>
      <PopoverContent side="top" align="start" className="w-auto gap-1.75 p-2.5">
        {plan && (
          <span className={cn(CARD_ROW, "border-b border-border-subtle pb-1.75")}>
            <span className="size-2.5 shrink-0 rounded-xs border border-dashed border-(--hue-blue)" />
            <span className="text-label-11-caps text-text-tertiary">
              Dashed = plan not satisfied
            </span>
          </span>
        )}
        {ORDER.map((layer) => (
          <span key={layer} className={CARD_ROW}>
            <span
              className="size-2 shrink-0 rounded-full"
              style={{ backgroundColor: `var(${LAYERS[layer].cssVar})` }}
            />
            <span className="text-label-11-caps text-text-tertiary">{LAYERS[layer].label}</span>
          </span>
        ))}
      </PopoverContent>
    </Popover>
  );
}
