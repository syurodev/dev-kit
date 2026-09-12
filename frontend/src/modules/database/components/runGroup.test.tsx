import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { TooltipProvider } from "@/components/ui/tooltip";
import { RunGroup } from "./RunGroup";
import type { ConsoleStatus } from "../consoleStatus";

const render = (outcome: ConsoleStatus, running = false) =>
  renderToString(
    <TooltipProvider>
      <RunGroup
        outcome={outcome}
        running={running}
        startedAt={running ? Date.now() : null}
        canRun
        onRun={() => {}}
        onStop={() => {}}
        runLabel="Run the query"
      />
    </TooltipProvider>,
  );

/**
 * The group is drawn twice — shapes under the goo filter, type above it — and
 * every one of these guards something that only breaks in the second copy.
 */
describe("the two layers", () => {
  const html = render({ kind: "success", time: "0.2s", message: "1 row" });

  it("hides the tracing from assistive technology", () => {
    expect(html).toContain('aria-hidden="true"');
  });

  // A focusable element inside aria-hidden is a stop the keyboard can reach
  // and a screen reader cannot describe.
  it("keeps the tracing out of the tab order", () => {
    const hidden = html.slice(html.indexOf('aria-hidden="true"'));
    expect(hidden).toContain('tabindex="-1"');
  });

  it("says the outcome once, not twice", () => {
    expect(html.match(/role="status"/g)).toHaveLength(2);
    // …and the second one is inside the tracing, which is hidden.
    expect(html.indexOf('aria-hidden="true"')).toBeLessThan(html.lastIndexOf('role="status"'));
  });
});

describe("what the pill is", () => {
  it("is a reading when there is nothing behind it", () => {
    const html = render({ kind: "success", time: "0.2s", message: "1 row" });
    expect(html).toContain('role="status"');
    expect(html).not.toContain("aria-expanded");
  });

  it("is a control when there is an error to open", () => {
    const html = render({
      kind: "error",
      time: "0.0s",
      message: "boom",
      detail: "boom\nand the rest",
    });
    expect(html).toContain('aria-expanded="false"');
    expect(html).toContain("show the full error");
  });

  /**
   * The ring is drawn on the unfiltered layer because a 1px ring under the goo
   * is blurred below the alpha threshold and disappears — so the filtered copy
   * must not try to draw one of its own.
   */
  it("leaves the focus ring to the layer that is not blurred", () => {
    const html = render({ kind: "running", time: "1.0s" }, true);
    expect(html).toContain("focus-visible:ring-0");
  });
});

/**
 * The filter exists, and it is never on while nothing is moving.
 *
 * It inflates a corner — a 28px square under it is the one round control in a
 * row of square ones — so it is paid for exactly the 180ms in which something
 * is on its way in or out. A static render, at rest or settled, must carry
 * none of it; if someone puts it back on permanently, this is what says so.
 */
describe("the filter", () => {
  it("is declared, so there is something to turn on", () => {
    expect(render({ kind: "running", time: "0.4s" }, true)).toContain("feGaussianBlur");
  });

  it.each([
    ["idle", { kind: "idle" } as ConsoleStatus, false],
    ["running and settled", { kind: "running", time: "0.4s" } as ConsoleStatus, true],
    ["done", { kind: "success", time: "0.2s", message: "1 row" } as ConsoleStatus, false],
  ])("is off while %s", (_name, status, running) => {
    expect(render(status, running)).not.toContain("filter:url(");
  });
});
