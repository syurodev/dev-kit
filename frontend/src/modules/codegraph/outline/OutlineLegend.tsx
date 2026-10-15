import type { ReactNode } from "react";
import { Info } from "lucide-react";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { LEGEND_TRIGGER } from "../graph/LayerLegend";
import { EFFECT_CHIP } from "../graph/tokens";
import { exitMarker } from "./outlineHints";
import { BRANCH_TEXT, ExitChip, FOLD_CHIP, QUIET_MARK, implementationsLabel } from "./markers";

/** Room the button takes at the outline's bottom-left, so the last row can scroll clear of it. */
export const OUTLINE_LEGEND_CLEARANCE = 40;

const chip = (exit: "return" | "throw" | "break" | "continue") => <ExitChip marker={exitMarker({ exits: true, exit, frameKind: "if" })!} />;

/** Each mark the outline draws, with a sample of it and what it means. */
export const OUTLINE_LEGEND: { sample: ReactNode; text: string }[] = [
  { sample: <span className={`font-mono ${BRANCH_TEXT}`}>if (ok)</span>, text: "A branch: the calls under it run only when it holds (if, else, case, catch, loop…)." },
  { sample: chip("return"), text: "The branch ends by returning; nothing after it runs on this path." },
  { sample: chip("throw"), text: "The branch ends by throwing an exception." },
  { sample: chip("break"), text: "The branch leaves its loop (or its switch)." },
  { sample: chip("continue"), text: "The branch skips to the loop's next iteration." },
  { sample: <span className="font-mono font-semibold text-text-primary">save(order)</span>, text: "A call into this project; plain text is a library call." },
  { sample: <span className={FOLD_CHIP}>2 calls · logging</span>, text: "Calls folded away (logging, metrics, helpers…); click to show them." },
  { sample: <span className="font-mono text-text-secondary opacity-60">log.info(…)</span>, text: "A call hidden by default, shown because its fold is open." },
  { sample: <span className={QUIET_MARK}>{implementationsLabel(2)}</span>, text: "Calls an abstract or overridable method; the classes that provide it are listed below." },
  { sample: <span className={QUIET_MARK}>(base)</span>, text: "The called method's own body, listed before its overrides." },
  { sample: <span className={QUIET_MARK}>shown above ↑</span>, text: "Already expanded above; click to jump there." },
  { sample: <span className={QUIET_MARK}>↻ recursion</span>, text: "Calls a method already open on this path; click to jump to it." },
  { sample: <span className={EFFECT_CHIP}>Kafka produce</span>, text: "What DevKit recognised the call doing: Kafka, HTTP, Redis or DB." },
  { sample: <span className="text-caption-11 text-text-tertiary">Right-click</span>, text: "On a call: always show or always hide it, or every call on its type." },
];

export function OutlineLegendContent() {
  return (
    <ul className="flex flex-col gap-1.75">
      {OUTLINE_LEGEND.map((entry) => (
        <li key={entry.text} className="flex items-baseline gap-3">
          <span className="flex w-28 shrink-0 justify-start text-code-12">{entry.sample}</span>
          <span className="text-caption-12 text-text-secondary">{entry.text}</span>
        </li>
      ))}
    </ul>
  );
}

/** The Graph's legend button, for the outline's marks: read on demand, then out of the way. */
export function OutlineLegend() {
  return (
    <Popover>
      <PopoverTrigger asChild>
        <button type="button" aria-label="Show outline legend" className={LEGEND_TRIGGER}>
          <Info size={12} />
          Legend
        </button>
      </PopoverTrigger>
      <PopoverContent side="top" align="start" className="max-h-(--radix-popover-content-available-height) max-w-(--radix-popover-content-available-width) w-104 overflow-y-auto p-3">
        <OutlineLegendContent />
      </PopoverContent>
    </Popover>
  );
}
