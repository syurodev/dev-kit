import { describe, expect, it } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { renderToString } from "react-dom/server";
import { ActionTooltip } from "./action-tooltip";
import { TooltipProvider } from "./tooltip";
import { Button } from "./button";

const render = (node: React.ReactElement) =>
  renderToString(<TooltipProvider>{node}</TooltipProvider>);

/**
 * A `title` attribute is two things at once: the tooltip, and the accessible
 * NAME of an icon-only button. A Radix tooltip is only the first — it sets
 * aria-describedby, which describes a control that already has a name. Moving
 * a toolbar from one to the other without this leaves a row of buttons a
 * screen reader can only call "button".
 */
describe("ActionTooltip", () => {
  it("names an icon-only button with its tooltip", () => {
    const html = render(
      <ActionTooltip label="Reload this page of rows">
        <Button size="icon-sm">
          <svg />
        </Button>
      </ActionTooltip>,
    );
    expect(html).toContain('aria-label="Reload this page of rows"');
  });

  // A button with visible text names itself. Overriding that with aria-label
  // would make the screen reader say something the user cannot see.
  it("leaves a button that names itself alone", () => {
    const html = render(
      <ActionTooltip label="Explain the query plan without executing">
        <Button>Explain</Button>
      </ActionTooltip>,
    );
    expect(html).not.toContain("aria-label");
    expect(html).toContain("Explain");
  });

  /**
   * A disabled button fires no pointer events, so the tooltip has nothing to
   * hang on and call sites wrapped the button in a span by hand. aria-label
   * then landed on the span — which has no role, so it names nothing — and the
   * control inside it went back to being called "button". Four of them had.
   */
  it("names the control, not the wrapper it needs when disabled", () => {
    const html = render(
      <ActionTooltip label="Commit" wrap>
        <Button size="icon" disabled>
          <svg />
        </Button>
      </ActionTooltip>,
    );
    // The span must be there (that is the point of `wrap`) and the name must
    // be past it, on the button.
    expect(html).toMatch(/<span[^>]*>\s*<button[^>]*aria-label="Commit"/);
    expect(html).not.toMatch(/<span[^>]*aria-label="Commit"/);
  });

  it("does not override a name the caller chose", () => {
    const html = render(
      <ActionTooltip label="Move to folder">
        <button type="button" aria-label="Move this connection">
          <svg />
        </button>
      </ActionTooltip>,
    );
    expect(html).toContain('aria-label="Move this connection"');
    expect(html).not.toContain('aria-label="Move to folder"');
  });
});

/**
 * The native tooltip and the app's are visibly different — a grey OS box after
 * a second and a half, against the app's glass one after 100ms — so a toolbar
 * that mixes them looks like two toolbars.
 *
 * The exception is deliberate and not a button: a grid cell's `title` is the
 * full value behind a clipped one (2,220 Radix roots is the cost the grid's
 * single context menu exists to avoid). A tree row's qualified name used to be
 * one too; the tree is windowed now, so its rows carry the app's tooltip.
 */
describe("the module's chrome uses one tooltip", () => {
  const read = (file: string) =>
    readFileSync(join(import.meta.dir, "../../modules/database", file), "utf8");

  it.each([
    "components/TableToolbar.tsx",
    "components/DatabaseSidebar.tsx",
    "components/QueryConsole.tsx",
    "components/BrowsePager.tsx",
  ])("%s raises no native tooltip", (file) => {
    expect(read(file), "title= is the OS tooltip, not the app's").not.toMatch(/\stitle="/);
  });

  // A truncated tab's full title is the tooltip people read most, so it is
  // the app's — the tab strip once drew the grey OS box instead.
  it("the tab strip raises no native tooltip", () => {
    const strip = readFileSync(join(import.meta.dir, "../../workbench/tabs/TabStrip.tsx"), "utf8");
    expect(strip).not.toMatch(/\stitle=\{/);
  });
});
