import { contextPane, DEFAULT_SLOT, type Placement } from "./slotModel";

/**
 * Layout presets (docs/dev-kit/layout.mdx § Preset): instead of the shell
 * guessing what to close when room runs out, you pick the arrangement for the
 * task — from the Layouts menu or with ⌃⇧1–⌃⇧4.
 *
 * A preset only arranges the module's own pane and turns companions on or
 * off; it moves a companion only where it says so.
 */
export type LayoutState = {
  placement: Placement;
  agentOpen: boolean;
  terminalOpen: boolean;
};

export type LayoutPlan = LayoutState & {
  /** Close the module's context pane (it is the module's to reopen). */
  closeContext: boolean;
};

export type BuiltInPreset = { id: string; name: string; key: string };

export const BUILT_IN_PRESETS: readonly BuiltInPreset[] = [
  { id: "focus", name: "Focus", key: "1" },
  { id: "side", name: "Side panel", key: "2" },
  { id: "pair", name: "Pair with agent", key: "3" },
  { id: "terminal", name: "Terminal below", key: "4" },
];

/** What a built-in preset does to the layout as it is now. */
export function planPreset(id: string, moduleId: string, now: LayoutState): LayoutPlan {
  const ctx = contextPane(moduleId);
  const keep = { ...now, closeContext: false };
  switch (id) {
    case "focus":
      return { ...now, agentOpen: false, terminalOpen: false, closeContext: true };
    case "side":
      // The module's pane alone in the first column, full height.
      return { ...keep, agentOpen: false, placement: { ...now.placement, [ctx]: "a.top" } };
    case "pair": {
      // The agent gets a column of its own beside the module's pane.
      return { ...keep, agentOpen: true, placement: { ...now.placement, [ctx]: "a.top", agent: "b.top" } };
    }
    case "terminal":
      return { ...keep, terminalOpen: true, placement: { ...now.placement, terminal: "bottom" } };
    default:
      return keep;
  }
}

/** Back to the shell's defaults: every pane where it starts, nothing opened or closed. */
export function planReset(moduleId: string, now: LayoutState): LayoutPlan {
  return {
    ...now,
    closeContext: false,
    placement: {
      ...now.placement,
      agent: DEFAULT_SLOT.agent,
      terminal: DEFAULT_SLOT.terminal,
      [contextPane(moduleId)]: "a.top",
    },
  };
}

/** ⌃⇧1–⌃⇧4. By code, not key: with Shift held, "1" arrives as "!". */
export function presetKeyFromEvent(e: Pick<KeyboardEvent, "code" | "ctrlKey" | "shiftKey" | "metaKey" | "altKey">): string | null {
  if (!e.ctrlKey || !e.shiftKey || e.metaKey || e.altKey) return null;
  const m = /^Digit([1-4])$/.exec(e.code);
  return m ? m[1] : null;
}
