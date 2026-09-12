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

describe("what the status segment is", () => {
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

});

/**
 * The body is one control, and the metaball is gone.
 *
 * It was two shapes welded by a blur-and-threshold filter, and every tuning
 * that fixed one of its problems made another worse: the filter inflated the
 * button's corners, the weld's reach depended on how wide the message was, and
 * a capsule end against a flat edge welds to a cusp. One body has no joint and
 * so has none of them. This is what says nobody put it back.
 */
describe("one body", () => {
  const html = render({ kind: "success", time: "0.2s", message: "1 row" });

  it("runs no filter over the control", () => {
    expect(html).not.toContain("feGaussianBlur");
    expect(html).not.toContain("filter:url(");
  });

  it("draws the control once, not as shapes plus a tracing", () => {
    expect(html.match(/aria-label="Run the query"/g)).toHaveLength(1);
    expect(html).not.toContain('aria-hidden="true" class="flex');
  });

  // The clip is what lets the body narrow onto Run; the ring has to be on the
  // body itself or that same clip would cut it off.
  it("clips to the body and rings the body", () => {
    expect(html).toContain("overflow-hidden");
    expect(html).toContain("has-[:focus-visible]:ring-1");
    expect(html).toContain("focus-visible:ring-0");
  });

  /**
   * ActionTooltip's `wrap` puts a content-sized span between the button and
   * the row, so `h-full` on the button resolved against 14px of icon and left
   * Run half the body's height — a notch at the right end, and the reason the
   * whole thing stopped reading as one shape. The height comes from `size`.
   */
  it("takes Run's height from its size, not from the wrapper it sits in", () => {
    expect(html).toContain("h-(--control-height-sm)");
    const button = html.slice(html.indexOf("<button"), html.indexOf("</button>"));
    expect(button, "h-full here resolves against the tooltip wrapper").not.toContain("h-full");
  });

  it("shows nothing but Run before anything has been run", () => {
    const idle = render({ kind: "idle" });
    expect(idle).not.toContain('role="status"');
    expect(idle).toContain('aria-label="Run the query"');
  });
});
