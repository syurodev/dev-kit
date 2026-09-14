import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";

import { MessageHoverCard } from "./MessageHoverCard";

// No DOM here: what is checked is the markup the keyboard path relies on. That
// the row shows on :focus-visible and Tab walks into it is the browser's CSS
// and focus order, verified in the app, not in this file.
function render(props: Partial<React.ComponentProps<typeof MessageHoverCard>> = {}) {
  return renderToString(
    <MessageHoverCard align="end" text="explain the repo" onCopy={() => {}} onEdit={() => {}} {...props}>
      <p>explain the repo</p>
    </MessageHoverCard>,
  );
}

describe("MessageHoverCard from a keyboard", () => {
  it("puts Copy and Edit in the page right after the message, not only in a portal", () => {
    const html = render();
    // A closed hover card renders nothing; these are the inline row.
    const row = html.slice(html.indexOf('data-testid="message-actions"'));
    expect(html.indexOf("explain the repo</p>")).toBeLessThan(html.indexOf('data-testid="message-actions"'));
    expect(row).toContain('aria-label="Copy message"');
    expect(row).toContain('aria-label="Edit in composer"');
  });

  it("keeps the row out of the layout until focus is keyboard focus inside the message", () => {
    const html = render();
    const tag = html.match(/<div[^>]*data-testid="message-actions"[^>]*>/)?.[0] ?? "";
    expect(tag).toContain("hidden");
    expect(tag).toContain("focus-visible");
    expect(tag).not.toContain("hover");
  });

  it("sizes the action buttons as 28px controls", () => {
    const html = render({ meta: "1.2s · $0.03", onDetails: () => {} });
    const row = html.slice(html.indexOf('data-testid="message-actions"'));
    expect(row).not.toContain("control-height-xs");
    expect((row.match(/control-height-sm/g) ?? []).length).toBeGreaterThanOrEqual(3);
  });

  it("renders the message alone when there is nothing to show", () => {
    const html = renderToString(
      <MessageHoverCard align="start" text="hi">
        <p>hi</p>
      </MessageHoverCard>,
    );
    expect(html).not.toContain("message-actions");
  });
});
