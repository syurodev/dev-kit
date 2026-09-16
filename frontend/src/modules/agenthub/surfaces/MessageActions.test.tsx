import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";

import { MessageActions } from "./MessageActions";

// No DOM here: what is checked is the markup. That hover and keyboard focus
// reveal the row is the browser's CSS, verified in the app, not in this file.
function render(props: Partial<React.ComponentProps<typeof MessageActions>> = {}) {
  return renderToString(
    <MessageActions align="end" text="explain the repo" onCopy={() => {}} onEdit={() => {}} {...props}>
      <p>explain the repo</p>
    </MessageActions>,
  );
}

function rowTag(html: string): string {
  return html.match(/<div[^>]*data-testid="message-actions"[^>]*>/)?.[0] ?? "";
}

describe("MessageActions", () => {
  it("puts time, Copy and Edit inline right under the message, with no floating card", () => {
    const html = render({ at: "2026-09-16T08:21:00Z" });
    expect(html.indexOf("explain the repo</p>")).toBeLessThan(html.indexOf('data-testid="message-actions"'));
    const row = html.slice(html.indexOf('data-testid="message-actions"'));
    expect(row).toContain('aria-label="Copy message"');
    expect(row).toContain('aria-label="Edit in composer"');
    expect(row).toMatch(/\d{1,2}:\d{2}/);
    expect(html).not.toContain("hover-card");
  });

  it("reveals the row on hover or keyboard focus without moving the layout", () => {
    const tag = rowTag(render());
    // Opacity, not display: the row keeps its height, so pointing at a message
    // does not push the transcript below it down.
    expect(tag).toContain("opacity-0");
    expect(tag).toContain("group-hover/message:opacity-100");
    expect(tag).toContain("group-focus-within/message:opacity-100");
    expect(tag).not.toMatch(/\bhidden\b/);
    expect(tag).toContain("h-(--control-height-sm)");
  });

  it("lines up with the message's own edge", () => {
    expect(render({ align: "end" })).toContain("items-end");
    expect(render({ align: "start" })).toContain("items-start");
  });

  it("sizes the action buttons as 28px controls", () => {
    const html = render({ meta: "1.2s · $0.03", onDetails: () => {} });
    const row = html.slice(html.indexOf('data-testid="message-actions"'));
    expect(row).not.toContain("control-height-xs");
    expect((row.match(/control-height-sm/g) ?? []).length).toBeGreaterThanOrEqual(3);
  });

  it("renders the message alone when there is nothing to show", () => {
    const html = renderToString(
      <MessageActions align="start" text="hi">
        <p>hi</p>
      </MessageActions>,
    );
    expect(html).toBe("<p>hi</p>");
  });
});
