import { describe, expect, it } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const SRC = join(import.meta.dir, "..");
const read = (rel: string) => readFileSync(join(SRC, rel), "utf8");

/**
 * Smoothness 8.1/8.2. Measured against the built app in headless Chromium
 * (a stub Wails transport recording every binding call): the shell's own
 * startup fetches — VaultStatus, ListModules, SyncConflicts,
 * AgentHubListAgents, AgentHubPendingPermissions, AgentHubListVendorTypes,
 * InteractionPendingList and two UIStateGets — all leave within ~50 ms of one
 * another rather than in a waterfall, and the rail, dock and tab strip are
 * first painted in the same frame as each other. What was missing was the
 * transition itself, which this pins.
 */
describe("gate → workbench", () => {
  it("hands over as one crossfade, not a cut", () => {
    expect(read("workbench/VaultGate.tsx")).toContain('className="shell-enter"');
  });

  it("fades the shell as a whole, without becoming a layout box of its own", () => {
    const css = read("index.css");
    const wrapper = css.match(/\.shell-enter\s*\{[^}]*\}/s)![0];
    // display:contents — the workbench stays the flex child it was.
    expect(wrapper).toContain("display: contents");

    const child = css.match(/\.shell-enter > \*\s*\{[^}]*@starting-style\s*\{[^}]*\}[^}]*\}/s);
    expect(child, ".shell-enter > * must carry an @starting-style block").not.toBeNull();
    expect(child![0]).toContain("opacity: 0");
    expect(child![0]).toContain("var(--motion-base)");
  });

  it("is switched off under prefers-reduced-motion", () => {
    const reduced = read("index.css").match(
      /@media \(prefers-reduced-motion: reduce\)\s*\{[\s\S]*?\n\}/,
    )![0];
    expect(reduced).toContain("shell-enter");
  });
});
