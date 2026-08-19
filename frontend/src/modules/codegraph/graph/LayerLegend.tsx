import { LAYERS, type Layer } from "./tokens";

const ORDER: Layer[] = ["controller", "service", "repository", "unknown"];

export function LayerLegend() {
  return (
    <div className="flex flex-col gap-1.75 rounded-control border border-border-subtle bg-surface-raised px-3 py-2.5 shadow-[0_2px_8px_rgba(0,0,0,0.3)]">
      {ORDER.map((layer) => (
        <span key={layer} className="flex items-center gap-2">
          <span
            className="size-2 rounded-full"
            style={{ backgroundColor: `var(${LAYERS[layer].cssVar})` }}
          />
          <span className="font-mono text-[9px] font-medium tracking-[0.06em] text-text-tertiary">
            {LAYERS[layer].label}
          </span>
        </span>
      ))}
    </div>
  );
}
