import { describe, expect, it } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
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
    // Open on arrival, so the control it offers is "hide", not "show".
    expect(html).toContain('aria-expanded="true"');
    expect(html).toContain("Hide the full error");
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

  /**
   * The outside-click listener used to be `mousedown` in the CAPTURE phase.
   * Closing is setState, React re-renders between mousedown and mouseup, and
   * a browser only fires `click` when both landed on the same element — so
   * the press that closed the panel could be the press that was swallowed.
   * Bubble-phase `click` runs after the press has already been delivered.
   */
  it("closes the error panel after a click, never before it", () => {
    const source = readFileSync(join(import.meta.dir, "RunGroup.tsx"), "utf8");
    expect(source).toContain('addEventListener("click"');
    expect(source, "capture-phase mousedown can eat the press that follows it")
      .not.toContain('addEventListener("mousedown"');
  });

  /**
   * An error opens itself, and it opens INSIDE the body.
   *
   * Reading the message was the point of running the statement, so a second
   * click to see it was charging for something nobody would decline. And it
   * is the same control getting taller rather than a popup arriving beside
   * it — one thing on screen, which is also why the message can sit on its
   * own ground without becoming a second object.
   */
  it("opens an error without being asked", () => {
    const html = render(
      { kind: "error", time: "0.0s", message: "boom", detail: "boom\nand the rest" },
      false,
    );
    expect(html).toContain('aria-expanded="true"');
    expect(html).toContain("and the rest");
  });

  it("keeps the message inside the control, not in a panel beside it", () => {
    const html = render(
      { kind: "error", time: "0.0s", message: "boom", detail: "boom\nand the rest" },
      false,
    );
    // The body is the only positioned box; the message is within it, so the
    // whole of the control ends after one closing tag of the body.
    const body = html.indexOf("z-30");
    expect(body).toBeGreaterThan(-1);
    expect(html.indexOf("and the rest")).toBeGreaterThan(body);
  });

  /**
   * Open, it is one block of text on one ground.
   *
   * The first version printed the summary in a tinted bar and the same
   * sentence again in a panel underneath it, on the app's ordinary surface.
   * Two grounds and two copies of one sentence is two objects, whatever the
   * markup says — the control is supposed to be getting taller, not opening
   * something.
   */
  it("reads the message once, wherever the copies are", () => {
    const html = render({
      kind: "error",
      time: "0.0s",
      message: "Unknown column 'hello'",
      detail: "Unknown column 'hello' in 'SELECT'",
    });
    // Three copies exist and two are hidden: the ghost that measures the
    // closed width, and the summary itself — still mounted at zero opacity
    // so it has something to animate out of, and aria-hidden so it is not
    // read out twice.
    expect(
      html,
      "the summary is still read out beside the message it duplicates",
    ).toContain(
      '<span class="max-w-64 truncate" aria-hidden="true" style="opacity:0;transform:translateY(10px)">',
    );
    expect(html).toContain('aria-hidden="true" class="pointer-events-none invisible');
    // And it starts there rather than animating there, or a restored error
    // would flash its summary out of a row it was never in.
    expect(html, "initial={false} is what puts it there on the first frame")
      .not.toContain('<span class="max-w-64 truncate" aria-hidden="true">');
  });

  it("gives the message no ground of its own", () => {
    const html = render({
      kind: "error",
      time: "0.0s",
      message: "boom",
      detail: "boom\nand the rest",
    });
    expect(html, "a second surface inside the control is a second object")
      .not.toContain("bg-surface-well");
    expect(html).not.toContain("bg-surface-base");
  });

  it("leaves the toolbar's own footprint one control tall", () => {
    const html = render(
      { kind: "error", time: "0.0s", message: "boom", detail: "boom\nand the rest" },
      false,
    );
    // The wrapper keeps the row's height whatever the body does above it.
    expect(html).toContain('class="relative h-(--control-height-sm) shrink-0"');
  });

  it("shows nothing but Run before anything has been run", () => {
    const idle = render({ kind: "idle" });
    expect(idle).not.toContain('role="status"');
    expect(idle).toContain('aria-label="Run the query"');
  });
});
