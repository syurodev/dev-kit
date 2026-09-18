import { describe, expect, it } from "bun:test";
import { planPreset, planReset, presetKeyFromEvent, type LayoutState } from "./layoutPresets";

const now: LayoutState = {
  placement: { agent: "a.bottom", terminal: "b.top", "context:notes": "b.bottom" },
  agentOpen: true,
  terminalOpen: true,
};

describe("layout presets", () => {
  it("Focus closes everything, moves nothing", () => {
    const p = planPreset("focus", "notes", now);
    expect(p).toMatchObject({ agentOpen: false, terminalOpen: false, closeContext: true });
    expect(p.placement).toEqual(now.placement);
  });

  it("Pair with agent gives the agent its own column", () => {
    const p = planPreset("pair", "notes", now);
    expect(p.placement).toMatchObject({ "context:notes": "a.top", agent: "b.top" });
    expect(p.agentOpen).toBe(true);
  });

  it("Terminal below only moves the terminal", () => {
    const p = planPreset("terminal", "notes", now);
    expect(p.placement).toEqual({ ...now.placement, terminal: "bottom" });
    expect(p.agentOpen).toBe(true);
  });

  it("Reset puts every pane back where it starts without opening or closing any", () => {
    const p = planReset("notes", now);
    expect(p.placement).toEqual({ agent: "a.bottom", terminal: "bottom", "context:notes": "a.top" });
    expect(p).toMatchObject({ agentOpen: true, terminalOpen: true, closeContext: false });
  });

  it("reads ⌃⇧1–⌃⇧4 by key position", () => {
    const e = { code: "Digit2", ctrlKey: true, shiftKey: true, metaKey: false, altKey: false };
    expect(presetKeyFromEvent(e)).toBe("2");
    expect(presetKeyFromEvent({ ...e, metaKey: true })).toBeNull();
    expect(presetKeyFromEvent({ ...e, code: "Digit5" })).toBeNull();
  });
});
