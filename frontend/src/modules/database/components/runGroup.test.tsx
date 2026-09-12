import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { TooltipProvider } from "@/components/ui/tooltip";
import { RunGroup } from "./RunGroup";
import type { ConsoleStatus } from "../consoleStatus";

const render = (status: ConsoleStatus, running = false) =>
  renderToString(
    <TooltipProvider>
      <RunGroup
        status={status}
        running={running}
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

describe("the filter", () => {
  // A 28px square under this blur comes back a circle, so at rest — when
  // there is no second body to be liquid with — it must not be applied.
  it("is off while there is nothing to split into", () => {
    expect(render({ kind: "idle" })).not.toContain("filter:url(");
  });

  it("is on once there is", () => {
    expect(render({ kind: "running", time: "0.4s" }, true)).toContain("filter:url(");
  });
});
