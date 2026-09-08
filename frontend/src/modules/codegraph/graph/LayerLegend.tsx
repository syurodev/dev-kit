import { LAYERS, type Layer } from "./tokens";

const ORDER: Layer[] = ["controller", "service", "repository", "unknown"];

/** Figma `layer-legend` (482:4565): a glass card, 8px swatches, caps labels. */
const CARD =
  "flex flex-col gap-1.75 rounded-control border border-glass-border bg-glass-float px-3 py-2.5 shadow-[0_2px_8px_rgba(0,0,0,0.3)] backdrop-blur-md";

export function LayerLegend({ plan = false }: { plan?: boolean }) {
  return (
    <div className="flex flex-col gap-2">
      {/* `border-legend` (482:8255): the plan canvas spends dashed borders on
          "not satisfied", so it says what a dashed border means. */}
      {plan && (
        <div className={CARD}>
          <span className="flex items-center gap-2">
            <span className="size-2.5 rounded-xs border border-dashed border-status-info" />
            <span className="text-label-11-caps text-text-tertiary">
              Dashed = plan not satisfied
            </span>
          </span>
        </div>
      )}
      <div className={CARD}>
        {ORDER.map((layer) => (
          <span key={layer} className="flex items-center gap-2">
            <span
              className="size-2 rounded-full"
              style={{ backgroundColor: `var(${LAYERS[layer].cssVar})` }}
            />
            <span className="text-label-11-caps text-text-tertiary">{LAYERS[layer].label}</span>
          </span>
        ))}
      </div>
    </div>
  );
}
