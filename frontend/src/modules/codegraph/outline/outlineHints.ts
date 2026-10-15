import type { CodeGraphStepEffect, CodeGraphStepFrame } from "@/bridge/codegraph";
import { effectName } from "../graph/graphView";
import type { ReasonCount } from "./buildOutline";

/**
 * What the outline's markers mean, in words: the chip after a branch heading
 * that leaves, and the tooltips of the other marks. Pure, so the wording is
 * tested once and the view and its legend say the same thing.
 */

export type ExitMarker = {
  /** The chip: "returns", "throws", "breaks", "continues", or "exits" when the index did not say which. */
  word: string;
  /** A thrown exception is the one way out that is about safety: the chrome's warning. */
  tone: "neutral" | "warning";
  hint: string;
};

const ENDS = "This branch ends here —";

export function exitMarker(frame: { exits: boolean; exit?: CodeGraphStepFrame["exit"]; frameKind: string }): ExitMarker | null {
  if (!frame.exits && !frame.exit) return null;
  switch (frame.exit) {
    case "return":
      return { word: "returns", tone: "neutral", hint: `${ENDS} the method returns. Nothing after it runs on this path.` };
    case "throw":
      return { word: "throws", tone: "warning", hint: `${ENDS} an exception is thrown. Nothing after it runs on this path.` };
    case "break":
      return frame.frameKind === "case"
        ? { word: "breaks", tone: "neutral", hint: `${ENDS} it leaves the switch. The other cases do not run.` }
        : { word: "breaks", tone: "neutral", hint: `${ENDS} it leaves the loop. Nothing after it in the loop runs again.` };
    case "continue":
      return { word: "continues", tone: "neutral", hint: `${ENDS} it skips to the next iteration. Nothing after it runs on this pass.` };
    default:
      return {
        word: "exits",
        tone: "neutral",
        hint: `${ENDS} it returns, throws, breaks or continues. Nothing after it runs on this path. Re-index to see which.`,
      };
  }
}

/** "3 hidden: 2 logging, 1 metrics — click to show". */
export function foldHint(count: number, counts: readonly ReasonCount[], open: boolean): string {
  const list = counts.map((c) => `${c.count} ${c.reason}`).join(", ");
  return `${count} hidden: ${list} — click to ${open ? "hide" : "show"}`;
}

export function implsHint(n: number): string {
  return n === 1
    ? "Calls an abstract or overridable method; this class provides it, listed below"
    : `Calls an abstract or overridable method; these ${n} classes provide it, listed below`;
}

export const BASE_HINT = "The called method's own body, listed before the classes that override it";
export const SHOWN_ABOVE_HINT = "Already expanded above — click to jump there";
export const RECURSION_HINT = "Calls a method already open further up this path — click to jump to it";

export function effectHint(effect: CodeGraphStepEffect): string {
  return effect.target ? `Recognised ${effectName(effect)} to ${effect.target}` : `Recognised ${effectName(effect)}`;
}

export function hiddenHint(reason: string): string {
  return reason === "you chose" ? "Hidden because you chose to hide it" : `Hidden by default: ${reason}`;
}

/** A shown call's reason, when it says more than "it is a project call". */
export function shownHint(reason: string): string | null {
  if (!reason || reason === "project") return null;
  return reason === "you chose" ? "Shown because you chose to show it" : `Shown: ${reason}`;
}
